// Command agymate is the unified CLI and headless local HTTP Hub for AgyMate.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/AgyMate/frontend"
	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/hub"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"github.com/realchendahuang/AgyMate/internal/version"
)

func main() {
	root := &cobra.Command{
		Use:     "agymate",
		Short:   "AgyMate — Antigravity 五开伴侣 (配额 · Token · 上下文 · 沙盒护航 · CDP 汉化)",
		Version: version.Version,
	}

	root.AddCommand(
		cmdServe(),
		cmdStatus(),
		cmdQuota(),
		cmdTokens(),
		cmdSearch(),
		cmdProfile(),
		cmdDoctor(),
		cmdBoxSync(),
		cmdZh(),
		cmdCache(),
		cmdInstallHooks(),
		cmdHook(),
		cmdSidecarProxy(),
		cmdVersion(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadCfg() config.Config {
	cfg, _ := config.Load()
	return cfg
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func cmdServe() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动本地 Loopback HTTP Hub 与后台遥测/CDP守护",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadCfg()
			if strings.TrimSpace(addr) != "" {
				cfg.ListenAddr = strings.TrimSpace(addr)
			}
			srv := hub.NewServer(cfg, frontend.FS())
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			srv.Start(ctx)

			httpSrv := &http.Server{
				Addr:              cfg.ListenAddr,
				Handler:           srv.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
			}

			errCh := make(chan error, 1)
			go func() {
				fmt.Printf("AgyMate Hub listening on http://%s\n", cfg.ListenAddr)
				if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				}
				close(errCh)
			}()

			select {
			case <-ctx.Done():
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return httpSrv.Shutdown(shutCtx)
			case err := <-errCh:
				return err
			}
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "监听地址 (默认读取 config.json 的 127.0.0.1:3778)")
	return cmd
}

func cmdStatus() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "显示所有实例 (Main / IDE / p1..pN) 状态、配额、上下文与今日账单",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			srv := hub.NewServer(cfg, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			srv.PollOnce(ctx)
			ov := srv.BuildOverview()
			if asJSON {
				printJSON(ov)
				return
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tLABEL\tSTATE\tACCOUNT\tQUOTA(5H)\tCTX\tTODAY_USD\tLS_VER")
			for _, iv := range ov.Instances {
				q5h := "-"
				ctxPct := "-"
				acc := iv.Account
				lsVer := "-"
				if iv.Hub != nil && iv.Hub.LSVersion != "" {
					lsVer = iv.Hub.LSVersion
				}
				if iv.Snapshot != nil {
					if acc == "" {
						acc = iv.Snapshot.Email
					}
					if len(iv.Snapshot.Buckets) > 0 {
						q5h = fmt.Sprintf("%.0f%%", iv.Snapshot.Buckets[0].RemainingPct)
					}
					if iv.Snapshot.Context != nil {
						ctxPct = fmt.Sprintf("%.0f%%", iv.Snapshot.Context.UsagePct)
					}
				}
				if acc == "" {
					acc = "-"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t$%.2f\t%s\n",
					iv.ID, iv.Label, iv.LiveState, acc, q5h, ctxPct, iv.TodayUSD, lsVer)
			}
			_ = tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func cmdQuota() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "quota",
		Short: "查询各在线实例的分桶配额、重置倒计时与速率耗尽预警",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			state := config.LoadState("")
			rpcCli := rpc.NewClient()
			instList := instances.DiscoverAll(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()

			type quotaEntry struct {
				Instance string               `json:"instance"`
				Account  string               `json:"account"`
				Plan     string               `json:"plan"`
				Buckets  []rpc.QuotaBucket    `json:"buckets"`
				Models   []rpc.ModelQuota     `json:"models"`
			}
			var out []quotaEntry
			for _, inst := range instList {
				if inst.Hub == nil {
					continue
				}
				snap := rpcCli.FetchInstance(ctx, inst.ID, inst.Hub, state)
				acc := inst.Account
				if acc == "" {
					acc = snap.Email
				}
				out = append(out, quotaEntry{
					Instance: inst.ID,
					Account:  acc,
					Plan:     snap.PlanName,
					Buckets:  snap.Buckets,
					Models:   snap.Models,
				})
			}
			_ = state.Save()
			if asJSON {
				printJSON(out)
				return
			}
			if len(out) == 0 {
				fmt.Println("当前没有正在运行且暴露 daemon/hub 的 Antigravity 实例。")
				return
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "INSTANCE\tPLAN\tBUCKET\tREMAINING\tRESET_LEFT\tRUNWAY")
			for _, e := range out {
				for _, b := range e.Buckets {
					runway := "-"
					if b.ExhaustsBeforeReset && b.HoursUntilEmpty > 0 {
						runway = fmt.Sprintf("⚠ %.1fh 耗尽 (%.1f%%/h)", b.HoursUntilEmpty, b.BurnPctPerHour)
					} else if b.BurnPctPerHour > 0 {
						runway = fmt.Sprintf("%.1f%%/h", b.BurnPctPerHour)
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f%%\t%s\t%s\n",
						e.Instance, e.Plan, b.DisplayName, b.RemainingPct, cdp.FormatResetCountdown(b.ResetUnix), runway)
				}
			}
			_ = tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func cmdTokens() *cobra.Command {
	var preset, mode, instFilter string
	var asCSV, asJSON bool
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "从本地 SQLite WAL 只读统计 Token 消耗、缓存节省与等值 API 账单",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			if mode == "" {
				mode = cfg.Pricing.Mode
			}
			rpcCli := rpc.NewClient()
			store := telemetry.NewStore(rpcCli.ModelLabel)
			instList := instances.DiscoverAll(cfg)
			rep := store.QueryTelemetry(instList, preset, mode, instFilter, cfg.Pricing.UltraMonthlyUSD)
			if asCSV {
				os.Stdout.Write(store.ExportCSV(rep))
				return
			}
			if asJSON {
				printJSON(rep)
				return
			}
			fmt.Printf("区间: %s | 计价: %s | 调用: %d | 扫描耗时: %.1fms\n",
				rep.PresetLabel, rep.Mode, rep.Summary.Calls, rep.ScanMs)
			fmt.Printf("Total Tokens: %d (In: %d | Cache: %d [命中 %.1f%%] | Out: %d)\n",
				rep.Summary.TotalTokens, rep.Summary.InputTokens, rep.Summary.CacheTokens,
				rep.Summary.CacheHitRate, rep.Summary.OutputTokens)
			fmt.Printf("等值账单: $%.2f USD (¥%.2f CNY) | 缓存节省: $%.2f | Main 本月回本: $%.2f / $%.0f (%.1fx)\n\n",
				rep.Summary.TotalUSD, rep.Summary.TotalUSD*cfg.Pricing.USDToCNY,
				rep.Summary.SavedByCacheUSD, rep.MonthROI.MainMonthUSD, rep.MonthROI.SubUSD, rep.MonthROI.ROIMultiple)

			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "MODEL\tCALLS\tINPUT\tCACHE\tOUTPUT\tUSD")
			for _, m := range rep.ByModel {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t$%.2f\n",
					m.ModelName, m.Calls, m.Input, m.Cache, m.Output, m.TotalUSD)
			}
			_ = tw.Flush()
		},
	}
	cmd.Flags().StringVar(&preset, "preset", "today", "today|yesterday|last_5h|last_24h|since_monday|last_7d|last_30d|this_month|all")
	cmd.Flags().StringVar(&mode, "mode", "", "std|promo|nc_std|nc_promo")
	cmd.Flags().StringVar(&instFilter, "instance", "all", "all|main|ide|p1...")
	cmd.Flags().BoolVar(&asCSV, "csv", false, "输出 CSV")
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出 JSON")
	return cmd
}

