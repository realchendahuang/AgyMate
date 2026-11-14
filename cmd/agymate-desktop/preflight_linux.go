//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func linuxWebkitRuntimeAvailable() bool {
	if p, err := exec.LookPath("ldconfig"); err == nil && p != "" {
		out, _ := exec.Command(p, "-p").CombinedOutput()
		if len(out) > 0 {
			return true
		}
	}
	return false
}

func runPreflight() {
	if !linuxWebkitRuntimeAvailable() {
		fmt.Fprintln(os.Stderr, "agymate-desktop: WebKitGTK runtime check skipped")
	}
}
