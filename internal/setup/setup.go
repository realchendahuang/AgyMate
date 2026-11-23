// Package setup implements self-contained, idempotent installation, migration,
// and clean uninstallation of AgyMate across macOS, Linux, and Windows.
package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/service"
)

// Options controls `agymate setup` behavior.
type Options struct {
	BinPath             string
	DryRun              bool
	Minimal             bool
	NoService           bool
	NoHooks             bool
	NoSidecar           bool
	NoPlugin            bool
	WithMagpie          bool
	NoMagpie            bool
	MagpieDevDir        string
	NoWait              bool
	AntigravityPluginFS fs.FS
	MagpiePluginFS      fs.FS
	HomeDir             string // optional override for testing
}

// UninstallOptions controls `agymate uninstall` behavior.
type UninstallOptions struct {
	DryRun    bool
	PurgeData bool
	NoService bool
	HomeDir   string // optional override for testing
}

// StepResult records the outcome of one setup or uninstall step.
type StepResult struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Status  string   `json:"status"` // ok | skipped | dry_run | warn | error
	Summary string   `json:"summary"`
	Paths   []string `json:"paths,omitempty"`
}

// Report aggregates all step results from Run or Uninstall.
type Report struct {
	OK         bool         `json:"ok"`
	DryRun     bool         `json:"dryRun,omitempty"`
	BinPath    string       `json:"binPath,omitempty"`
	ListenAddr string       `json:"listenAddr,omitempty"`
	Steps      []StepResult `json:"steps"`
}

func resolveHome(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return filepath.Clean(override), nil
	}
	return os.UserHomeDir()
}

// ResolveCLIBin determines the canonical path of the agymate binary to register
// in background services, hooks, and Sidecar manifests.
func ResolveCLIBin(explicit string) string {
	if p := strings.TrimSpace(explicit); p != "" {
		if resolved, err := service.ResolveExecutable(p); err == nil && resolved != "" {
			return resolved
		}
		return config.ExpandPath(p)
	}
	if exe, err := os.Executable(); err == nil {
		base := strings.ToLower(filepath.Base(exe))
		if base == "agymate" || base == "agymate.exe" {
			if realPath, rerr := filepath.EvalSymlinks(exe); rerr == nil {
				return realPath
			}
			return exe
		}
	}
	if found, err := exec.LookPath("agymate"); err == nil && found != "" {
		if realPath, rerr := filepath.EvalSymlinks(found); rerr == nil {
			return realPath
		}
		return found
	}
	home, _ := os.UserHomeDir()
	name := "agymate"
	if runtime.GOOS == "windows" {
		name = "agymate.exe"
	}
	return filepath.Join(home, ".local", "bin", name)
}

