//go:build !darwin

package instances

import "fmt"

// FocusWindow is a no-op outside macOS.
func FocusWindow(inst Instance) error {
	return fmt.Errorf("focus window unsupported on this platform (%s)", inst.ID)
}
