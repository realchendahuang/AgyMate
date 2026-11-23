//go:build darwin

package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

const (
	mainBundleID = "com.google.antigravity"
	lsregister   = "/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Support/lsregister"
)

var (
	mdatRe             = regexp.MustCompile(`"mdat"<timedate>=0x[0-9A-Fa-f]+\s+"([^"]+)"`)
	safeSchemeOrBundle = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
)

func parseLaunchServicesHandlerJSON(raw []byte, scheme string) string {
	var doc struct {
		LSHandlers []struct {
			Scheme  string `json:"LSHandlerURLScheme"`
			RoleAll string `json:"LSHandlerRoleAll"`
			Viewer  string `json:"LSHandlerRoleViewer"`
		} `json:"LSHandlers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	var matched string
	for _, h := range doc.LSHandlers {
		if strings.EqualFold(strings.TrimSpace(h.Scheme), scheme) {
			if id := strings.TrimSpace(h.RoleAll); id != "" {
				matched = id
			} else if id := strings.TrimSpace(h.Viewer); id != "" {
				matched = id
			}
		}
	}
	return matched
}

func getURLHandler(scheme string) string {
	scheme = strings.TrimSpace(scheme)
	if !safeSchemeOrBundle.MatchString(scheme) {
		return ""
	}

	// Fast path: read user-level LaunchServices override via plutil (no CGO required).
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		plistPath := filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist")
		if out, err := exec.Command("/usr/bin/plutil", "-convert", "json", "-o", "-", plistPath).Output(); err == nil {
			if id := parseLaunchServicesHandlerJSON(out, scheme); id != "" {
				return id
			}
		}
	}

	// Fallback: query modern AppKit NSWorkspace via built-in macOS JXA (zero CGO / Xcode CLT).
	schemeJSON, _ := json.Marshal(scheme + "://agymate-probe")
	script := fmt.Sprintf(
		`ObjC.import("AppKit"); var u = $.NSWorkspace.sharedWorkspace.URLForApplicationToOpenURL($.NSURL.URLWithString(%s)); u ? $.NSBundle.bundleWithURL(u).bundleIdentifier.js : "";`,
		string(schemeJSON),
	)
	if out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func setURLHandler(scheme, bundleID string) error {
	scheme = strings.TrimSpace(scheme)
	bundleID = strings.TrimSpace(bundleID)
	if !safeSchemeOrBundle.MatchString(scheme) || !safeSchemeOrBundle.MatchString(bundleID) {
		return fmt.Errorf("invalid scheme %q or bundleID %q", scheme, bundleID)
	}
	schemeJSON, _ := json.Marshal(scheme)
	bundleJSON, _ := json.Marshal(bundleID)
	script := fmt.Sprintf(
		`ObjC.import("CoreServices"); String($.LSSetDefaultHandlerForURLScheme($(%s), $(%s)));`,
		string(schemeJSON),
		string(bundleJSON),
	)
	out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("LSSetDefaultHandlerForURLScheme(%s -> %s) failed: %w (%s)", scheme, bundleID, err, strings.TrimSpace(string(out)))
	}
	codeStr := strings.TrimSpace(string(out))
	if code, perr := strconv.Atoi(codeStr); perr == nil && code != 0 {
		return fmt.Errorf("LSSetDefaultHandlerForURLScheme(%s -> %s) status=%d", scheme, bundleID, code)
	}
	return nil
}

func parseKeychainMdat(output string) string {
	m := mdatRe.FindStringSubmatch(output)
	if len(m) < 2 {
		return ""
	}
	raw := strings.TrimRight(m[1], "\\0")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if ts, err := time.Parse("20060102150405Z", raw); err == nil {
		return ts.Local().Format("2006-01-02 15:04:05")
	}
	return raw
}

// RunChecks executes the 6-point local health audit across official Antigravity clients, profiles, and integrations.
func RunChecks(cfg config.Config, instList []instances.Instance, cdpTargets []cdp.TargetStatus) Report {
	mainPath := config.ExpandPath(cfg.Paths.MainAppPath)
	idePath := config.ExpandPath(cfg.Paths.IDEAppPath)
	mainVer := instances.ReadAppBundleVersion(mainPath)
	ideVer := instances.ReadAppBundleVersion(idePath)

	rep := Report{
		CheckedAt:   time.Now().Unix(),
		MainVersion: mainVer,
		IDEVersion:  ideVer,
	}

	var checks []CheckResult
	home, _ := os.UserHomeDir()
	var profiles []instances.Instance
	for _, inst := range instList {
		if inst.Kind == instances.KindProfile {
			profiles = append(profiles, inst)
		}
	}

	// 1. Official Antigravity app detection
	c1 := CheckResult{
		ID:      "app_version",
		Title:   "Antigravity 官方客户端",
		Fixable: false,
	}
	switch {
	case mainVer == "" && ideVer == "":
		c1.Status = "error"
		c1.Summary = "未检测到官方 Antigravity 客户端"
		c1.Detail = fmt.Sprintf("配置路径: %s", mainPath)
	case mainVer != "":
		c1.Status = "ok"
		c1.Summary = fmt.Sprintf("Antigravity %s", mainVer)
		if ideVer != "" {
			c1.Detail = "Antigravity IDE: " + ideVer
		} else {
			c1.Detail = mainPath
		}
	default:
		c1.Status = "ok"
		c1.Summary = fmt.Sprintf("Antigravity IDE %s", ideVer)
		c1.Detail = idePath
	}
	checks = append(checks, c1)

	// 2. macOS URL Scheme routing (official antigravity:// only)
	agyScheme := getURLHandler("antigravity")
	c2 := CheckResult{
		ID:        "url_scheme",
		Title:     "URL Scheme OAuth 回调路由",
		FixAction: "fix_url_scheme",
	}
	if strings.EqualFold(agyScheme, mainBundleID) {
		c2.Status = "ok"
		c2.Summary = "antigravity:// 路由正常"
		c2.Detail = fmt.Sprintf("antigravity → %s", agyScheme)
	} else {
		c2.Status = "warn"
		c2.Fixable = true
		c2.Summary = "antigravity:// 协议处理器偏移，可能影响 OAuth 登录回跳"
		shownScheme := agyScheme
		if shownScheme == "" {
			shownScheme = "未绑定"
		}
		c2.Detail = fmt.Sprintf("当前绑定: %s（期望: %s）", shownScheme, mainBundleID)
	}
	checks = append(checks, c2)

	// 3. Read-only macOS Keychain metadata check (NEVER passes -w!)
	c3 := CheckResult{
		ID:      "keychain_main",
		Title:   "默认客户端 Keychain 凭证（只读元数据）",
		Fixable: false,
	}
	cmd := exec.Command("security", "find-generic-password", "-s", "gemini", "-a", "antigravity")
	out, kerr := cmd.CombinedOutput()
	if kerr == nil {
		mdat := parseKeychainMdat(string(out))
		c3.Status = "ok"
		if mdat != "" {
			c3.Summary = "Keychain 凭证完好（最近更新: " + mdat + "）"
		} else {
			c3.Summary = "Keychain 凭证项存在（gemini / antigravity）"
		}
		c3.Detail = "仅校验只读元数据（绝不使用 -w 导出密钥），不触碰默认客户端登录态"
	} else {
		c3.Status = "warn"
		c3.Summary = "未在 Keychain 找到 gemini / antigravity 元数据条目"
		c3.Detail = strings.TrimSpace(string(out))
	}
	checks = append(checks, c3)

	// 4. Isolated profile broken symlinks & macOS launcher bundles
	var brokenProfiles []string
	var missingApps []string
	for _, inst := range profiles {
		if inst.BrokenSymlinks > 0 {
			brokenProfiles = append(brokenProfiles, fmt.Sprintf("%s(%d)", inst.ID, inst.BrokenSymlinks))
		}
		appStub := filepath.Join(home, "Applications", fmt.Sprintf("Antigravity (%s).app", inst.ID))
		if _, err := os.Stat(appStub); err != nil {
			missingApps = append(missingApps, inst.ID)
		}
	}
	c4 := CheckResult{
		ID:        "profile_symlinks",
		Title:     "隔离实例符号链接与启动器",
		FixAction: "heal_profiles",
	}
	switch {
	case len(profiles) == 0:
		c4.Status = "ok"
		c4.Summary = "暂无隔离实例（单实例模式无需沙盒）"
	case len(brokenProfiles) == 0 && len(missingApps) == 0:
		c4.Status = "ok"
		c4.Summary = fmt.Sprintf("已扫描 %d 个隔离实例，0 个损坏符号链接 / 缺失启动器", len(profiles))
	default:
		c4.Status = "warn"
		c4.Fixable = true
		c4.Summary = fmt.Sprintf("发现异常（损坏链接: %d，缺失启动器: %d）", len(brokenProfiles), len(missingApps))
		c4.Detail = fmt.Sprintf("损坏链接: [%s] · 缺失启动器: [%s]", strings.Join(brokenProfiles, ", "), strings.Join(missingApps, ", "))
	}
	checks = append(checks, c4)

	// 5. Isolated profile OAuth refresh_token health
	var okAuth, badAuth []string
	for _, inst := range profiles {
		if inst.AuthState == "ok" {
			okAuth = append(okAuth, inst.ID)
		} else {
			badAuth = append(badAuth, fmt.Sprintf("%s(%s)", inst.ID, localizeAuthState(inst.AuthState)))
		}
	}
	c5 := CheckResult{
		ID:      "profile_oauth",
		Title:   "隔离实例 OAuth 刷新令牌状态",
		Fixable: false,
	}
	if len(profiles) == 0 {
		c5.Status = "ok"
		c5.Summary = "暂无隔离实例"
	} else if len(badAuth) == 0 {
		c5.Status = "ok"
		c5.Summary = fmt.Sprintf("全部 %d 个隔离实例均持有有效刷新令牌", len(okAuth))
		c5.Detail = "兼容 JSON OAuth 与旧版 JWT 凭据格式"
	} else {
		c5.Status = "warn"
		c5.Summary = fmt.Sprintf("%d/%d 个隔离实例待登录授权或缺少刷新令牌", len(badAuth), len(profiles))
		c5.Detail = strings.Join(badAuth, "，")
	}
	checks = append(checks, c5)

	// 6. Hooks + Plugin MCP App + Sidecar (+ optional CDP inject script when explicitly enabled)
	installedHooks, totalHooks, missingHooks := hooks.CheckInstalled(cfg)
	pluginOK, pluginDetail := CheckPlugin()
	sidecarOK, sidecarDetail := CheckSidecar("")
	zhScriptPath := config.ExpandPath(cfg.CDP.ZhScriptPath)
	_, zhStatErr := os.Stat(zhScriptPath)
	zhNeeded := cfg.CDP.Enabled && cfg.CDP.ZhInject
	zhOK := !zhNeeded || zhStatErr == nil

	c6 := CheckResult{
		ID:        "companion_hooks_sidecar",
		Title:     "Hooks · 插件 MCP App · Sidecar",
		FixAction: "install_integrations",
	}
	zhInjectedCount := 0
	hudCount := 0
	for _, t := range cdpTargets {
		if t.ZhInjected {
			zhInjectedCount++
		}
		if t.HUDActive {
			hudCount++
		}
	}
	pluginStatus := "未安装"
	if pluginOK {
		pluginStatus = "已就绪"
	}
	cdpNote := ""
	if len(cdpTargets) == 0 && mainVer != "" {
		cdpNote = " | Antigravity 2.33+ 已关闭默认 CDP 端口，推荐使用原生插件 MCP App（左侧栏/侧边栏）"
	}
	if installedHooks == totalHooks && pluginOK && sidecarOK && zhOK {
		c6.Status = "ok"
		if zhNeeded {
			c6.Summary = fmt.Sprintf("Hooks %d/%d · 插件 MCP App %s · Sidecar 已注册 · CDP 窗口 %d 个（汉化 %d / 浮窗 %d）",
				installedHooks, totalHooks, pluginStatus, len(cdpTargets), zhInjectedCount, hudCount)
			c6.Detail = fmt.Sprintf("插件: %s | Sidecar: %s | 汉化脚本: %s%s", pluginDetail, sidecarDetail, zhScriptPath, cdpNote)
		} else {
			c6.Summary = fmt.Sprintf("Hooks %d/%d · 插件 MCP App %s · Sidecar 已注册 · CDP 窗口 %d 个（浮窗 %d）",
				installedHooks, totalHooks, pluginStatus, len(cdpTargets), hudCount)
			c6.Detail = fmt.Sprintf("插件: %s | Sidecar: %s%s", pluginDetail, sidecarDetail, cdpNote)
		}
	} else {
		c6.Status = "warn"
		c6.Fixable = true
		scStatus := "未注册"
		if sidecarOK {
			scStatus = "已注册"
		}
		if zhNeeded {
			zhStatus := "缺失"
			if zhStatErr == nil {
				zhStatus = "已就绪"
			}
			c6.Summary = fmt.Sprintf("Hooks %d/%d · 插件 MCP App %s · Sidecar %s · 汉化脚本%s",
				installedHooks, totalHooks, pluginStatus, scStatus, zhStatus)
			c6.Detail = fmt.Sprintf("待补齐 Hooks: %d 处 | 插件: %s | Sidecar: %s | 脚本: %s%s", len(missingHooks), pluginDetail, sidecarDetail, zhScriptPath, cdpNote)
		} else {
			c6.Summary = fmt.Sprintf("Hooks %d/%d · 插件 MCP App %s · Sidecar %s",
				installedHooks, totalHooks, pluginStatus, scStatus)
			c6.Detail = fmt.Sprintf("待补齐 Hooks: %d 处 | 插件: %s | Sidecar: %s%s", len(missingHooks), pluginDetail, sidecarDetail, cdpNote)
		}
	}
	checks = append(checks, c6)

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
	rep.Checks = checks
	return rep
}

// FixSafe applies non-destructive automated fixes (URL scheme, stopped profile healing, hooks, Sidecar).
// It never touches Main or IDE lifecycles.
func FixSafe(cfg config.Config, cliBin string) FixReport {
	var rep FixReport

	// 1. Repair LaunchServices & official antigravity:// URL Scheme handler
	if _, err := os.Stat(lsregister); err == nil {
		mainPath := config.ExpandPath(cfg.Paths.MainAppPath)
		if mainPath != "" {
			if _, err := os.Stat(mainPath); err == nil {
				_ = exec.Command(lsregister, "-f", mainPath).Run()
			}
		}
	}
	if err := setURLHandler("antigravity", mainBundleID); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, "URL Scheme antigravity:// -> "+mainBundleID)
	}

	// 2. Heal isolated profiles safely (never main or ide)
	instList := instances.DiscoverAll(cfg)
	for _, inst := range instList {
		if inst.Kind != instances.KindProfile {
			continue
		}
		hrep, err := instances.HealProfile(cfg, inst.ID, false)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("修复实例 %s 失败: %v", inst.ID, err))
			continue
		}
		if hrep.SkippedRunning {
			if len(hrep.SymlinksRepaired) > 0 {
				rep.Fixed = append(rep.Fixed, fmt.Sprintf("%s: 修复 %d 个断链（运行中已跳过锁清理）", inst.ID, len(hrep.SymlinksRepaired)))
			} else {
				rep.Skipped = append(rep.Skipped, inst.ID+"（运行中）")
			}
		} else if len(hrep.LocksCleaned) > 0 || len(hrep.SymlinksRepaired) > 0 || len(hrep.CachesPurged) > 0 || hrep.UpdaterSuppressed {
			rep.Fixed = append(rep.Fixed, fmt.Sprintf("%s: 清理锁 %d 项 / 修复链接 %d 项 / 清理缓存 %d 项",
				inst.ID, len(hrep.LocksCleaned), len(hrep.SymlinksRepaired), len(hrep.CachesPurged)))
		}
		// Refresh ~/Applications/Antigravity (<name>).app launcher bundle cleanly
		if err := instances.EnsureMacOSLauncherStub(inst.ID); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("刷新启动器 %s 失败: %v", inst.ID, err))
		} else {
			rep.Fixed = append(rep.Fixed, "刷新启动器: "+inst.ID)
		}
	}

	// 3. Install non-destructive hooks & Sidecar registration
	if updated, err := hooks.InstallHooks(cfg, cliBin); err != nil {
		rep.Errors = append(rep.Errors, "安装 Hooks 失败: "+err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, fmt.Sprintf("无损更新 Hooks（%d 个目标）", len(updated)))
	}
	if scPaths, err := EnsureSidecarForProfiles(cfg, cliBin); err != nil {
		rep.Errors = append(rep.Errors, "注册 Sidecar 失败: "+err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, fmt.Sprintf("已注册 Sidecar（%d 个目标）: %s", len(scPaths), scPaths[0]))
	}
	return rep
}