// Run executes idempotent self-contained setup for AgyMate.
func Run(ctx context.Context, opts Options) Report {
	binPath := ResolveCLIBin(opts.BinPath)
	rep := Report{
		OK:         true,
		DryRun:     opts.DryRun,
		BinPath:    binPath,
		ListenAddr: "127.0.0.1:3778",
	}

	home, err := resolveHome(opts.HomeDir)
	if err != nil {
		rep.OK = false
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "home",
			Title:   "定位用户目录",
			Status:  "error",
			Summary: err.Error(),
		})
		return rep
	}

	// Step 1: Config initialization & smooth migration + hub.token
	cfgStep, listenAddr := stepConfigMigrate(opts.DryRun)
	rep.ListenAddr = listenAddr
	rep.Steps = append(rep.Steps, cfgStep)
	if cfgStep.Status == "error" {
		rep.OK = false
	}

	cfg, _ := config.Load()
	if strings.TrimSpace(cfg.ListenAddr) != "" {
		rep.ListenAddr = normalizeLoopbackAddr(cfg.ListenAddr)
	}

	// Step 2: Background service
	if opts.NoService {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "service",
			Title:   "后台守护服务",
			Status:  "skipped",
			Summary: "已通过 --no-service 跳过",
		})
	} else if opts.DryRun {
		unit := previewServiceUnitPath(home)
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "service",
			Title:   "后台守护服务",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将安装并启动后台服务 (%s)", unit),
			Paths:   []string{unit},
		})
	} else {
		msg, serr := service.Install(binPath)
		if serr != nil {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "service",
				Title:   "后台守护服务",
				Status:  "warn",
				Summary: fmt.Sprintf("跳过自动托管（可前台运行 `%s serve`）: %v", binPath, serr),
			})
		} else {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "service",
				Title:   "后台守护服务",
				Status:  "ok",
				Summary: msg,
			})
		}
	}

	// Step 3: Antigravity lifecycle hooks
	if opts.Minimal || opts.NoHooks {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "hooks",
			Title:   "Antigravity 生命周期 Hooks",
			Status:  "skipped",
			Summary: skipReason(opts.Minimal, "--no-hooks"),
		})
	} else if opts.DryRun {
		targets := hooks.InstallTargets(cfg)
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "hooks",
			Title:   "Antigravity 生命周期 Hooks",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将无损合并写入 %d 个 hooks.json（仅更新 agymate 键）", len(targets)),
			Paths:   targets,
		})
	} else {
		updated, herr := hooks.InstallHooks(cfg, binPath)
		if herr != nil {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "hooks",
				Title:   "Antigravity 生命周期 Hooks",
				Status:  "warn",
				Summary: herr.Error(),
				Paths:   updated,
			})
		} else {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "hooks",
				Title:   "Antigravity 生命周期 Hooks",
				Status:  "ok",
				Summary: fmt.Sprintf("已无损更新 %d 个 hooks.json", len(updated)),
				Paths:   updated,
			})
		}
	}

	// Step 4: Antigravity Sidecar
	if opts.Minimal || opts.NoSidecar {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "sidecar",
			Title:   "Antigravity 原生 Sidecar",
			Status:  "skipped",
			Summary: skipReason(opts.Minimal, "--no-sidecar"),
		})
	} else if opts.DryRun {
		scTargets := collectSidecarTargets(home, cfg)
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "sidecar",
			Title:   "Antigravity 原生 Sidecar",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将注册 %d 个 Sidecar 清单", len(scTargets)),
			Paths:   scTargets,
		})
	} else {
		scPaths, serr := doctor.EnsureSidecarForProfiles(cfg, binPath)
		if serr != nil {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "sidecar",
				Title:   "Antigravity 原生 Sidecar",
				Status:  "warn",
				Summary: serr.Error(),
			})
		} else {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "sidecar",
				Title:   "Antigravity 原生 Sidecar",
				Status:  "ok",
				Summary: fmt.Sprintf("已注册 %d 个 Sidecar 目标", len(scPaths)),
				Paths:   scPaths,
			})
		}
	}

	// Step 5: Antigravity Native Plugin & MCP App
	if opts.Minimal || opts.NoPlugin {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "plugin",
			Title:   "Antigravity 原生插件 & MCP App",
			Status:  "skipped",
			Summary: skipReason(opts.Minimal, "--no-plugin"),
		})
	} else if opts.AntigravityPluginFS == nil {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "plugin",
			Title:   "Antigravity 原生插件 & MCP App",
			Status:  "skipped",
			Summary: "未提供内置插件资源",
		})
	} else {
		pStep := stepInstallAntigravityPlugin(home, cfg, binPath, opts.AntigravityPluginFS, opts.DryRun)
		rep.Steps = append(rep.Steps, pStep)
		if pStep.Status == "error" {
			rep.OK = false
		}
	}

	// Step 6: Optional Magpie Page Plugin
	if opts.Minimal || opts.NoMagpie {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "magpie",
			Title:   "Magpie 页面插件（可选）",
			Status:  "skipped",
			Summary: skipReason(opts.Minimal, "--no-magpie"),
		})
	} else {
		mStep := stepInstallMagpiePlugin(home, opts.MagpiePluginFS, opts.WithMagpie, opts.MagpieDevDir, opts.DryRun)
		rep.Steps = append(rep.Steps, mStep)
	}

	// Step 7: Refresh running Language Servers + wait for Hub readiness + sync HUD
	if opts.DryRun || opts.NoWait {
		reason := "预览模式不触发运行时刷新"
		if !opts.DryRun {
			reason = "已通过 --no-wait 跳过运行时刷新"
		}
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "runtime",
			Title:   "刷新运行态 MCP App 与本地 Hub",
			Status:  "skipped",
			Summary: reason,
		})
	} else {
		rStep := stepRefreshRuntime(ctx, home, rep.ListenAddr)
		rep.Steps = append(rep.Steps, rStep)
	}

	return rep
}

