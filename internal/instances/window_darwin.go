//go:build darwin

package instances

import (
	"fmt"
	"os/exec"
)

// FocusWindow brings an Antigravity instance window to the foreground on macOS.
func FocusWindow(inst Instance) error {
	if inst.MainPID > 0 && IsPIDAlive(inst.MainPID) {
		script := fmt.Sprintf(
			`tell application "System Events" to set frontmost of first process whose unix id is %d to true`,
			inst.MainPID,
		)
		if err := exec.Command("osascript", "-e", script).Run(); err == nil {
			return nil
		}
	}
	if inst.Kind == KindMain || inst.Kind == KindIDE {
		if inst.AppPath != "" {
			return exec.Command("open", "-a", inst.AppPath).Run()
		}
	}
	return fmt.Errorf("instance %q has no active window PID to focus", inst.ID)
}
