// Package doctor inspects and repairs environment health across official Antigravity clients,
// isolated profiles, Hooks, CDP integration, and Sidecar.
package doctor

import (
	"fmt"
	"strings"

	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

// CheckResult describes one diagnostic item produced by the local health doctor.
type CheckResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"` // ok | warn | error
	Summary   string `json:"summary"`
	Detail    string `json:"detail,omitempty"`
	Fixable   bool   `json:"fixable"`
	FixAction string `json:"fixAction,omitempty"`
}

// Report aggregates all health checks plus installed Antigravity client versions.
type Report struct {
	CheckedAt   int64         `json:"checkedAt"`
	MainVersion string        `json:"mainVersion,omitempty"`
	IDEVersion  string        `json:"ideVersion,omitempty"`
	OKCount     int           `json:"okCount"`
	WarnCount   int           `json:"warnCount"`
	ErrorCount  int           `json:"errorCount"`
	Checks      []CheckResult `json:"checks"`
}

// FixReport summarizes automated repairs executed by FixSafe.
type FixReport struct {
	Fixed   []string `json:"fixed,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

func localizeAuthState(state string) string {
	switch state {
	case "ok":
		return "正常"
	case "missing":
		return "未登录"
	case "expired":
		return "已过期"
	case "no_refresh_token":
		return "缺少刷新令牌"
	case "keychain_managed":
		return "系统钥匙串托管"
	default:
		if state == "" {
			return "未知"
		}
		return state
	}
}

// describeMissingCDP detects running Antigravity instances that lack an active DevToolsActivePort
// when CDP features (window HUD or Chinese localization) are enabled.
func describeMissingCDP(cfg config.Config, instList []instances.Instance, cdpTargets []cdp.TargetStatus, hasApp bool, isDarwin bool) (missingIDs []string, note string) {
	if !cfg.CDP.Enabled || (!cfg.CDP.WindowHUD && !cfg.CDP.ZhInject) {
		return nil, ""
	}
	var mainMissing []string
	var profileMissing []string
	for _, inst := range instList {
		if !inst.Running || inst.CDPPort > 0 {
			continue
		}
		missingIDs = append(missingIDs, inst.ID)
		if inst.Kind == instances.KindMain || inst.Kind == instances.KindIDE {
			mainMissing = append(mainMissing, inst.ID)
		} else {
			profileMissing = append(profileMissing, inst.ID)
		}
	}
	if len(missingIDs) > 0 {
		var hints []string
		if len(mainMissing) > 0 {
			if isDarwin {
				hints = append(hints, "官方客户端请退出 (⌘Q) 后通过 `agymate launch` 或 ~/Applications/Antigravity (AgyMate).app 启动")
			} else {
				hints = append(hints, "官方客户端请退出后通过 `agymate launch` 启动")
			}
		}
		if len(profileMissing) > 0 {
			hints = append(hints, fmt.Sprintf("隔离实例 (%s) 重启后将自动附加 --remote-debugging-port=0", strings.Join(profileMissing, ", ")))
		}
		return missingIDs, fmt.Sprintf(" | 未开 CDP 端口的运行中实例: %s（%s）", strings.Join(missingIDs, ", "), strings.Join(hints, "；"))
	}
	if len(cdpTargets) == 0 && hasApp {
		if isDarwin {
			return nil, " | Antigravity 2.33+ 已关闭默认 CDP 端口，需通过 `agymate launch`（或 ~/Applications/Antigravity (AgyMate).app）启动官方客户端以启用右上角悬浮窗与中文汉化，亦可直接使用原生插件 MCP App"
		}
		return nil, " | Antigravity 2.33+ 需通过 `agymate launch` 启动以开启浮窗与中文汉化"
	}
	return nil, ""
}
