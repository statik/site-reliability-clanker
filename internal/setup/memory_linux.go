package setup

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func machineMemory() (uint64, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0, fmt.Errorf("read the memory size of this machine: %w", err)
	}
	return uint64(info.Totalram) * uint64(info.Unit), nil
}
