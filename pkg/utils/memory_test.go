// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package utils

import (
	"math"
	"strconv"
	"testing"
)

// TestCalculateMemoryAllocationHeadroomInvariant locks the guarantee that keeps
// the JVM alive under cgroups: the container limit must exceed -Xmx by
// HeadroomRatio plus the off-heap buffer. A 4G container with -Xmx4G is
// OOM-killed (exit 137) while the heap still has free space.
func TestCalculateMemoryAllocationHeadroomInvariant(t *testing.T) {
	rams := []int{256, 512, 640, 641, 1024, 2048, 4096, 8192, 16384, 32768}
	for _, ram := range rams {
		a := CalculateMemoryAllocation(ram)

		if a.MaxMemoryMB < 256 {
			t.Errorf("ram=%d: max heap %d below the 256MB floor", ram, a.MaxMemoryMB)
		}
		if a.InitMemoryMB < 128 {
			t.Errorf("ram=%d: init heap %d below the 128MB floor", ram, a.InitMemoryMB)
		}
		if a.InitMemoryMB > a.MaxMemoryMB {
			t.Errorf("ram=%d: init heap %d exceeds max heap %d", ram, a.InitMemoryMB, a.MaxMemoryMB)
		}

		required := int(math.Ceil(float64(a.MaxMemoryMB)*HeadroomRatio)) + OffHeapBufferMB
		if a.ContainerLimitMB < required {
			t.Errorf("ram=%d: container limit %d < required %d (max heap %d); would OOM-kill", ram, a.ContainerLimitMB, required, a.MaxMemoryMB)
		}
		if a.ContainerLimitMB < ram {
			t.Errorf("ram=%d: container limit %d is below the requested allocation", ram, a.ContainerLimitMB)
		}
		if a.ContainerLimitBytes != int64(a.ContainerLimitMB)*1024*1024 {
			t.Errorf("ram=%d: ContainerLimitBytes %d does not match ContainerLimitMB %d", ram, a.ContainerLimitBytes, a.ContainerLimitMB)
		}
		if a.MaxMemoryStr != strconv.Itoa(a.MaxMemoryMB)+"M" {
			t.Errorf("ram=%d: MaxMemoryStr %q inconsistent with MaxMemoryMB %d", ram, a.MaxMemoryStr, a.MaxMemoryMB)
		}
	}
}

// TestCalculateMemoryAllocationDefaults guards the non-positive input path.
func TestCalculateMemoryAllocationDefaults(t *testing.T) {
	for _, ram := range []int{0, -1, -4096} {
		a := CalculateMemoryAllocation(ram)
		if a.MaxMemoryMB <= 0 {
			t.Errorf("ram=%d: expected a sane default heap, got %d", ram, a.MaxMemoryMB)
		}
		if a.ContainerLimitMB <= a.MaxMemoryMB {
			t.Errorf("ram=%d: default allocation lost the OOM headroom", ram)
		}
	}
}

// TestCalculateMemoryAllocationBranches checks the two sizing branches: small
// allocations use a 0.65 multiplier, larger ones subtract the off-heap buffer
// and divide by the headroom ratio.
func TestCalculateMemoryAllocationBranches(t *testing.T) {
	smallRAM := 512
	small := CalculateMemoryAllocation(smallRAM)
	if want := int(float64(smallRAM) * 0.65); small.MaxMemoryMB != want {
		t.Errorf("small branch: max heap %d, want %d", small.MaxMemoryMB, want)
	}

	large := CalculateMemoryAllocation(4096)
	if want := int(math.Floor(float64(4096-OffHeapBufferMB) / HeadroomRatio)); large.MaxMemoryMB != want {
		t.Errorf("large branch: max heap %d, want %d", large.MaxMemoryMB, want)
	}
}

// TestEnsureMemoryHeadroom covers the custom MAX_MEMORY path: a configured limit
// below the required headroom must be raised, one above it must be respected.
func TestEnsureMemoryHeadroomEnforcesFloor(t *testing.T) {
	const maxHeap = 2048
	required := int(math.Ceil(float64(maxHeap)*HeadroomRatio)) + OffHeapBufferMB

	got, gotBytes := EnsureMemoryHeadroom(maxHeap, 1024)
	if got != required {
		t.Errorf("undersized limit: got %d, want raised to %d", got, required)
	}
	if gotBytes != int64(got)*1024*1024 {
		t.Errorf("byte conversion mismatch: %d", gotBytes)
	}

	if got, _ := EnsureMemoryHeadroom(maxHeap, 8192); got != 8192 {
		t.Errorf("sufficient limit should be kept, got %d", got)
	}
}

func TestParseMemoryMB(t *testing.T) {
	valid := []struct {
		in   string
		want int
	}{
		{"4096M", 4096},
		{"4G", 4096},
		{"2048", 2048},
		{"4096m", 4096},
		{"4g", 4096},
		{" 4096M ", 4096},
		{"1G", 1024},
		{"1", 1},
	}
	for _, c := range valid {
		got, err := ParseMemoryMB(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseMemoryMB(%q) = %d, %v; want %d, nil", c.in, got, err, c.want)
		}
	}

	invalid := []string{"", "   ", "abc", "G", "M", "4T", "-1G", "-1", "0", "0M", "1.5G"}
	for _, c := range invalid {
		if got, err := ParseMemoryMB(c); err == nil {
			t.Errorf("ParseMemoryMB(%q) = %d, nil; want an error", c, got)
		}
	}
}
