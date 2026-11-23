//go:build !windows

package instances

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func listProcesses() []procEntry {
	// POSIX "-eo pid=,ppid=,args=" works consistently across macOS and Linux.
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,args=").Output()
	if err != nil {
		// Fallback for older BSD/minimal environments.
		out, err = exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
		if err != nil {
			return nil
		}
	}
	return parseUnixPSOutput(string(out))
}

func parseUnixPSOutput(raw string) []procEntry {
	lines := strings.Split(raw, "\n")
	res := make([]procEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		// Advance past pid and ppid fields cleanly without matching digits inside pid.
		rest := strings.TrimSpace(line[len(fields[0]):])
		rest = strings.TrimSpace(rest[len(fields[1]):])
		res = append(res, procEntry{PID: pid, PPID: ppid, Command: rest})
	}
	return res
}

// IsPIDAlive reports whether a process with the given PID exists.
func IsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func stopProcessTree(pid int, force bool) error {
	if pid <= 0 || !IsPIDAlive(pid) {
		return nil
	}
	// Capture process group ID before the leader exits so child language_server/helper processes can be reaped.
	pgid, pgidErr := syscall.Getpgid(pid)

	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !IsPIDAlive(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if pgidErr == nil && pgid == pid && pgid > 1 {
		_ = syscall.Kill(-pgid, sig)
		if IsPIDAlive(pid) {
			grpDeadline := time.Now().Add(1 * time.Second)
			for time.Now().Before(grpDeadline) {
				if !IsPIDAlive(pid) {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	if IsPIDAlive(pid) && force {
		if pgidErr == nil && pgid == pid && pgid > 1 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		killDeadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(killDeadline) {
			if !IsPIDAlive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	if IsPIDAlive(pid) {
		return fmt.Errorf("process %d is still running", pid)
	}
	return nil
}