// Uninstall cleanly removes background services, Hooks, Sidecars, and plugins installed by AgyMate.
func Uninstall(ctx context.Context, opts UninstallOptions) Report {
	_ = ctx
	rep := Report{
		OK:     true,
		DryRun: opts.DryRun,
	}

	home, err := resolveHome(opts.HomeDir)
	if err != nil {
		rep.OK = false
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "home",
			Title:   "定位用户目录",
			Status:  "error",
			Summary: err.Error(),
		})
		return rep
	}

	cfg, _ := config.Load()

	// 1. Background service
	if opts.NoService {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "service",
			Title:   "停止并移除后台守护服务",
			Status:  "skipped",
			Summary: "已通过 --no-service 跳过",
		})
	} else if opts.DryRun {
		unit := previewServiceUnitPath(home)
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "service",
			Title:   "停止并移除后台守护服务",
			Status:  "dry_run",
			Summary: "将卸载后台服务: " + unit,
			Paths:   []string{unit},
		})
	} else {
		msg, serr := service.Uninstall()
		if serr != nil {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "service",
				Title:   "停止并移除后台守护服务",
				Status:  "warn",
				Summary: serr.Error(),
			})
		} else {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "service",
				Title:   "停止并移除后台守护服务",
				Status:  "ok",
				Summary: msg,
			})
		}
	}

	// 2. Non-destructive Hooks cleanup
	if opts.DryRun {
		targets := hooks.InstallTargets(cfg)
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "hooks",
			Title:   "无损清理 Antigravity Hooks",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将从 %d 个 hooks.json 中仅移除 agymate 键", len(targets)),
			Paths:   targets,
		})
	} else {
		cleaned, herr := hooks.UninstallHooks(cfg)
		if herr != nil {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "hooks",
				Title:   "无损清理 Antigravity Hooks",
				Status:  "warn",
				Summary: herr.Error(),
				Paths:   cleaned,
			})
		} else {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "hooks",
				Title:   "无损清理 Antigravity Hooks",
				Status:  "ok",
				Summary: fmt.Sprintf("已无损清理 %d 个 hooks.json（保留其余 Hooks）", len(cleaned)),
				Paths:   cleaned,
			})
		}
	}

	// 3. Remove Sidecar registrations
	if opts.DryRun {
		targets := collectSidecarTargets(home, cfg)
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "sidecar",
			Title:   "移除 Antigravity Sidecar",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将移除 %d 处 Sidecar 注册", len(targets)),
			Paths:   targets,
		})
	} else {
		removed, serr := doctor.RemoveSidecarForProfiles(cfg)
		if serr != nil {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "sidecar",
				Title:   "移除 Antigravity Sidecar",
				Status:  "warn",
				Summary: serr.Error(),
				Paths:   removed,
			})
		} else {
			rep.Steps = append(rep.Steps, StepResult{
				ID:      "sidecar",
				Title:   "移除 Antigravity Sidecar",
				Status:  "ok",
				Summary: fmt.Sprintf("已移除 %d 处 Sidecar 目录", len(removed)),
				Paths:   removed,
			})
		}
	}

	// 4. Remove Antigravity Plugin directories
	pluginDirs := collectPluginTargets(home, cfg)
	if opts.DryRun {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "plugin",
			Title:   "移除 Antigravity 原生插件",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将检查并移除 %d 处插件目录", len(pluginDirs)),
			Paths:   pluginDirs,
		})
	} else {
		var removed []string
		for _, dir := range pluginDirs {
			if _, err := os.Stat(dir); err == nil {
				if rerr := os.RemoveAll(dir); rerr == nil {
					removed = append(removed, dir)
				}
			}
		}
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "plugin",
			Title:   "移除 Antigravity 原生插件",
			Status:  "ok",
			Summary: fmt.Sprintf("已移除 %d 处插件目录", len(removed)),
			Paths:   removed,
		})
	}

	// 5. Remove Magpie Page Plugin registration & directory
	magpieDir := filepath.Join(home, ".local", "share", "agymate", "magpie-plugin")
	if opts.DryRun {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "magpie",
			Title:   "移除 Magpie 页面插件",
			Status:  "dry_run",
			Summary: "将尝试反注册并清理 " + magpieDir,
			Paths:   []string{magpieDir},
		})
	} else {
		if magpieBin := findMagpieBinary(); magpieBin != "" {
			_ = exec.Command(magpieBin, "plugin", "rm", magpieDir).Run()
			_ = exec.Command(magpieBin, "plugin", "rm", "agymate").Run()
		}
		_ = os.RemoveAll(magpieDir)
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "magpie",
			Title:   "移除 Magpie 页面插件",
			Status:  "ok",
			Summary: "已清理 Magpie 插件目录与注册项",
			Paths:   []string{magpieDir},
		})
	}

	// 6. Optional data purge (--purge)
	cfgDir := filepath.Dir(config.ConfigPath())
	stateDir := filepath.Dir(config.StatePath())
	if !opts.PurgeData {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "data",
			Title:   "用户配置与历史快照",
			Status:  "skipped",
			Summary: fmt.Sprintf("默认保留用户配置与 14 天配额快照（如需彻底删除请附加 --purge）"),
			Paths:   []string{cfgDir, stateDir},
		})
	} else if opts.DryRun {
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "data",
			Title:   "彻底清理配置与数据目录 (--purge)",
			Status:  "dry_run",
			Summary: "将删除配置目录与状态目录",
			Paths:   []string{cfgDir, stateDir},
		})
	} else {
		var purged []string
		for _, dir := range []string{cfgDir, stateDir} {
			if dir != "" && dir != home && dir != "/" && dir != "." {
				if err := os.RemoveAll(dir); err == nil {
					purged = append(purged, dir)
				}
			}
		}
		rep.Steps = append(rep.Steps, StepResult{
			ID:      "data",
			Title:   "彻底清理配置与数据目录 (--purge)",
			Status:  "ok",
			Summary: fmt.Sprintf("已清理 %d 个数据目录", len(purged)),
			Paths:   purged,
		})
	}

	return rep
}