func cmdSearch() *cobra.Command {
	var instFilter string
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "跨全部实例只读全文检索 (FTS5) 历史会话与摘要",
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			cfg := loadCfg()
			q := strings.Join(args, " ")
			store := telemetry.NewStore(nil)
			instList := instances.DiscoverAll(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			hits := store.SearchConversations(ctx, instList, q, instFilter, limit)
			if asJSON {
				printJSON(hits)
				return
			}
			if len(hits) == 0 {
				fmt.Println("未找到匹配的会话记录。")
				return
			}
			for _, h := range hits {
				fmt.Printf("[%s] %s (%s · %d steps · %s)\n", h.InstanceID, h.Title, h.Workspace, h.StepCount, h.ModifiedAt)
				if h.Snippet != "" {
					fmt.Printf("  %s\n", strings.ReplaceAll(h.Snippet, "\n", " "))
				}
			}
		},
	}
	cmd.Flags().StringVar(&instFilter, "instance", "all", "过滤实例")
	cmd.Flags().IntVar(&limit, "limit", 25, "最大结果数")
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON 输出")
	return cmd
}

func cmdProfile() *cobra.Command {
	root := &cobra.Command{
		Use:   "profile",
		Short: "ParaGravity 分身沙盒生命周期管理 (拒绝操作 main 与 ide)",
	}

	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "列出全部分身沙盒状态",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tKIND\tRUNNING\tPID\tACCOUNT\tAUTH\tBROKEN_LINKS")
			for _, inst := range instances.DiscoverAll(cfg) {
				fmt.Fprintf(tw, "%s\t%s\t%v\t%d\t%s\t%s\t%d\n",
					inst.ID, inst.Kind, inst.Running, inst.MainPID, inst.Account, inst.AuthState, inst.BrokenSymlinks)
			}
			_ = tw.Flush()
		},
	})

	var ws string
	var forceHeal bool
	launchCmd := &cobra.Command{
		Use:   "launch <profile>",
		Short: "使用 Antigravity-Box 启动指定 ParaGravity 分身",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			pid, err := instances.LaunchProfile(loadCfg(), args[0], ws, forceHeal)
			if err != nil {
				return err
			}
			fmt.Printf("已启动 %s (PID %d)\n", args[0], pid)
			return nil
		},
	}
	launchCmd.Flags().StringVar(&ws, "workspace", "", "可选打开的工作区目录")
	launchCmd.Flags().BoolVar(&forceHeal, "heal", false, "启动前强制重建缓存")

	var forceStop bool
	stopCmd := &cobra.Command{
		Use:   "stop <profile>",
		Short: "安全停止指定 ParaGravity 分身 (严格禁止 main / ide)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := instances.StopProfile(loadCfg(), args[0], forceStop); err != nil {
				return err
			}
			fmt.Printf("已停止 %s\n", args[0])
			return nil
		},
	}
	stopCmd.Flags().BoolVar(&forceStop, "force", false, "使用 SIGKILL 强制终止")

	var healAll, healForce bool
	healCmd := &cobra.Command{
		Use:   "heal [profile]",
		Short: "清理过期锁、修复损坏符号链接并重建版本缓存",
		RunE: func(_ *cobra.Command, args []string) error {
			cfg := loadCfg()
			if healAll || len(args) == 0 {
				for _, inst := range instances.DiscoverAll(cfg) {
					if inst.Kind != instances.KindProfile {
						continue
					}
					rep, err := instances.HealProfile(cfg, inst.ID, healForce)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s: %v\n", inst.ID, err)
						continue
					}
					printJSON(rep)
				}
				return nil
			}
			rep, err := instances.HealProfile(cfg, args[0], healForce)
			if err != nil {
				return err
			}
			printJSON(rep)
			return nil
		},
	}
	healCmd.Flags().BoolVar(&healAll, "all", false, "修复全部 ParaGravity 分身")
	healCmd.Flags().BoolVar(&healForce, "force", false, "强制清理已停止分身的临时渲染缓存")

	var opts instances.CreateProfileOpts
	var noMCP bool
	createCmd := &cobra.Command{
		Use:   "create <profile>",
		Short: "创建新的 ParaGravity 隔离分身沙盒并注册 macOS 应用壳",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			opts.Name = args[0]
			opts.InheritMCP = !noMCP
			cfg := loadCfg()
			meta, err := instances.CreateProfile(cfg, opts)
			if err != nil {
				return err
			}
			_, _ = hooks.InstallHooks(cfg, "")
			printJSON(meta)
			if opts.Launch {
				pid, err := instances.LaunchProfile(cfg, opts.Name, opts.WorkspacePath, false)
				if err != nil {
					return err
				}
				fmt.Printf("已启动新分身 %s (PID %d)\n", opts.Name, pid)
			}
			return nil
		},
	}
	createCmd.Flags().StringVar(&opts.Description, "desc", "", "分身备注描述")
	createCmd.Flags().StringVar(&opts.LinksPolicy, "links", "minimal", "符号链接策略: minimal|full|none")
	createCmd.Flags().StringVar(&opts.InheritFrom, "inherit", "host", "配置继承源: host 或空")
	createCmd.Flags().BoolVar(&noMCP, "no-mcp", false, "跳过脱敏继承 mcp_config.json")
	createCmd.Flags().BoolVar(&opts.Launch, "launch", false, "创建后立即启动")
	createCmd.Flags().StringVar(&opts.WorkspacePath, "workspace", "", "启动时打开的工作区路径")

	root.AddCommand(launchCmd, stopCmd, healCmd, createCmd)
	return root
}

