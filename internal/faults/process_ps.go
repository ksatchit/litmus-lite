//go:build unix

package faults

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func pidsFromPS(command string) ([]int, error) {
	out, err := exec.Command("ps", "-axo", "pid,comm").Output()
	if err != nil {
		return nil, fmt.Errorf("resolve command %q: %w", command, err)
	}
	var found []int
	seen := map[int]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "PID" {
			continue
		}
		if !commandMatches(fields[1], command) {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		found = append(found, pid)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no process matching command %q", command)
	}
	return found, nil
}
