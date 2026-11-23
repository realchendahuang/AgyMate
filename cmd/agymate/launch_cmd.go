package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

func cmdLaunch() *cobra.Command {
	var useIDE bool
	var noCDP bool
	var installApp bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "launch [workspace]",
		Short: "以 CDP 增强模式启动官方 Antigravity 客户端（开启右上角悬浮窗与客户端中文汉化）",
		Long: "启动官方 Antigravity（或 Antigravity IDE）客户端并附加 --remote-debugging-port=0，" +
			"在不修改官方安装包或破坏签名的前提下启用右上角悬浮窗与内置 Antigravity 客户端中文汉化。" +
			"\n\nmacOS 用户也可运行 `agymate launch --install-app` 在 ~/Applications/ 创建 `Antigravity (AgyMate).app` 快捷启动器。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg := loadCfg()
			if installApp {
				appPath, err := instances.EnsureMainAppLauncherStub(cfg)
				if err != nil {
					return err
				}
				if asJSON {
					printJSON(map[string]any{
						"ok":      true,
						"appPath": appPath,
					})
					return nil
				}
				fmt.Printf("已创建 macOS 快捷启动器: %s\n", appPath)
				fmt.Println("提示：以后退出原客户端 (⌘Q) 后，直接从聚焦搜索/访达打开「Antigravity (AgyMate)」即可自动启用右上角悬浮窗与中文汉化。")
				return nil
			}

			ws := ""
			if len(args) > 0 {
				ws = strings.TrimSpace(args[0])
			}
			enableCDP := !noCDP
			pid, cdpReady, err := instances.LaunchMainApp(cfg, useIDE, ws, enableCDP)
			if err != nil {
				return err
			}

			// If Hub is running and CDP was requested, trigger a best-effort sync shortly after startup.
			hubNotified := false
			if enableCDP {
				go func() {
					time.Sleep(1200 * time.Millisecond)
					if resp, rerr := hubRequest(http.MethodPost, "http://"+cfg.ListenAddr+"/api/cdp/sync", []byte("{}"), 3*time.Second); rerr == nil {
						resp.Body.Close()
					}
				}()
				if resp, rerr := hubRequest(http.MethodGet, "http://"+cfg.ListenAddr+"/api/health", nil, 800*time.Millisecond); rerr == nil {
					hubNotified = resp.StatusCode == http.StatusOK
					resp.Body.Close()
				}
			}

			zhSource, _ := cdp.ResolveZhSource(cfg.CDP.ZhScriptPath)
			target := "Antigravity"
			if useIDE {
				target = "Antigravity IDE"
			}

			if asJSON {
				printJSON(map[string]any{
					"ok":          true,
					"target":      target,
					"pid":         pid,
					"cdpEnabled":  cdpReady,
					"windowHud":   cfg.CDP.Enabled && cfg.CDP.WindowHUD,
					"zhInject":    cfg.CDP.Enabled && cfg.CDP.ZhInject,
					"zhSource":    zhSource,
					"hubNotified": hubNotified,
				})
				return nil
			}

			fmt.Printf("已启动 %s (PID %d)\n", target, pid)
			if cdpReady {
				var feats []string
				if cfg.CDP.Enabled && cfg.CDP.WindowHUD {
					feats = append(feats, fmt.Sprintf("悬浮窗(%s)", cfg.CDP.HUDPosition))
				}
				if cfg.CDP.Enabled && cfg.CDP.ZhInject {
					feats = append(feats, fmt.Sprintf("客户端中文汉化(%s)", zhSource))
				}
				if len(feats) > 0 {
					fmt.Printf("CDP 增强模式已开启：AgyMate Hub 将在窗口就绪后自动挂载 %s。\n", strings.Join(feats, " + "))
				} else {
					fmt.Println("CDP 增强模式已开启（当前配置未启用悬浮窗或中文汉化，可通过 `agymate zh on` 开启）。")
				}
				if !hubNotified {
					fmt.Println("提示：未检测到运行中的本地 Hub，请先运行 `agymate service install` 或 `agymate serve` 以便自动挂载悬浮窗与汉化。")
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&useIDE, "ide", false, "启动 Antigravity IDE 客户端而非主客户端")
	cmd.Flags().BoolVar(&noCDP, "no-cdp", false, "不附加 --remote-debugging-port=0（以原版无 CDP 模式启动）")
	cmd.Flags().BoolVar(&installApp, "install-app", false, "在 ~/Applications/ 创建 macOS 快捷启动器 `Antigravity (AgyMate).app`")
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func updateZhViaHubOrConfig(enable bool, scriptPath *string) (hubSynced bool, zhSource string, err error) {
	cfg := loadCfg()
	if enable {
		cfg.CDP.Enabled = true
	}
	cfg.CDP.ZhInject = enable
	if scriptPath != nil {
		cfg.CDP.ZhScriptPath = strings.TrimSpace(*scriptPath)
	}

	zhSource, _ = cdp.ResolveZhSource(cfg.CDP.ZhScriptPath)

	// Send the full cfg.CDP struct so sibling fields (windowHud, hudPosition, hudInstances) are preserved.
	body, _ := json.Marshal(map[string]any{"cdp": cfg.CDP})
	if resp, rerr := hubRequest(http.MethodPut, "http://"+cfg.ListenAddr+"/api/config", body, 5*time.Second); rerr == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true, zhSource, nil
		}
	}

	if serr := config.Save(cfg); serr != nil {
		return false, zhSource, serr
	}
	return false, zhSource, nil
}