func stepConfigMigrate(dryRun bool) (StepResult, string) {
	cfgPath := config.ConfigPath()
	tokPath := config.HubTokenPath()
	addr := "127.0.0.1:3778"

	if dryRun {
		return StepResult{
			ID:      "config",
			Title:   "配置文件初始化与迁移",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将确保 %s 与 %s 就绪（权限 0600）", cfgPath, tokPath),
			Paths:   []string{cfgPath, tokPath},
		}, addr
	}

	raw, err := os.ReadFile(cfgPath)
	migrated := false
	created := false
	if err != nil {
		if !os.IsNotExist(err) {
			return StepResult{
				ID:      "config",
				Title:   "配置文件初始化与迁移",
				Status:  "error",
				Summary: err.Error(),
			}, addr
		}
		cfg := config.DefaultConfig()
		if serr := config.Save(cfg); serr != nil {
			return StepResult{
				ID:      "config",
				Title:   "配置文件初始化与迁移",
				Status:  "error",
				Summary: serr.Error(),
			}, addr
		}
		created = true
		addr = normalizeLoopbackAddr(cfg.ListenAddr)
	} else {
		_ = os.Chmod(cfgPath, 0o600)
		var doc map[string]any
		if len(bytes.TrimSpace(raw)) == 0 {
			doc = map[string]any{}
		}
		if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &doc) == nil {
			if doc == nil {
				doc = map[string]any{}
			}
			if rawAddr, ok := doc["listenAddr"].(string); ok && strings.TrimSpace(rawAddr) != "" {
				addr = normalizeLoopbackAddr(rawAddr)
			}
			cdpRaw, hasCDP := doc["cdp"]
			if !hasCDP || cdpRaw == nil {
				doc["cdp"] = map[string]any{"hudPosition": "top-right"}
				migrated = true
			} else if cdpMap, ok := cdpRaw.(map[string]any); ok {
				pos, _ := cdpMap["hudPosition"].(string)
				if strings.TrimSpace(pos) == "" || strings.EqualFold(strings.TrimSpace(pos), "bottom-right") {
					cdpMap["hudPosition"] = "top-right"
					migrated = true
				}
			}
			if migrated {
				out, merr := json.MarshalIndent(doc, "", "  ")
				if merr == nil {
					out = append(out, '\n')
					tmp := cfgPath + ".tmp"
					if werr := os.WriteFile(tmp, out, 0o600); werr == nil {
						_ = os.Rename(tmp, cfgPath)
					}
				}
			}
		}
	}

	if _, terr := config.LoadOrCreateHubToken(); terr != nil {
		return StepResult{
			ID:      "config",
			Title:   "配置文件初始化与迁移",
			Status:  "warn",
			Summary: fmt.Sprintf("配置已就绪，但生成 hub.token 失败: %v", terr),
			Paths:   []string{cfgPath},
		}, addr
	}

	summary := "配置无需迁移: " + cfgPath
	if created {
		summary = "已初始化默认配置: " + cfgPath
	} else if migrated {
		summary = fmt.Sprintf("已平滑迁移 %s（cdp.hudPosition → top-right，其余字段保留）", cfgPath)
	}
	return StepResult{
		ID:      "config",
		Title:   "配置文件初始化与迁移",
		Status:  "ok",
		Summary: summary,
		Paths:   []string{cfgPath, tokPath},
	}, addr
}