func cmdDoctor() *cobra.Command {
	var fix, asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "执行六维健康体检 (Box版本/URL Scheme/Keychain只读元数据/断链/OAuth/Hooks+Sidecar)",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			instList := instances.DiscoverAll(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			targets := cdp.NewWatcher().ProbeTargetStatus(ctx, instList)

			if fix {
				frep := doctor.FixSafe(cfg, "")
				instList = instances.DiscoverAll(cfg)
				rep := doctor.RunChecks(cfg, instList, targets)
				if asJSON {
					printJSON(map[string]any{"fix": frep, "doctor": rep})
					return
				}
				fmt.Printf("安全修复完成: %s\n\n", strings.Join(frep.Fixed, "; "))
				if len(frep.Errors) > 0 {
					fmt.Fprintf(os.Stderr, "警告: %s\n", strings.Join(frep.Errors, "; "))
				}
				printDoctorReport(rep)
				return
			}
			rep := doctor.RunChecks(cfg, instList, targets)
			if asJSON {
				printJSON(rep)
				return
			}
			printDoctorReport(rep)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "执行一键安全修复")
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func printDoctorReport(rep doctor.Report) {
	fmt.Printf("Antigravity Main: %s | Box: %s | IDE: %s\n", rep.MainVersion, rep.BoxVersion, rep.IDEVersion)
	for _, c := range rep.Checks {
		icon := "✓"
		if c.Status == "warn" {
			icon = "!"
		} else if c.Status == "error" {
			icon = "✗"
		}
		fmt.Printf("[%s] %-26s %s\n", icon, c.Title, c.Summary)
		if c.Detail != "" {
			fmt.Printf("    %s\n", c.Detail)
		}
	}
}

func cmdBoxSync() *cobra.Command {
	return &cobra.Command{
		Use:   "box-sync",
		Short: "将 Antigravity.app 版本同步至 Antigravity-Box.app (需先停止运行中的分身)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			out, err := doctor.SyncBox(ctx, loadCfg())
			if out != "" {
				fmt.Println(out)
			}
			return err
		},
	}
}

