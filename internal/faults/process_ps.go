//go:build unix

package faults

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func pidFromPS(command string) (int, error) {
	out, err := exec.Command("ps", "-axo", "pid,comm").Output()
	if err != nil {
		return 0, fmt.Errorf("resolve command %q: %w", command, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.Contains(fields[1], command) || strings.HasSuffix(fields[1], command) {
			pid, err := strconv.Atoi(fields[0])
			if err != nil {
				continue
			}
			return pid, nil
		}
	}
	return 0, fmt.Errorf("no process matching command %q", command)
}