func stepInstallAntigravityPlugin(home string, cfg config.Config, binPath string, pluginFS fs.FS, dryRun bool) StepResult {
	targets := collectPluginTargets(home, cfg)
	if dryRun {
		return StepResult{
			ID:      "plugin",
			Title:   "Antigravity 原生插件 & MCP App",
			Status:  "dry_run",
			Summary: fmt.Sprintf("将同步安装原生插件至 %d 处目录", len(targets)),
			Paths:   targets,
		}
	}

	var installed []string
	for _, dst := range targets {
		if err := extractPluginTree(pluginFS, dst, binPath, true); err != nil {
			return StepResult{
				ID:      "plugin",
				Title:   "Antigravity 原生插件 & MCP App",
				Status:  "error",
				Summary: fmt.Sprintf("安装插件至 %s 失败: %v", dst, err),
				Paths:   installed,
			}
		}
		installed = append(installed, dst)
	}

	validateNote := ""
	if len(installed) > 0 {
		if agyBin := findAgyBinary(home); agyBin != "" {
			if out, err := exec.Command(agyBin, "plugin", "validate", "--strict", installed[0]).CombinedOutput(); err == nil {
				validateNote = " · 已通过 agy plugin validate --strict"
			} else {
				validateNote = fmt.Sprintf(" · agy 校验提示: %s", firstLine(string(out)))
			}
		}
	}

	return StepResult{
		ID:      "plugin",
		Title:   "Antigravity 原生插件 & MCP App",
		Status:  "ok",
		Summary: fmt.Sprintf("已同步安装 %d 处插件目录%s", len(installed), validateNote),
		Paths:   installed,
	}
}

func stepInstallMagpiePlugin(home string, magpieFS fs.FS, force bool, devDir string, dryRun bool) StepResult {
	magpieBin := findMagpieBinary()
	if magpieBin == "" && !force && strings.TrimSpace(devDir) == "" {
		return StepResult{
			ID:      "magpie",
			Title:   "Magpie 页面插件（可选）",
			Status:  "skipped",
			Summary: "未检测到 Magpie 客户端（可随时通过 `agymate setup --with-magpie` 单独启用）",
		}
	}

	dst := filepath.Join(home, ".local", "share", "agymate", "magpie-plugin")
	target := dst
	if strings.TrimSpace(devDir) != "" {
		target = config.ExpandPath(strings.TrimSpace(devDir))
	}

	if dryRun {
		return StepResult{
			ID:      "magpie",
			Title:   "Magpie 页面插件（可选）",
			Status:  "dry_run",
			Summary: "将同步并注册 Magpie 页面插件: " + target,
			Paths:   []string{target},
		}
	}

	if strings.TrimSpace(devDir) == "" {
		if magpieFS == nil {
			return StepResult{
				ID:      "magpie",
				Title:   "Magpie 页面插件（可选）",
				Status:  "skipped",
				Summary: "未提供内置 Magpie 插件资源",
			}
		}
		if err := extractPluginTree(magpieFS, dst, "", false); err != nil {
			return StepResult{
				ID:      "magpie",
				Title:   "Magpie 页面插件（可选）",
				Status:  "warn",
				Summary: fmt.Sprintf("释放 Magpie 插件失败: %v", err),
			}
		}
	}

	if magpieBin != "" {
		if strings.TrimSpace(devDir) != "" {
			_ = exec.Command(magpieBin, "plugin", "rm", dst).Run()
		}
		_ = exec.Command(magpieBin, "plugin", "add", target).Run()
		_ = exec.Command(magpieBin, "plugin", "on", target).Run()
		_ = exec.Command(magpieBin, "plugin", "on", "agymate").Run()
		return StepResult{
			ID:      "magpie",
			Title:   "Magpie 页面插件（可选）",
			Status:  "ok",
			Summary: "已注册并启用 Magpie 页面插件: " + target,
			Paths:   []string{target},
		}
	}
	return StepResult{
		ID:      "magpie",
		Title:   "Magpie 页面插件（可选）",
		Status:  "ok",
		Summary: "已释放插件文件（未检测到 Magpie 可执行文件，跳过自动注册）: " + target,
		Paths:   []string{target},
	}
}