func cmdZh() *cobra.Command {
	root := &cobra.Command{
		Use:   "zh",
		Short: "CDP 中文补丁与 Shadow-DOM 窗口微胶囊探针/立即注入",
	}

	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "探测全部 Antigravity 窗口的 CDP 汉化与 HUD 状态",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			instList := instances.DiscoverAll(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			targets := cdp.NewWatcher().ProbeTargetStatus(ctx, instList)
			printJSON(targets)
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "inject",
		Short: "立即向所有运行中的 Antigravity CDP 窗口注入中文补丁与 HUD",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			// If local Hub is running, ask Hub to execute SyncCDP so HUD carries fresh telemetry.
			hc := &http.Client{Timeout: 4 * time.Second}
			if resp, err := hc.Post("http://"+cfg.ListenAddr+"/api/cdp/inject-now", "application/json", strings.NewReader("{}")); err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var res map[string]any
					if json.NewDecoder(resp.Body).Decode(&res) == nil {
						printJSON(res)
						return
					}
				}
			}
			srv := hub.NewServer(cfg, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv.PollOnce(ctx)
			targets := srv.SyncCDPOnce(ctx, true)
			printJSON(targets)
		},
	})

	return root
}

func cmdCache() *cobra.Command {
	root := &cobra.Command{
		Use:   "cache",
		Short: "安全扫描并清理超龄 Brain 与 Conversations UUID 缓存",
	}

	var keep int
	var asJSON bool
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "只读统计可回收的旧对话目录",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			store := telemetry.NewStore(nil)
			sum := store.ScanCache(instances.DiscoverAll(cfg), keep, nil)
			if asJSON {
				printJSON(sum)
				return
			}
			fmt.Printf("总占用: %.1f MB | 安全可回收 (保留最近 %d 条): %.1f MB | 总条目: %d\n",
				float64(sum.TotalBytes)/(1024*1024), sum.KeepCount,
				float64(sum.Reclaimable)/(1024*1024), len(sum.Items))
		},
	}
	scanCmd.Flags().IntVar(&keep, "keep", 20, "每个实例保留最近多少条会话")
	scanCmd.Flags().BoolVar(&asJSON, "json", false, "JSON 输出")

	var cleanKeep int
	var cleanInst string
	cleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "清理非活跃且超出保留数的 UUID 会话目录",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			store := telemetry.NewStore(nil)
			res := store.CleanCache(instances.DiscoverAll(cfg), cleanKeep, cleanInst, nil)
			fmt.Printf("已清理 %d 个过期会话，释放 %.2f MB\n", res.DeletedCount, float64(res.FreedBytes)/(1024*1024))
		},
	}
	cleanCmd.Flags().IntVar(&cleanKeep, "keep", 20, "每个实例保留最近多少条会话")
	cleanCmd.Flags().StringVar(&cleanInst, "instance", "all", "目标实例过滤")

	root.AddCommand(scanCmd, cleanCmd)
	return root
}

