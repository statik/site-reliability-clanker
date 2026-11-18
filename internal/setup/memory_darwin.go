package setup

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func machineMemory() (uint64, error) {
	memory, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, fmt.Errorf("read the memory size of this machine: %w", err)
	}
	return memory, nil
}