func stepRefreshRuntime(ctx context.Context, home, listenAddr string) StepResult {
	refreshedLS := refreshRunningLanguageServers(ctx, home)

	hc := &http.Client{Timeout: 1200 * time.Millisecond}
	ready := false
	healthURL := "http://" + listenAddr + "/api/health"
	for i := 0; i < 40; i++ {
		if ctx.Err() != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err == nil {
			resp, rerr := hc.Do(req)
			if rerr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					ready = true
					break
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	if !ready {
		return StepResult{
			ID:      "runtime",
			Title:   "刷新运行态 MCP App 与本地 Hub",
			Status:  "warn",
			Summary: fmt.Sprintf("Hub 尚未在 http://%s 响应（已刷新 %d 个 LS）；请运行 `agymate service status` 检查", listenAddr, refreshedLS),
		}
	}

	syncOK := false
	tok := config.LoadHubToken()
	syncClient := &http.Client{Timeout: 5 * time.Second}
	for _, ep := range []string{"/api/cdp/sync", "/api/cdp/inject-now"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+listenAddr+ep, bytes.NewReader([]byte("{}")))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("X-AgyMate-Token", tok)
		}
		resp, rerr := syncClient.Do(req)
		if rerr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				syncOK = true
				break
			}
		}
	}

	parts := []string{fmt.Sprintf("Hub 在线 (http://%s)", listenAddr)}
	if refreshedLS > 0 {
		parts = append(parts, fmt.Sprintf("已向 %d 个运行中的 Language Server 刷新 MCP App", refreshedLS))
	}
	if syncOK {
		parts = append(parts, "已触发窗口 HUD 同步")
	}
	return StepResult{
		ID:      "runtime",
		Title:   "刷新运行态 MCP App 与本地 Hub",
		Status:  "ok",
		Summary: strings.Join(parts, " · "),
	}
}

func refreshRunningLanguageServers(ctx context.Context, home string) int {
	hc := &http.Client{Timeout: 3 * time.Second}
	seen := make(map[string]bool)
	count := 0

	tryRefresh := func(addr, csrf string) {
		addr = strings.TrimSpace(addr)
		csrf = strings.TrimSpace(csrf)
		if addr == "" || csrf == "" || seen[addr] {
			return
		}
		seen[addr] = true
		u := fmt.Sprintf("http://%s/exa.language_server_pb.LanguageServerService/RefreshMcpServers", addr)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader([]byte(`{"serverName":"agymate_hub"}`)))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-codeium-csrf-token", csrf)
		resp, err := hc.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				count++
			}
		}
	}

	if envAddr, envCSRF := os.Getenv("ANTIGRAVITY_LS_ADDRESS"), os.Getenv("ANTIGRAVITY_CSRF_TOKEN"); envAddr != "" && envCSRF != "" {
		tryRefresh(envAddr, envCSRF)
	}

	patterns := []string{
		filepath.Join(home, ".gemini", "antigravity", "daemon", "hub_*.json"),
		filepath.Join(home, ".gemini", "antigravity-ide", "daemon", "hub_*.json"),
		filepath.Join(home, ".antigravity-profiles", "*", "home", ".gemini", "antigravity", "daemon", "hub_*.json"),
	}
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, path := range matches {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var d struct {
				PID       int    `json:"pid"`
				HTTPPort  int    `json:"httpPort"`
				CSRFToken string `json:"csrfToken"`
			}
			if json.Unmarshal(raw, &d) != nil || d.PID <= 0 || d.HTTPPort <= 0 || d.CSRFToken == "" {
				continue
			}
			if !instances.IsPIDAlive(d.PID) {
				continue
			}
			tryRefresh(fmt.Sprintf("127.0.0.1:%d", d.HTTPPort), d.CSRFToken)
		}
	}
	return count
}

