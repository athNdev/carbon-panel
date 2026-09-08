package utils

import (
	"testing"
)

func TestCalculateMemoryAllocation(t *testing.T) {
	tests := []struct {
		allocatedMB int
	}{
		{allocatedMB: 2048},
		{allocatedMB: 4096},
		{allocatedMB: 8192},
		{allocatedMB: 16384},
	}

	for _, tt := range tests {
		alloc := CalculateMemoryAllocation(tt.allocatedMB)
		
		// Guard condition: Container limit must exceed MaxMemory by >= 25% + 384MB
		minRequiredLimit := int(float64(alloc.MaxMemoryMB)*1.25) + 384
		if alloc.ContainerLimitMB < minRequiredLimit {
			t.Errorf("Allocated %dMB: Container limit %dMB does not satisfy >= 25%% + 384MB over MaxHeap %dMB (minimum required: %dMB)",
				tt.allocatedMB, alloc.ContainerLimitMB, alloc.MaxMemoryMB, minRequiredLimit)
		}

		if alloc.InitMemoryMB <= 0 || alloc.InitMemoryMB > alloc.MaxMemoryMB {
			t.Errorf("Allocated %dMB: Invalid init memory %dMB (max: %dMB)", tt.allocatedMB, alloc.InitMemoryMB, alloc.MaxMemoryMB)
		}
	}
}

func TestParseMemoryMB(t *testing.T) {
	if val, err := ParseMemoryMB("4096M"); err != nil || val != 4096 {
		t.Fatalf("expected 4096, got %d, err: %v", val, err)
	}
	if val, err := ParseMemoryMB("4G"); err != nil || val != 4096 {
		t.Fatalf("expected 4096, got %d, err: %v", val, err)
	}
	if val, err := ParseMemoryMB("2048"); err != nil || val != 2048 {
		t.Fatalf("expected 2048, got %d, err: %v", val, err)
	}
}
