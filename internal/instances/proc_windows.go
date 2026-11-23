//go:build windows

package instances

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func listProcesses() []procEntry {
	const psScript = `$p = Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId,CommandLine,Name; if ($p) { $p | ConvertTo-Json -Compress }`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript).Output()
	if err != nil {
		return nil
	}
	return parseWindowsProcessJSON(out)
}

func parseWindowsProcessJSON(raw []byte) []procEntry {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	type winProc struct {
		ProcessID       int    `json:"ProcessId"`
		ParentProcessID int    `json:"ParentProcessId"`
		CommandLine     string `json:"CommandLine"`
		Name            string `json:"Name"`
	}
	var items []winProc
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil
		}
	} else {
		var single winProc
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil
		}
		items = []winProc{single}
	}
	res := make([]procEntry, 0, len(items))
	for _, it := range items {
		if it.ProcessID <= 0 {
			continue
		}
		cmd := strings.TrimSpace(it.CommandLine)
		if cmd == "" {
			cmd = strings.TrimSpace(it.Name)
		}
		if cmd == "" {
			continue
		}
		res = append(res, procEntry{
			PID:     it.ProcessID,
			PPID:    it.ParentProcessID,
			Command: cmd,
		})
	}
	return res
}

// IsPIDAlive reports whether a process with the given PID exists on Windows.
func IsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	pidStr := strconv.Itoa(pid)
	out, err := exec.Command("tasklist", "/FI", "PID eq "+pidStr, "/NH", "/FO", "CSV").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "INFO:") {
			continue
		}
		for _, field := range strings.Split(line, ",") {
			if strings.Trim(field, "\" \r\t") == pidStr {
				return true
			}
		}
	}
	return false
}

func configureDetachedProcess(_ *exec.Cmd) {}

func stopProcessTree(pid int, force bool) error {
	if pid <= 0 || !IsPIDAlive(pid) {
		return nil
	}
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	if out, err := exec.Command("taskkill", args...).CombinedOutput(); err != nil && IsPIDAlive(pid) {
		return fmt.Errorf("taskkill failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !IsPIDAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if IsPIDAlive(pid) {
		return fmt.Errorf("process %d is still running", pid)
	}
	return nil
}
