//go:build !darwin

package doctor

import (
	"context"
	"fmt"
	"time"

	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

func parseKeychainMdat(_ string) string {
	return ""
}

// RunChecks executes platform-independent health checks on non-macOS hosts.
func RunChecks(cfg config.Config, instList []instances.Instance, cdpTargets []cdp.TargetStatus) Report {
	installed, total, _ := hooks.CheckInstalled(cfg)
	sidecarOK, detail := CheckSidecar("")
	c := CheckResult{
		ID:      "companion_hooks_sidecar",
		Title:   "Hooks · Sidecar · CDP",
		Status:  "ok",
		Summary: fmt.Sprintf("Hooks %d/%d · Sidecar=%v · CDP 目标 %d", installed, total, sidecarOK, len(cdpTargets)),
		Detail:  detail,
		Fixable: installed != total || !sidecarOK,
	}
	okCount, warnCount := 1, 0
	if c.Fixable {
		c.Status = "warn"
		okCount, warnCount = 0, 1
	}
	return Report{
		CheckedAt: time.Now().Unix(),
		OKCount:   okCount,
		WarnCount: warnCount,
		Checks:    []CheckResult{c},
	}
}

// FixSafe applies cross-platform repairs (hooks & sidecar).
func FixSafe(cfg config.Config, cliBin string) FixReport {
	var rep FixReport
	if updated, err := hooks.InstallHooks(cfg, cliBin); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, fmt.Sprintf("hooks updated (%d)", len(updated)))
	}
	if p, err := EnsureSidecar(cliBin); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, "sidecar registered: "+p)
	}
	return rep
}

// SyncBox is only supported on macOS.
func SyncBox(_ context.Context, _ config.Config) (string, error) {
	return "", fmt.Errorf("Antigravity-Box sync is only supported on macOS")
}
