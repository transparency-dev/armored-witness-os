// Copyright 2022 The Armored Witness Applet authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package slots

import (
	"errors"
	"fmt"
	"sync"

	"k8s.io/klog/v2"
)

// Geometry describes the physical layout of a Partition and its slots on the
// underlying storage.
type Geometry struct {
	// Start identifies the address of first block which is part of a partition.
	Start uint
	// Length is the number of blocks covered by this partition.
	// i.e. [Start, Start+Length) is the range of blocks covered by this partition.
	Length uint
	// SlotLengths is an ordered list containing the lengths of the slot(s)
	// allocated within this partition.
	// For obvious reasons, great care must be taken if, once data has been written
	// to one or more slots, the values specified in this list at the time the data
	// was written are changed.
	SlotLengths []uint
}

// Validate checks that the geometry is self-consistent.
func (g Geometry) Validate() error {
	t := uint(0)
	for _, l := range g.SlotLengths {
		t += l
	}
	if t > g.Length {
		return fmt.Errorf("invalid geometry: total slot length (%d blocks) exceeds overall length (%d blocks)", t, g.Length)
	}
	return nil
}

// Partition describes the extent and layout of a single contiguous region of
// underlying block storage.
type Partition struct {
	// dev provides the device-specific read/write functionality.
	dev BlockReaderWriter

	// slots describes the layout of the slot(s) stored within this partition.
	slots []Slot
}

// OpenPartition returns a partition struct for accessing the slots described by the given
// geometry using the provided read/write methods.
func OpenPartition(rw BlockReaderWriter, geo Geometry) (*Partition, error) {
	if err := geo.Validate(); err != nil {
		return nil, err
	}

	ret := &Partition{
		dev: rw,
	}

	b := geo.Start
	for _, l := range geo.SlotLengths {
		ret.slots = append(ret.slots, Slot{
			start:  b,
			length: l,
		})
		b += l
	}

	return ret, nil
}

// Erase destroys the data stored in all slots configured in this partition.
// WARNING: Data Loss!
func (p *Partition) Erase() error {
	klog.Info("Erasing partition")
	borked := false
	for i := range p.slots {
		if err := p.eraseSlot(i); err != nil {
			klog.Warningf("Failed to erase slot %d: %v", i, err)
		}
	}
	if borked {
		return errors.New("failed to erase one or more slots in partition")
	}
	return nil
}

func (p *Partition) eraseSlot(i int) error {
	p.slots[i].mu.Lock()
	defer p.slots[i].mu.Unlock()

	// Invalidate journal since we're erasing data from underneath it.
	p.slots[i].journal = nil

	klog.Infof("Erasing partition slot %d @ block %d len %d blocks", i, p.slots[i].start, p.slots[i].length)
	length := p.slots[i].length * p.dev.BlockSize()
	start := p.slots[i].start
	b := make([]byte, length)
	if _, err := p.dev.WriteBlocks(start, b); err != nil {
		return fmt.Errorf("slot %d occupying blocks [%d, %d): %v", i, start, start+length, err)
	}
	return nil
}

// Open opens the specified slot, returns an error if the slot is out of bounds.
func (p *Partition) Open(slot uint) (*Slot, error) {
	if l := uint(len(p.slots)); slot >= l {
		return nil, fmt.Errorf("invalid slot %d (partition has %d slots)", slot, l)
	}
	s := &p.slots[slot]
	klog.V(2).Infof("Opening slot %d", slot)
	if err := s.Open(p.dev); err != nil {
		klog.V(2).Infof("Failed to open slot %d: %v", slot, err)
	}

	return s, nil
}

// NumSlots returns the number of slots configured in this partition.
func (p *Partition) NumSlots() int {
	return len(p.slots)
}

// Slot represents the current data in a slot.
type Slot struct {
	// mu guards access to this Slot.
	mu sync.RWMutex

	// start and length define the on-storage blocks assigned to this journal:
	// [start, start+length).
	start, length uint

	// journal is the underlying journal used to store the data in this slot.
	// if it's nil, it hasn't yet been opened and will be opened upon first
	// access.
	journal *Journal
}

// Open prepares the slot for use.
// This method is idempotent and will not return an error if called multiple times.
func (s *Slot) Open(dev BlockReaderWriter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.journal != nil {
		return nil
	}
	j, err := OpenJournal(dev, s.start, s.length)
	if err != nil {
		return fmt.Errorf("failed to open journal: %v", err)
	}
	s.journal = j
	return nil
}

// Read returns the last data successfully written to the slot, along with a token
// which can be used with CheckAndWrite.
func (s *Slot) Read() ([]byte, uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.journal.current.Data, s.journal.current.Revision, nil
}

// Write writes the provided data to the slot.
// Upon successful completion, this data will be returned by future calls to Read
// until another successful Write call is mode.
// If the call to Write fails, future calls to Read will return the previous
// successfully written data, if any.
func (s *Slot) Write(p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.journal.Update(p)
}

// CheckAndWrite behaves like Write, with the exception that it will immediately
// return an error if the slot has been successfully written to since the Read call
// which produced the passed-in token.
func (s *Slot) CheckAndWrite(token uint32, p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.journal.current.Revision != token {
		return errors.New("invalid token, slot updated since then")
	}
	return s.journal.Update(p)
}
