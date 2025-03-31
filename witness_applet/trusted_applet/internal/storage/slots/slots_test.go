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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/transparency-dev/armored-witness-applet/trusted_applet/internal/storage/testonly"
)

func TestOpenPartition(t *testing.T) {
	type slotGeo struct {
		Start  uint
		Length uint
	}
	toSlotGeo := func(in []Slot) []slotGeo {
		r := make([]slotGeo, len(in))
		for i := range in {
			r[i] = slotGeo{
				Start:  in[i].start,
				Length: in[i].length,
			}
		}
		return r
	}

	const devBlockSize = 32

	for _, test := range []struct {
		name      string
		geo       Geometry
		wantErr   bool
		wantSlots []slotGeo
	}{
		{
			name: "free space remaining",
			geo: Geometry{
				Start:       10,
				Length:      10,
				SlotLengths: []uint{1, 1, 2, 4},
			},
			wantSlots: []slotGeo{
				{Start: 10, Length: 1},
				{Start: 11, Length: 1},
				{Start: 12, Length: 2},
				{Start: 14, Length: 4},
			},
		}, {
			name: "fully allocated",
			geo: Geometry{
				Start:       10,
				Length:      10,
				SlotLengths: []uint{1, 1, 2, 4, 2},
			},
			wantSlots: []slotGeo{
				{Start: 10, Length: 1},
				{Start: 11, Length: 1},
				{Start: 12, Length: 2},
				{Start: 14, Length: 4},
				{Start: 18, Length: 2},
			},
		}, {
			name: "over allocated",
			geo: Geometry{
				Start:       10,
				Length:      10,
				SlotLengths: []uint{1, 1, 2, 4, 3},
			},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dev := testonly.NewMemDev(t, devBlockSize)
			p, err := OpenPartition(dev, test.geo)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Fatalf("Got %v, wantErr %t", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if diff := cmp.Diff(toSlotGeo(p.slots), test.wantSlots); diff != "" {
				t.Fatalf("Got diff: %s", diff)
			}
		})
	}
}

func memPartition(t *testing.T) (*Partition, *testonly.MemDev) {
	t.Helper()
	md := testonly.NewMemDev(t, 32)
	geo := Geometry{
		Start:       10,
		Length:      10,
		SlotLengths: []uint{1, 1, 2, 4},
	}
	p, err := OpenPartition(md, geo)
	if err != nil {
		t.Fatalf("Failed to create mem partition: %v", err)
	}
	return p, md
}

func TestOpenSlot(t *testing.T) {
	p, _ := memPartition(t)
	for _, test := range []struct {
		name    string
		slot    uint
		wantErr bool
	}{
		{
			name: "works",
			slot: 0,
		}, {
			name:    "invalid slot: too big",
			slot:    uint(len(p.slots)),
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := p.Open(test.slot)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Fatalf("Failed to open slot: %v", err)
			}
		})
	}
}

func TestErase(t *testing.T) {
	p, _ := memPartition(t)

	// Create some data in each slot
	for i := 0; i < p.NumSlots(); i++ {
		s, err := p.Open(uint(i))
		if err != nil {
			t.Fatalf("Failed to open slot %d: %v", i, err)
		}
		s.Write([]byte(fmt.Sprintf("data for slot %d", i)))
	}

	// Verify slots contain _something_
	for i := 0; i < p.NumSlots(); i++ {
		d, r := openAndRead(t, p, uint(i))
		if got, want := r, uint32(1); got != want {
			t.Fatalf("Got data with revision %d, want %d", got, want)
		}
		if len(d) == 0 {
			t.Fatal("Got unexpected zero length data")
		}
	}

	// Erase all the slots
	if err := p.Erase(); err != nil {
		t.Fatalf("Failed to erase partition: %v", err)
	}

	// All slots should now be empty
	for i := 0; i < p.NumSlots(); i++ {
		d, r := openAndRead(t, p, uint(i))
		if got, want := r, uint32(0); got != want {
			t.Fatalf("Got data with revision %d, want %d", got, want)
		}
		if len(d) != 0 {
			t.Fatalf("Got unexpected data: %x", d)
		}
	}
}

func openAndRead(t *testing.T, p *Partition, i uint) ([]byte, uint32) {
	t.Helper()
	s, err := p.Open(uint(i))
	if err != nil {
		t.Fatalf("Failed to open slot %d: %v", i, err)
	}
	d, r, err := s.Read()
	if err != nil {
		t.Fatalf("Failed to read slot %d: %v", i, err)
	}
	return d, r
}
