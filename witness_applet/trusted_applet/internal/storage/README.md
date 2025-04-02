# Storage

This directory contains a work-in-progress storage system for use with
the ArmoredWitness.
It should be considered experimental and subject to change!

Some details on the requirements and design of the storage system are below.

### Requirements

*   Allow the witness unikernel to persist small amounts of data, think multiple independent records of up to a few MB.
*   Use the eMMC as storage
*   Avoid artificially shortening the life of storage hardware (flash)
*   Persisted state should be resilient to corruption from power failure/reboot during writes

#### Nice-to-haves

*   Be somewhat reusable for other ArmoredWitness use cases we may have.
    *   This probably means being able to store different types of data in specified locations.

#### Non-requirements

*   While we're ultimately limited by the performance of the storage hardware, it's not a priority to achieve the lowest possible latency or highest possible throughput for writes.
*   Integration with Go's `os.Open()` style APIs (this _would_ be great, but would require upstream work in TamaGo so is explicitly out of scope for now).

#### Out-of-scope

Some things are explicitly out of scope for this design:

*   Protecting against an attacker modifying the data on the storage in some out-of-band fashion.
*   Hardware failure resulting in previously readable data becoming unreadable/corrupted.
*   Supporting easy discovery / enumeration of data on disk, or preventing duplicate data from being written. Higher level code should be responsible for understanding what data should be in which slots.

### Design

A relatively simple storage API which offers a fixed number of "storage slots" to which a representation of state can be written. Slot storage will be allocated a range of the underlying storage, starting at a known byte offset and with a known length. This slot storage is also preconfigured with the number of slots that it should allocate (or alternatively/equivalently, the number of bytes to be reserved per-slot).

Each slot is backed by a fixed size "journal" stored across _N_ eMMC blocks. 

Logically it can be thought of like so:

![image showing logical layout](images/logical_layout.png)

Physically it may look like this on the MMC block device itself (9 blocks per journal is just an example):

![image showing physical layout](images/physical_layout.png)

#### API

The API tries to be as simple as possible to use and implement for now - e.g. since we're only intending this to be used for O(MB) of data, it's probably fine to pass this to/from the storage layer as a straight `[]byte` slice.

However, if necessary, we could try to make the API more like Go's io framework, with `Reader` and `Writers`.


```go
// Partition describes the extent and layout of a single contiguous region 
// underlying block storage.
type Partition struct {}

// Open opens the specified slot, returns an error if the slot is out of bounds.
func (p *Partition) Open(slot int) (*Slot, error)


// Slot represents the current data in a slot.
type Slot struct {}

// Read returns the last data successfully written to the slot, along with 
// a token which can be used with CheckAndWrite.
func (s *Slot) Read() ([]byte, uint32, error)

// Write stores the provided data to the slot.
// Upon successful completion, this data will be returned by future calls 
// to Read until another successful Write call is mode.
// If the call to Write fails, future calls to Read will return the 
// previous successfully written data, if any.
func (s *Slot) Write(p []byte) error 

// CheckAndWrite behaves like Write, with the exception that it will 
// immediately return an error if the slot has been successfully written 
// to since the Read call which produced the passed-in token.
func (s *Slot) CheckAndWrite(token uint32, p []byte) error

```

#### Internal structures

Data stored in the slot is represented by an _"update record"_ written to the journal.

The update record contains:

Field Name   | Type                        | Notes
-------------|-----------------------------|-------------------------
`Magic`      |`[4]byte{'T', 'F', 'J', '0'}`| Magic record header, v0
`Revision`   |`uint32`                     | Incremented with each write to slot
`DataLen`    |`uint64`                     | `len(RecordData)`
`Checksum`   |`[32]byte{}`                 | `SHA256` of `RecordData`
`RecordData` |`[DataLen]byte{}`            | Application data


An update record is considered _valid_ if its:

*   `Magic` is correct
*   `Checksum` is correct for the data in `RecordData[:DataLen]`

The first time `Open` is called for a given slot, the slot's journal will be scanned from the beginning to look for the valid update record with the largest `Revision`. The Data from this record is the data associated with the slot. It could potentially be cached in RAM at this point if it's small enough.

