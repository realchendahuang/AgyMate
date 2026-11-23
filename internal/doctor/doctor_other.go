//go:build !darwin

package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

// RunChecks executes cross-platform health checks on non-macOS hosts.
func RunChecks(cfg config.Config, instList []instances.Instance, cdpTargets []cdp.TargetStatus) Report {
	var checks []CheckResult

	// 1. Official Antigravity executable check
	mainPath := config.ExpandPath(cfg.Paths.MainAppPath)
	foundApp := ""
	if mainPath != "" {
		if st, err := os.Stat(mainPath); err == nil && !st.IsDir() {
			foundApp = mainPath
		}
	}
	if foundApp == "" {
		for _, cand := range []string{"antigravity", "Antigravity.exe", "Antigravity"} {
			if p, err := exec.LookPath(cand); err == nil && p != "" {
				foundApp = p
				break
			}
		}
	}
	c1 := CheckResult{
		ID:    "app_version",
		Title: "Antigravity 官方客户端",
	}
	if foundApp != "" {
		c1.Status = "ok"
		c1.Summary = "已检测到官方客户端"
		c1.Detail = foundApp
	} else {
		c1.Status = "warn"
		c1.Summary = "未在配置路径或 PATH 中找到 Antigravity"
		c1.Detail = mainPath
	}
	checks = append(checks, c1)

	// 2. Profile health checks (when isolated profiles exist)
	var profiles []instances.Instance
	for _, inst := range instList {
		if inst.Kind == instances.KindProfile {
			profiles = append(profiles, inst)
		}
	}
	if len(profiles) > 0 {
		var broken []string
		var badAuth []string
		for _, inst := range profiles {
			if inst.BrokenSymlinks > 0 {
				broken = append(broken, fmt.Sprintf("%s(%d)", inst.ID, inst.BrokenSymlinks))
			}
			if inst.AuthState != "ok" {
				badAuth = append(badAuth, fmt.Sprintf("%s(%s)", inst.ID, localizeAuthState(inst.AuthState)))
			}
		}

		cLinks := CheckResult{
			ID:        "profile_symlinks",
			Title:     "隔离实例符号链接",
			FixAction: "heal_profiles",
		}
		if len(broken) == 0 {
			cLinks.Status = "ok"
			cLinks.Summary = fmt.Sprintf("已扫描 %d 个隔离实例，0 个损坏符号链接", len(profiles))
		} else {
			cLinks.Status = "warn"
			cLinks.Fixable = true
			cLinks.Summary = fmt.Sprintf("发现 %d 个实例存在损坏链接", len(broken))
			cLinks.Detail = strings.Join(broken, "，")
		}
		checks = append(checks, cLinks)

		cAuth := CheckResult{
			ID:    "profile_oauth",
			Title: "隔离实例 OAuth 刷新令牌状态",
		}
		if len(badAuth) == 0 {
			cAuth.Status = "ok"
			cAuth.Summary = fmt.Sprintf("全部 %d 个隔离实例均持有有效刷新令牌", len(profiles))
		} else {
			cAuth.Status = "warn"
			cAuth.Summary = fmt.Sprintf("%d/%d 个隔离实例待登录授权或缺少刷新令牌", len(badAuth), len(profiles))
			cAuth.Detail = strings.Join(badAuth, "，")
		}
		checks = append(checks, cAuth)
	}

	// 3. Hooks + Plugin MCP App + Sidecar (+ built-in Antigravity Chinese localization engine)
	installed, total, _ := hooks.CheckInstalled(cfg)
	pluginOK, pluginDetail := CheckPlugin()
	sidecarOK, detail := CheckSidecar("")
	zhSource, _ := cdp.ResolveZhSource(cfg.CDP.ZhScriptPath)
	zhNeeded := cfg.CDP.Enabled && cfg.CDP.ZhInject
	zhOK := !zhNeeded || cdp.HasBuiltinZhScript() || zhSource != ""
	pluginLabel := "未安装"
	if pluginOK {
		pluginLabel = "已就绪"
	}
	scLabel := "未注册"
	if sidecarOK {
		scLabel = "已注册"
	}
	missingCDP, cdpNote := describeMissingCDP(cfg, instList, cdpTargets, foundApp != "", false)
	missingSuffix := ""
	if len(missingCDP) > 0 {
		missingSuffix = fmt.Sprintf(" · 未开 CDP: %s", strings.Join(missingCDP, ", "))
	}
	extraDetail := fmt.Sprintf("插件: %s | Sidecar: %s", pluginDetail, detail)
	if zhNeeded {
		extraDetail += fmt.Sprintf(" | 汉化引擎: %s", zhSource)
	}
	extraDetail += cdpNote

	fixable := installed != total || !pluginOK || !sidecarOK || !zhOK
	cHooks := CheckResult{
		ID:        "companion_hooks_sidecar",
		Title:     "Hooks · 插件 MCP App · Sidecar",
		Status:    "ok",
		Summary:   fmt.Sprintf("Hooks %d/%d · 插件 MCP App %s · Sidecar %s · CDP 窗口 %d 个%s", installed, total, pluginLabel, scLabel, len(cdpTargets), missingSuffix),
		Detail:    extraDetail,
		Fixable:   fixable,
		FixAction: "install_integrations",
	}
	if fixable || len(missingCDP) > 0 {
		cHooks.Status = "warn"
	}
	checks = append(checks, cHooks)

	rep := Report{
		CheckedAt: time.Now().Unix(),
		Checks:    checks,
	}
	for _, c := range checks {
		switch c.Status {
		case "ok":
			rep.OKCount++
		case "warn":
			rep.WarnCount++
		case "error":
			rep.ErrorCount++
		}
	}
	return rep
}

// FixSafe applies cross-platform repairs (profile healing, hooks & sidecar).
func FixSafe(cfg config.Config, cliBin string) FixReport {
	var rep FixReport
	for _, inst := range instances.DiscoverAll(cfg) {
		if inst.Kind != instances.KindProfile {
			continue
		}
		hrep, err := instances.HealProfile(cfg, inst.ID, false)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("修复实例 %s 失败: %v", inst.ID, err))
		} else if len(hrep.LocksCleaned) > 0 || len(hrep.SymlinksRepaired) > 0 {
			rep.Fixed = append(rep.Fixed, fmt.Sprintf("%s: 清理锁 %d 项 / 修复链接 %d 项", inst.ID, len(hrep.LocksCleaned), len(hrep.SymlinksRepaired)))
		}
	}
	if updated, err := hooks.InstallHooks(cfg, cliBin); err != nil {
		rep.Errors = append(rep.Errors, "安装 Hooks 失败: "+err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, fmt.Sprintf("无损更新 Hooks（%d 个目标）", len(updated)))
	}
	if paths, err := EnsureSidecarForProfiles(cfg, cliBin); err != nil {
		rep.Errors = append(rep.Errors, "注册 Sidecar 失败: "+err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, fmt.Sprintf("已注册 Sidecar（%d 个目标）: %s", len(paths), paths[0]))
	}
	return rep
}
