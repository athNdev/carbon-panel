package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	// OffHeapBufferMB is the minimum off-heap buffer (metaspace, thread stacks, direct buffers, OS overhead)
	OffHeapBufferMB = 384
	// HeadroomRatio is the 25-30% headroom multiplier (1.28 represents 28% headroom)
	HeadroomRatio = 1.28
)

// MemoryAllocation holds calculated JVM and container memory limits to prevent cgroup OOM kills (exit 137)
type MemoryAllocation struct {
	InitMemoryMB        int
	MaxMemoryMB         int
	ContainerLimitMB    int
	InitMemoryStr       string
	MaxMemoryStr        string
	ContainerLimitBytes int64
}

// CalculateMemoryAllocation calculates dynamic -Xms and -Xmx from total allocated RAM
// and guarantees container memory limit exceeds JVM -Xmx by 25-30% plus 384MB off-heap buffer.
func CalculateMemoryAllocation(allocatedRAMMB int) MemoryAllocation {
	if allocatedRAMMB <= 0 {
		allocatedRAMMB = 2048
	}

	var maxHeapMB int
	if allocatedRAMMB > (OffHeapBufferMB + 256) {
		maxHeapMB = int(math.Floor(float64(allocatedRAMMB-OffHeapBufferMB) / HeadroomRatio))
	} else {
		maxHeapMB = int(float64(allocatedRAMMB) * 0.65)
	}

	if maxHeapMB < 256 {
		maxHeapMB = 256
	}

	initHeapMB := int(float64(maxHeapMB) * 0.5)
	if initHeapMB < 128 {
		initHeapMB = 128
	}

	guardedContainerLimitMB := int(math.Ceil(float64(maxHeapMB)*HeadroomRatio)) + OffHeapBufferMB
	if guardedContainerLimitMB < allocatedRAMMB {
		guardedContainerLimitMB = allocatedRAMMB
	}

	return MemoryAllocation{
		InitMemoryMB:        initHeapMB,
		MaxMemoryMB:         maxHeapMB,
		ContainerLimitMB:    guardedContainerLimitMB,
		InitMemoryStr:       fmt.Sprintf("%dM", initHeapMB),
		MaxMemoryStr:        fmt.Sprintf("%dM", maxHeapMB),
		ContainerLimitBytes: int64(guardedContainerLimitMB) * 1024 * 1024,
	}
}

// EnsureMemoryHeadroom validates that a container memory limit provides the required headroom over maxHeapMB.
// Returns the safe container memory limit in MB and bytes.
func EnsureMemoryHeadroom(maxHeapMB int, configuredContainerMB int) (int, int64) {
	requiredContainerMB := int(math.Ceil(float64(maxHeapMB)*HeadroomRatio)) + OffHeapBufferMB
	if configuredContainerMB < requiredContainerMB {
		configuredContainerMB = requiredContainerMB
	}
	return configuredContainerMB, int64(configuredContainerMB) * 1024 * 1024
}

// ParseMemoryMB parses memory string values (e.g. "4096M", "4G", "4096") into megabytes
func ParseMemoryMB(memStr string) (int, error) {
	memStr = strings.TrimSpace(memStr)
	if memStr == "" {
		return 0, fmt.Errorf("empty memory string")
	}
	unit := strings.ToUpper(memStr[len(memStr)-1:])
	if unit == "G" {
		val, err := strconv.Atoi(strings.TrimSpace(memStr[:len(memStr)-1]))
		if err != nil {
			return 0, err
		}
		return val * 1024, nil
	}
	if unit == "M" {
		val, err := strconv.Atoi(strings.TrimSpace(memStr[:len(memStr)-1]))
		if err != nil {
			return 0, err
		}
		return val, nil
	}
	return strconv.Atoi(memStr)
}
