// Package version holds the build-time version string and VCS metadata of AgyMate.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Version, Commit, and BuildTime are overridden at link time via -ldflags
// "-X github.com/realchendahuang/AgyMate/internal/version.<Var>=...".
var (
	Version   = "0.5.1"
	Commit    = ""
	BuildTime = ""
)

// EffectiveCommit returns the injected Commit or falls back to Go build info VCS revision.
func EffectiveCommit() string {
	if c := strings.TrimSpace(Commit); c != "" {
		return c
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

// Full returns a human-readable version string including commit and build timestamp when available.
func Full() string {
	commit := EffectiveCommit()
	bt := strings.TrimSpace(BuildTime)
	switch {
	case commit != "" && bt != "":
		return fmt.Sprintf("%s (%s, %s)", Version, commit, bt)
	case commit != "":
		return fmt.Sprintf("%s+%s", Version, commit)
	case bt != "":
		return fmt.Sprintf("%s (%s)", Version, bt)
	default:
		return Version
	}
}