If no such record exists, then the slot has not yet been successfully written to and there is no data associated with the slot.

An update to the slot causes an update record to be written to the journal starting at either:

*   The first byte of the blocks following the extent of the "current" update record (i.e all blocks contain header/data for at most 1 record), if there is sufficient space remaining in the journal to accommodate the entire update record without wrapping around to the first blocks, or
*   The first byte of the first block in the journal, if there is no current record or the update record will not fit in the remaining journal space.

Following a successful write to storage, the metadata associated with slot (i.e. Revision, current header location, location for next write, etc.) is updated.

The diagram below shows a sequence of several update record writes of varying data sizes. These writes are taking place in a single journal, which you'll remember comprises several blocks.

The grey boxes represent blocks containing old/previous data, green represents blocks holding the latest successful write.

The numbers indicate a header with a particular `Revision`, blocks with `…` contain follow-on `RecordData`, and an x indicates invalid record header:

```
⬛⬛⬛⬛⬛⬛⬛⬛⬛⬛ - Initial state, nothing written
🟩🟩🟩⬛⬛⬛⬛⬛⬛⬛ - First record (rev=1) has been successfully stored
⬜⬜⬜🟩🟩⬛⬛⬛⬛⬛ - Next record (rev=2) is stored with the next available block
⬜⬜⬜⬜⬜🟩🟩🟩⬛⬛ - Same again.
🟩🟩🟩⬜⬜⬜⬜⬜⬛⬛ - The 4th record will not fit in the remaining space, so is written starting at the zeroth block, overwriting old revision(s) - note it does not wrap around.
⬜⬜⬜🟩🟩🟩⬜⬜⬛⬛ - Subsequent revisions continue in this vein.
```

Since record revisions should always be increasing as we scan left-to-right through the slot storage, we can assume we've found the newest update record when we've either reached the end of the storage space, or after having read at least 1 _good_ update record we find a record with a lower `Revision` than the previous record, or one with an invalid `Magic` or `Checksum.`

#### Failed/interrupted writes

For a failed write to the storage to have any permanent effect at all, it must have succeeded in writing at least the 1st block of the update record, and so the stored header checksum will be invalid. This allows the failure to be detected when reading back with high probability.

The maximum permitted `RecordData` size is restricted to `(TotalSlotSize/3) - len(Header)`; this prevents a failed write obliterating all or part of the previous successful write, so unless the failed write is the first attempt to write to the slot, there will always be a valid previous record available (modulo storage fabric failure).

Adding records with failed writes:

```
⬛⬛⬛⬛⬛⬛⬛⬛⬛ - Initial state, nothing written
🟩🟩⬜⬜⬜⬜⬜⬜⬛ - First record (rev=1) stored successfully
⬜⬜🟩🟩🟩⬜⬜⬜⬛ - Second write (rev=2) is successful too.
⬜⬜⬜⬜⬜🟥🟥🟥⬛ - Third write fails
⬜⬜⬜⬜⬜🟩🟩🟩⬛ - Application retries, record (rev=3) is written successfully this time.
🟩🟩⬜⬜⬜⬜⬜⬜⬛ - Application succesfully retries and writes (rev=4)
⬜⬜🟩🟩🟩⬜⬜⬜⬛ - and (rev=5)
⬜⬜⬜⬜⬜🟩🟩🟩⬛ - and (rev=6), too
🟥🟥🟥⬜⬜🟩🟩🟩⬛ - Attempt to write (rev=7), located at the zeroth block, fails, corrupting (rev=4) and (rev=5), but rev=6, the current good record, is intact.
```

#### Other properties

This journal type approach affords a couple of additional nice properties given the environment and use case:

1. The API can provide _check-and-set_ semantics: _"Write an update record with revision X, iff the current record is revision X-1"_.
2. A very basic notion of "wear levelling" is provided since writes are spread out across most blocks. Note that this is less important here as the ArmoredWitness has eMMC storage, which mandates that the integrated controlled performs wear-leveling transparently.