func extractPluginTree(srcFS fs.FS, dst, binPath string, isAntigravityPlugin bool) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".agymate-plugin-*.tmp")
	if err != nil {
		return err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	walkErr := fs.WalkDir(srcFS, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(tmp, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := fs.ReadFile(srcFS, rel)
		if rerr != nil {
			return rerr
		}
		// On Windows, rewrite mcp_config.json to invoke the native agymate binary directly (no bash required).
		if isAntigravityPlugin && runtime.GOOS == "windows" && rel == "mcp_config.json" && strings.TrimSpace(binPath) != "" {
			cfgDoc := map[string]any{
				"mcpServers": map[string]any{
					"hub": map[string]any{
						"command": binPath,
						"args":    []string{"mcp"},
					},
				},
			}
			if b, merr := json.MarshalIndent(cfgDoc, "", "  "); merr == nil {
				data = append(b, '\n')
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		return os.WriteFile(target, data, mode)
	})
	if walkErr != nil {
		cleanup()
		return walkErr
	}

	if err := os.RemoveAll(dst); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		cleanup()
		return err
	}
	return nil
}

func collectPluginTargets(home string, cfg config.Config) []string {
	targets := []string{doctor.PluginPathIn(home)}
	profilesDir := config.ExpandPath(cfg.Paths.ProfilesDir)
	if entries, err := os.ReadDir(profilesDir); err == nil {
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			profHome := filepath.Join(profilesDir, ent.Name(), "home")
			if st, err := os.Stat(filepath.Join(profHome, ".gemini", "config")); err == nil && st.IsDir() {
				targets = append(targets, doctor.PluginPathIn(profHome))
			}
		}
	}
	return targets
}

func collectSidecarTargets(home string, cfg config.Config) []string {
	targets := []string{doctor.SidecarPathIn(home)}
	profilesDir := config.ExpandPath(cfg.Paths.ProfilesDir)
	if entries, err := os.ReadDir(profilesDir); err == nil {
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			profHome := filepath.Join(profilesDir, ent.Name(), "home")
			if st, err := os.Stat(profHome); err == nil && st.IsDir() {
				targets = append(targets, doctor.SidecarPathIn(profHome))
			}
		}
	}
	return targets
}

func previewServiceUnitPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return service.LaunchdPlistPath(home)
	case "linux":
		return service.SystemdUnitPath(home)
	default:
		return "manual (`agymate serve`)"
	}
}

func findAgyBinary(home string) string {
	if p, err := exec.LookPath("agy"); err == nil && p != "" {
		return p
	}
	cand := filepath.Join(home, ".local", "bin", "agy")
	if st, err := os.Stat(cand); err == nil && !st.IsDir() {
		return cand
	}
	return ""
}

func findMagpieBinary() string {
	if runtime.GOOS == "darwin" {
		const appBin = "/Applications/magpie.app/Contents/MacOS/magpie"
		if st, err := os.Stat(appBin); err == nil && !st.IsDir() {
			return appBin
		}
	}
	if p, err := exec.LookPath("magpie"); err == nil && p != "" {
		return p
	}
	return ""
}

func normalizeLoopbackAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "127.0.0.1:3778"
	}
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	return addr
}

func skipReason(minimal bool, flag string) string {
	if minimal {
		return "精简模式 (--minimal) 已跳过"
	}
	return "已通过 " + flag + " 跳过"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if line, _, ok := strings.Cut(s, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return s
}