func cmdInstallHooks() *cobra.Command {
	return &cobra.Command{
		Use:   "install-hooks",
		Short: "无损合并安装 Antigravity Hooks（保留 herdr / marktoast）并注册原生 Sidecar",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadCfg()
			home, _ := os.UserHomeDir()
			cliBin := filepath.Join(home, ".local", "bin", "agymate")
			if exe, err := os.Executable(); err == nil && strings.Contains(exe, ".local/bin") {
				cliBin = exe
			}
			updated, err := hooks.InstallHooks(cfg, cliBin)
			if err != nil {
				return err
			}
			scPath, err := doctor.EnsureSidecar(cliBin)
			if err != nil {
				return err
			}
			fmt.Printf("Hooks 已无损更新 (%d 个目标):\n", len(updated))
			for _, p := range updated {
				fmt.Println("  -", p)
			}
			fmt.Println("原生 Sidecar 已注册:", scPath)
			return nil
		},
	}
}

func cmdHook() *cobra.Command {
	var event string
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "接收 Antigravity stdin Hook JSON 并于 <100ms 转发至 Hub",
		Hidden: true,
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			out := hooks.IngestFromStdin(cfg.ListenAddr, event)
			os.Stdout.Write(out)
			os.Stdout.Write([]byte("\n"))
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "Hook event override")
	return cmd
}

func cmdSidecarProxy() *cobra.Command {
	return &cobra.Command{
		Use:    "sidecar-proxy",
		Short:  "以 Antigravity Sidecar AUX_PANE 运行本地代理",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadCfg()
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return doctor.RunSidecarProxy(ctx, cfg.ListenAddr)
		},
	}
}

func cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出 AgyMate 版本号",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("agymate %s\n", version.Version)
		},
	}
}
