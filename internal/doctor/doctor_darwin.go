//go:build darwin

package doctor

/*
#cgo LDFLAGS: -framework CoreFoundation -framework CoreServices
#include <CoreFoundation/CoreFoundation.h>
#include <CoreServices/CoreServices.h>
#include <stdlib.h>

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

static char* copyDefaultHandler(const char* scheme) {
	if (!scheme) return NULL;
	CFStringRef cfScheme = CFStringCreateWithCString(kCFAllocatorDefault, scheme, kCFStringEncodingUTF8);
	if (!cfScheme) return NULL;
	CFStringRef bundleID = LSCopyDefaultHandlerForURLScheme(cfScheme);
	CFRelease(cfScheme);
	if (!bundleID) return NULL;
	CFIndex len = CFStringGetLength(bundleID);
	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
	char* out = (char*)malloc((size_t)maxSize);
	if (out && !CFStringGetCString(bundleID, out, maxSize, kCFStringEncodingUTF8)) {
		free(out);
		out = NULL;
	}
	CFRelease(bundleID);
	return out;
}

static int setDefaultHandler(const char* scheme, const char* bundleID) {
	if (!scheme || !bundleID) return -1;
	CFStringRef cfScheme = CFStringCreateWithCString(kCFAllocatorDefault, scheme, kCFStringEncodingUTF8);
	CFStringRef cfBundle = CFStringCreateWithCString(kCFAllocatorDefault, bundleID, kCFStringEncodingUTF8);
	if (!cfScheme || !cfBundle) {
		if (cfScheme) CFRelease(cfScheme);
		if (cfBundle) CFRelease(cfBundle);
		return -1;
	}
	OSStatus st = LSSetDefaultHandlerForURLScheme(cfScheme, cfBundle);
	CFRelease(cfScheme);
	CFRelease(cfBundle);
	return (int)st;
}

#pragma clang diagnostic pop
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

const (
	mainBundleID = "com.google.antigravity"
	boxBundleID  = "com.google.antigravity.box"
	lsregister   = "/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Support/lsregister"
)

var mdatRe = regexp.MustCompile(`"mdat"<timedate>=0x[0-9A-Fa-f]+\s+"([^"]+)"`)

func getURLHandler(scheme string) string {
	cs := C.CString(scheme)
	defer C.free(unsafe.Pointer(cs))
	res := C.copyDefaultHandler(cs)
	if res == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(res))
	return strings.TrimSpace(C.GoString(res))
}

func setURLHandler(scheme, bundleID string) error {
	cs := C.CString(scheme)
	cb := C.CString(bundleID)
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(cb))
	if st := int(C.setDefaultHandler(cs, cb)); st != 0 {
		return fmt.Errorf("LSSetDefaultHandlerForURLScheme(%s -> %s) status=%d", scheme, bundleID, st)
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

// RunChecks executes the 6-point local health audit across Main, IDE, Box, profiles, and integrations.
func RunChecks(cfg config.Config, instList []instances.Instance, cdpTargets []cdp.TargetStatus) Report {
	mainVer := instances.ReadAppBundleVersion(cfg.Box.MainAppPath)
	boxVer := instances.ReadAppBundleVersion(cfg.Box.BoxAppPath)
	ideVer := instances.ReadAppBundleVersion(cfg.Box.IDEAppPath)

	rep := Report{
		CheckedAt:   time.Now().Unix(),
		MainVersion: mainVer,
		BoxVersion:  boxVer,
		IDEVersion:  ideVer,
	}

	var checks []CheckResult

	// 1. Main vs Antigravity-Box version drift
	c1 := CheckResult{
		ID:        "box_version",
		Title:     "Main 与 Antigravity-Box 版本一致",
		Fixable:   false,
		FixAction: "sync_box",
	}
	switch {
	case mainVer == "" && boxVer == "":
		c1.Status = "error"
		c1.Summary = "未检测到 Antigravity.app 或 Antigravity-Box.app"
		c1.Detail = fmt.Sprintf("Main=%s Box=%s", cfg.Box.MainAppPath, cfg.Box.BoxAppPath)
	case boxVer == "":
		c1.Status = "warn"
		c1.Summary = fmt.Sprintf("未安装 Antigravity-Box (Main %s)", mainVer)
		c1.Detail = "请确保 p1..p4 已停止后执行「同步 Antigravity-Box」"
		c1.Fixable = true
	case mainVer != boxVer:
		c1.Status = "warn"
		c1.Summary = fmt.Sprintf("版本漂移：Main %s ≠ Box %s", mainVer, boxVer)
		c1.Detail = fmt.Sprintf("停止运行中的 p1..pN 后可通过 %s 将 Box 更新到 %s", cfg.Box.UpdateScriptPath, mainVer)
		c1.Fixable = true
	default:
		c1.Status = "ok"
		c1.Summary = fmt.Sprintf("版本完全一致 (%s)", mainVer)
		if ideVer != "" {
			c1.Detail = "Antigravity IDE: " + ideVer
		}
	}
	checks = append(checks, c1)

	// 2. macOS URL Scheme routing
	agyScheme := getURLHandler("antigravity")
	boxScheme := getURLHandler("antigravity-box")
	c2 := CheckResult{
		ID:        "url_scheme",
		Title:     "URL Scheme OAuth 回调路由",
		FixAction: "fix_url_scheme",
	}
	agyOK := strings.EqualFold(agyScheme, mainBundleID)
	boxOK := strings.EqualFold(boxScheme, boxBundleID)
	if agyOK && boxOK {
		c2.Status = "ok"
		c2.Summary = "antigravity:// 与 antigravity-box:// 路由正常"
		c2.Detail = fmt.Sprintf("antigravity → %s | antigravity-box → %s", agyScheme, boxScheme)
	} else {
		c2.Status = "warn"
		c2.Fixable = true
		c2.Summary = "URL Scheme 协议处理器偏移，可能影响 OAuth 登录回跳"
		c2.Detail = fmt.Sprintf("antigravity=%q (期望 %s) | antigravity-box=%q (期望 %s)", agyScheme, mainBundleID, boxScheme, boxBundleID)
	}
	checks = append(checks, c2)

	// 3. Read-only macOS Keychain metadata check (NEVER passes -w!)
	c3 := CheckResult{
		ID:      "keychain_main",
		Title:   "Main Keychain 登录凭证（只读元数据）",
		Fixable: false,
	}
	cmd := exec.Command("security", "find-generic-password", "-s", "gemini", "-a", "antigravity")
	out, kerr := cmd.CombinedOutput()
	if kerr == nil {
		mdat := parseKeychainMdat(string(out))
		c3.Status = "ok"
		if mdat != "" {
			c3.Summary = "Keychain 凭证完好，mdat: " + mdat
		} else {
			c3.Summary = "Keychain 凭证项存在 (gemini / antigravity)"
		}
		c3.Detail = "仅读取只读元数据（未使用 -w 导出密钥），不触碰 Main 登录态"
	} else {
		c3.Status = "warn"
		c3.Summary = "未在 Keychain 找到 gemini/antigravity 元数据条目"
		c3.Detail = strings.TrimSpace(string(out))
	}
	checks = append(checks, c3)

	// 4. ParaGravity broken symlinks & macOS launcher stubs
	home, _ := os.UserHomeDir()
	var profiles []instances.Instance
	var brokenProfiles []string
	var missingApps []string
	for _, inst := range instList {
		if inst.Kind != instances.KindProfile {
			continue
		}
		profiles = append(profiles, inst)
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
		Title:     "分身沙盒符号链接与 Launcher Stub",
		FixAction: "heal_profiles",
	}
	if len(brokenProfiles) == 0 && len(missingApps) == 0 {
		c4.Status = "ok"
		c4.Summary = fmt.Sprintf("已扫描 %d 个分身，0 个损坏符号链接 / 缺失应用壳", len(profiles))
	} else {
		c4.Status = "warn"
		c4.Fixable = true
		c4.Summary = fmt.Sprintf("发现异常（损坏链: %d，缺 Launcher: %d）", len(brokenProfiles), len(missingApps))
		c4.Detail = fmt.Sprintf("broken=[%s] missingStub=[%s]", strings.Join(brokenProfiles, ","), strings.Join(missingApps, ","))
	}
	checks = append(checks, c4)

	// 5. ParaGravity OAuth refresh_token health
	var okAuth, badAuth []string
	for _, inst := range profiles {
		if inst.AuthState == "ok" {
			okAuth = append(okAuth, inst.ID)
		} else {
			badAuth = append(badAuth, fmt.Sprintf("%s:%s", inst.ID, inst.AuthState))
		}
	}
	c5 := CheckResult{
		ID:      "profile_oauth",
		Title:   "分身 OAuth Refresh Token 状态",
		Fixable: false,
	}
	if len(profiles) == 0 {
		c5.Status = "ok"
		c5.Summary = "暂无 ParaGravity 分身"
	} else if len(badAuth) == 0 {
		c5.Status = "ok"
		c5.Summary = fmt.Sprintf("全部 %d 个分身均持有有效 refresh_token", len(okAuth))
		c5.Detail = "兼容 v2.29+ JSON OAuth 格式与旧版 JWT"
	} else {
		c5.Status = "warn"
		c5.Summary = fmt.Sprintf("%d/%d 个分身需要重新授权或缺失 Token", len(badAuth), len(profiles))
		c5.Detail = strings.Join(badAuth, ", ")
	}
	checks = append(checks, c5)

	// 6. Hooks + Sidecar + CDP localization script
	installedHooks, totalHooks, missingHooks := hooks.CheckInstalled(cfg)
	sidecarOK, sidecarDetail := CheckSidecar("")
	zhScriptPath := config.ExpandPath(cfg.CDP.ZhScriptPath)
	_, zhStatErr := os.Stat(zhScriptPath)

	c6 := CheckResult{
		ID:        "companion_hooks_sidecar",
		Title:     "Hooks · Sidecar · CDP 汉化脚本",
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
	if installedHooks == totalHooks && sidecarOK && zhStatErr == nil {
		c6.Status = "ok"
		c6.Summary = fmt.Sprintf("Hooks %d/%d · Sidecar 已注册 · CDP 目标 %d (已汉化 %d / HUD %d)",
			installedHooks, totalHooks, len(cdpTargets), zhInjectedCount, hudCount)
		c6.Detail = fmt.Sprintf("Sidecar: %s | 词库: %s", sidecarDetail, zhScriptPath)
	} else {
		c6.Status = "warn"
		c6.Fixable = true
		c6.Summary = fmt.Sprintf("Hooks %d/%d · Sidecar=%v · 词库=%v",
			installedHooks, totalHooks, sidecarOK, zhStatErr == nil)
		c6.Detail = fmt.Sprintf("missingHooks=%d sidecar=%s zhScript=%s", len(missingHooks), sidecarDetail, zhScriptPath)
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

// FixSafe applies non-destructive automated fixes (URL schemes, stopped profile healing, hooks, Sidecar).
// It never touches Main or IDE and never clones Antigravity-Box unless SyncBox is explicitly called.
func FixSafe(cfg config.Config, cliBin string) FixReport {
	var rep FixReport

	// 1. Repair LaunchServices & URL Scheme handlers
	if _, err := os.Stat(lsregister); err == nil {
		for _, appPath := range []string{cfg.Box.MainAppPath, cfg.Box.BoxAppPath} {
			if _, err := os.Stat(appPath); err == nil {
				_ = exec.Command(lsregister, "-f", appPath).Run()
			}
		}
	}
	if err := setURLHandler("antigravity", mainBundleID); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, "URL Scheme antigravity:// -> "+mainBundleID)
	}
	if err := setURLHandler("antigravity-box", boxBundleID); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, "URL Scheme antigravity-box:// -> "+boxBundleID)
	}

	// 2. Heal ParaGravity profiles safely (never main or ide)
	instList := instances.DiscoverAll(cfg)
	for _, inst := range instList {
		if inst.Kind != instances.KindProfile {
			continue
		}
		hrep, err := instances.HealProfile(cfg, inst.ID, false)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("heal %s: %v", inst.ID, err))
			continue
		}
		if hrep.SkippedRunning {
			if len(hrep.SymlinksRepaired) > 0 {
				rep.Fixed = append(rep.Fixed, fmt.Sprintf("%s: 修复 %d 个断链 (运行中跳过锁清理)", inst.ID, len(hrep.SymlinksRepaired)))
			} else {
				rep.Skipped = append(rep.Skipped, inst.ID+" (运行中)")
			}
		} else if len(hrep.LocksCleaned) > 0 || len(hrep.SymlinksRepaired) > 0 || len(hrep.CachesPurged) > 0 || hrep.UpdaterSuppressed {
			rep.Fixed = append(rep.Fixed, fmt.Sprintf("%s: locks=%d symlinks=%d caches=%d",
				inst.ID, len(hrep.LocksCleaned), len(hrep.SymlinksRepaired), len(hrep.CachesPurged)))
		}
		// Repair missing ~/Applications/Antigravity (<name>).app launcher stub if needed
		home, _ := os.UserHomeDir()
		stubPath := filepath.Join(home, "Applications", fmt.Sprintf("Antigravity (%s).app", inst.ID))
		if _, err := os.Stat(stubPath); err != nil {
			if err := instances.EnsureMacOSLauncherStub(inst.ID); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("launcher stub %s: %v", inst.ID, err))
			} else {
				rep.Fixed = append(rep.Fixed, "重建 Launcher Stub: "+inst.ID)
			}
		}
	}

	// 3. Install non-destructive hooks & Sidecar registration
	if updated, err := hooks.InstallHooks(cfg, cliBin); err != nil {
		rep.Errors = append(rep.Errors, "install hooks: "+err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, fmt.Sprintf("更新 Hooks (%d 个目标)", len(updated)))
	}
	if scPath, err := EnsureSidecar(cliBin); err != nil {
		rep.Errors = append(rep.Errors, "ensure sidecar: "+err.Error())
	} else {
		rep.Fixed = append(rep.Fixed, "注册 Sidecar: "+scPath)
	}
	return rep
}

// SyncBox clones Antigravity.app -> Antigravity-Box.app via ~/.antigravity-boxes/update-box.sh.
// All ParaGravity sandbox profiles must be stopped first; Main and IDE remain untouched.
func SyncBox(ctx context.Context, cfg config.Config) (string, error) {
	instList := instances.DiscoverAll(cfg)
	var running []string
	for _, inst := range instList {
		if inst.Kind == instances.KindProfile && inst.Running {
			running = append(running, inst.ID)
		}
	}
	if len(running) > 0 {
		return "", fmt.Errorf("同步 Antigravity-Box 前需先停止运行中的分身: %s（Main 与 IDE 无需停止）", strings.Join(running, ", "))
	}

	script := config.ExpandPath(cfg.Box.UpdateScriptPath)
	if _, err := os.Stat(script); err != nil {
		return "", fmt.Errorf("未找到 Box 更新脚本: %s", script)
	}

	tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(tctx, "bash", script).CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("update-box.sh 执行失败: %w (%s)", err, output)
	}

	_ = setURLHandler("antigravity", mainBundleID)
	_ = setURLHandler("antigravity-box", boxBundleID)
	for _, inst := range instList {
		if inst.Kind == instances.KindProfile {
			_, _ = instances.HealProfile(cfg, inst.ID, false)
		}
	}
	return output, nil
}
