// Command agymate is the unified CLI and headless local HTTP Hub for AgyMate.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realchendahuang/AgyMate/frontend"
	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/colab"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/hub"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/mcpapp"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/service"
	"github.com/realchendahuang/AgyMate/internal/setup"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"github.com/realchendahuang/AgyMate/internal/version"
)

func main() {
	root := &cobra.Command{
		Use:     "agymate",
		Short:   "AgyMate — Antigravity 实例配额、Token 账单、上下文水位与多开管理",
		Version: version.Full(),
	}

	root.AddCommand(
		cmdSetup(),
		cmdUninstall(),
		cmdConfig(),
		cmdServe(),
		cmdService(),
		cmdLaunch(),
		cmdStatus(),
		cmdQuota(),
		cmdColab(),
		cmdTokens(),
		cmdSearch(),
		cmdProfile(),
		cmdDoctor(),
		cmdZh(),
		cmdCache(),
		cmdInstallHooks(),
		cmdHook(),
		cmdSidecarProxy(),
		cmdMCP(),
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

func hubRequest(method, rawURL string, body []byte, timeout time.Duration) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := config.LoadHubToken(); tok != "" {
		req.Header.Set("X-AgyMate-Token", tok)
	}
	hc := &http.Client{Timeout: timeout}
	return hc.Do(req)
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
				BaseContext:       func(_ net.Listener) context.Context { return ctx },
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
		Short: "显示所有账号与客户端状态、配额、上下文与今日账单",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			srv := hub.NewServer(cfg, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			srv.PollOnce(ctx)
			ov := srv.BuildOverview()
			if asJSON {
				printJSON(ov)
				return
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "账号\t实例槽位\t状态\t套餐\t配额\t上下文\t今日账单")
			for _, acct := range ov.Accounts {
				slotNames := make([]string, 0, len(acct.Instances))
				for _, iv := range acct.Instances {
					slotNames = append(slotNames, iv.ID)
				}
				slotsStr := strings.Join(slotNames, "+")
				if slotsStr == "" {
					slotsStr = "-"
				}
				stateStr := cliLiveState(acct.LiveState)
				if acct.CachedQuota {
					stateStr += " (缓存)"
				}
				plan := acct.PlanName
				if plan == "" {
					plan = "-"
				}
				quotaStr := "-"
				if len(acct.Buckets) > 0 {
					picked := acct.Buckets[0]
					for _, b := range acct.Buckets {
						if !b.Disabled {
							picked = b
							break
						}
					}
					if picked.Disabled {
						quotaStr = "不适用"
					} else {
						q := fmt.Sprintf("%.0f%%", picked.RemainingPct)
						if acct.CachedQuota {
							q += "*"
						}
						quotaStr = q
					}
				}
				ctxPct := "-"
				if acct.Context != nil {
					ctxPct = fmt.Sprintf("%.0f%%", acct.Context.UsagePct)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					acct.DisplayName, slotsStr, stateStr, plan, quotaStr, ctxPct, cliMoney(acct.TodayUSD, ov.Pricing))
			}
			_ = tw.Flush()
			if ov.Colab.Enabled {
				fmt.Println()
				if len(ov.Colab.Accounts) > 0 {
					fmt.Printf("Colab 云算力池: %.2f CCU ($%.2f) | 消耗: %.2f CCU/小时 | 活跃实例: %d 台 (%d 个账号)\n",
						ov.Colab.CurrentBalance, ov.Colab.EstimatedValueUSD,
						ov.Colab.ConsumptionRateHourly, ov.Colab.AssignmentsCount, len(ov.Colab.Accounts))
					for _, ca := range ov.Colab.Accounts {
						name := ca.Account
						if name == "" {
							name = "<未知账号>"
						}
						if ca.OK {
							fmt.Printf("  - %-28s [%s] %.2f CCU | %.2f/小时 | %d 台\n",
								name, ca.Source, ca.CurrentBalance, ca.ConsumptionRateHourly, ca.AssignmentsCount)
						} else {
							fmt.Printf("  - %-28s [%s] %s (%s)\n",
								name, ca.Source, cliColabStatus(ca.Status), ca.LastError)
						}
					}
				} else if ov.Colab.OK {
					hw := strings.Join(ov.Colab.EligibleGPUs, ",")
					if hw == "" {
						hw = "-"
					}
					fmt.Printf("Colab 云算力: %.2f CCU ($%.2f) | 消耗: %.2f CCU/小时 | 活跃实例: %d 台 | 加速规格: %s\n",
						ov.Colab.CurrentBalance, ov.Colab.EstimatedValueUSD,
						ov.Colab.ConsumptionRateHourly, ov.Colab.AssignmentsCount, hw)
				} else {
					fmt.Printf("Colab 云算力: %s (%s)\n", cliColabStatus(ov.Colab.Status), ov.Colab.LastError)
				}
			}
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func cmdColab() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "colab",
		Short: "管理多账号 Google Colab CCU 算力池、绑定 OAuth 与一键清场",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			snap := fetchColabViaHubOrCLI(cfg)
			if asJSON {
				printJSON(snap)
				return
			}
			printColabSnapshot(snap)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")

	var loginHint string
	var noBrowser bool
	var loginJSON bool
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "通过 Google OAuth2 绑定一个新的 Colab 算力账号",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadCfg()
			reqBody, _ := json.Marshal(map[string]any{
				"loginHint":   strings.TrimSpace(loginHint),
				"openBrowser": !noBrowser,
			})
			resp, err := hubRequest(http.MethodPost, "http://"+cfg.ListenAddr+"/api/colab/accounts/login", reqBody, 8*time.Second)
			if err != nil {
				return fmt.Errorf("无法连接本地 Hub (%s)：请先启动 `agymate serve` 或检查 `agymate service status`（OAuth 回调需由 Hub 接收）", cfg.ListenAddr)
			}
			defer resp.Body.Close()
			var out struct {
				OK      bool   `json:"ok"`
				AuthURL string `json:"authUrl"`
				State   string `json:"state"`
				Opened  bool   `json:"opened"`
				Error   string `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return err
			}
			if resp.StatusCode >= 400 || !out.OK {
				return errors.New(out.Error)
			}
			if loginJSON {
				printJSON(out)
				return nil
			}
			if out.Opened {
				fmt.Println("已在默认浏览器打开 Google 授权页，完成授权后会自动回传至本地 Hub。")
			} else {
				fmt.Println("请在浏览器打开以下链接完成 Google 授权：")
			}
			fmt.Println(out.AuthURL)
			return nil
		},
	}
	loginCmd.Flags().StringVar(&loginHint, "hint", "", "预填 Google 账号邮箱 (login_hint)")
	loginCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "仅输出授权 URL，不自动拉起浏览器")
	loginCmd.Flags().BoolVar(&loginJSON, "json", false, "以 JSON 格式输出")

	var removeJSON bool
	removeCmd := &cobra.Command{
		Use:   "remove <email>",
		Short: "解绑指定 Google 账号（或隐藏 colab-safe 默认账号）",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			email := strings.TrimSpace(args[0])
			cfg := loadCfg()
			removed, snap, err := removeColabViaHubOrCLI(cfg, email)
			if removeJSON {
				printJSON(map[string]any{
					"ok":      err == nil,
					"removed": removed,
					"colab":   snap,
					"error":   errString(err),
				})
				return err
			}
			if err != nil {
				return err
			}
			if removed {
				fmt.Printf("已解绑 Colab OAuth 账号: %s\n", email)
			} else {
				fmt.Printf("已隐藏 Colab 账号: %s\n", email)
			}
			return nil
		},
	}
	removeCmd.Flags().BoolVar(&removeJSON, "json", false, "以 JSON 格式输出")

	var nukeJSON bool
	var nukeAccount string
	nukeCmd := &cobra.Command{
		Use:   "nuke",
		Short: "一键强制销毁云端活跃 Colab VM（可指定 --account 仅清场单个账号）",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadCfg()
			res, snap, err := nukeColabViaHubOrCLI(cfg, nukeAccount)
			if nukeJSON {
				printJSON(map[string]any{
					"ok":    err == nil && res.OK,
					"nuke":  res,
					"colab": snap,
					"error": errString(err),
				})
				return err
			}
			fmt.Printf("清场完成: 销毁 %d 台 | 失败 %d 台 | 剩余活跃 %d 台\n",
				res.TerminatedCount, res.FailedCount, res.RemainingCount)
			for _, t := range res.Terminated {
				tag := t.Name
				if t.Orphan {
					tag = "? (孤儿)"
				}
				acctPart := ""
				if t.Account != "" {
					acctPart = " · " + t.Account
				}
				fmt.Printf("  - [%s] %s (%s%s)\n", tag, t.Endpoint, t.Accelerator, acctPart)
			}
			fmt.Printf("当前余额: %.2f CCU | 速率: %.2f CCU/h\n", snap.CurrentBalance, snap.ConsumptionRateHourly)
			return err
		},
	}
	nukeCmd.Flags().BoolVar(&nukeJSON, "json", false, "以 JSON 格式输出")
	nukeCmd.Flags().StringVar(&nukeAccount, "account", "", "仅清场指定 Google 账号邮箱")

	cmd.AddCommand(loginCmd, removeCmd, nukeCmd)
	return cmd
}

func newConfiguredColabCollector() *colab.Collector {
	st := config.LoadState("")
	c := colab.NewCollector()
	c.SetHiddenChecker(st.IsColabHidden)
	return c
}

func fetchColabViaHubOrCLI(cfg config.Config) colab.Snapshot {
	if resp, err := hubRequest(http.MethodGet, "http://"+cfg.ListenAddr+"/api/colab?refresh=1", nil, 8*time.Second); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var snap colab.Snapshot
			if json.NewDecoder(resp.Body).Decode(&snap) == nil {
				return snap
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return newConfiguredColabCollector().Probe(ctx, cfg.Colab, true)
}

func removeColabViaHubOrCLI(cfg config.Config, email string) (bool, colab.Snapshot, error) {
	reqBody, _ := json.Marshal(map[string]string{"email": email})
	if resp, err := hubRequest(http.MethodPost, "http://"+cfg.ListenAddr+"/api/colab/accounts/remove", reqBody, 10*time.Second); err == nil {
		defer resp.Body.Close()
		var out struct {
			OK      bool           `json:"ok"`
			Removed bool           `json:"removed"`
			Error   string         `json:"error"`
			Colab   colab.Snapshot `json:"colab"`
		}
		if json.NewDecoder(resp.Body).Decode(&out) == nil && (out.OK || out.Error != "") {
			if out.Error != "" {
				return out.Removed, out.Colab, errors.New(out.Error)
			}
			return out.Removed, out.Colab, nil
		}
	}
	st := config.LoadState("")
	c := colab.NewCollector()
	c.SetHiddenChecker(st.IsColabHidden)
	removed, err := c.RemoveAccount(email)
	if err != nil {
		return false, colab.Snapshot{}, err
	}
	st.SetColabHidden(email, true)
	_ = st.Save()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return removed, c.Probe(ctx, cfg.Colab, true), nil
}

func nukeColabViaHubOrCLI(cfg config.Config, account string) (colab.NukeResult, colab.Snapshot, error) {
	reqBody, _ := json.Marshal(map[string]string{"account": strings.TrimSpace(account)})
	if resp, err := hubRequest(http.MethodPost, "http://"+cfg.ListenAddr+"/api/colab/nuke", reqBody, 40*time.Second); err == nil {
		defer resp.Body.Close()
		var payload struct {
			OK    bool             `json:"ok"`
			Error string           `json:"error"`
			Nuke  colab.NukeResult `json:"nuke"`
			Colab colab.Snapshot   `json:"colab"`
		}
		if json.NewDecoder(resp.Body).Decode(&payload) == nil && (payload.OK || payload.Error != "") {
			if payload.Error != "" {
				return payload.Nuke, payload.Colab, errors.New(payload.Error)
			}
			return payload.Nuke, payload.Colab, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return newConfiguredColabCollector().NukeAccount(ctx, cfg.Colab, account)
}

func printColabSnapshot(snap colab.Snapshot) {
	if !snap.Enabled {
		fmt.Println("Colab 云算力雷达已在配置中禁用。")
		return
	}
	if len(snap.Accounts) > 0 {
		fmt.Printf("算力池总计 (%d 个账号): 余额 %.2f CCU ($%.2f) | 速率 %.2f CCU/小时 ($%.2f/小时) | 活跃实例 %d 台 (孤儿 %d 台)\n",
			len(snap.Accounts), snap.CurrentBalance, snap.EstimatedValueUSD,
			snap.ConsumptionRateHourly, snap.BurnUSDPerHour, snap.AssignmentsCount, snap.OrphanCount)
		fmt.Println()
		atw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(atw, "账号\t来源\t状态\t余额(CCU)\t速率(/小时)\t活跃实例\t可用GPU")
		for _, ca := range snap.Accounts {
			acc := ca.Account
			if acc == "" {
				acc = "<未知账号>"
			}
			gpus := strings.Join(ca.EligibleGPUs, ",")
			if gpus == "" {
				gpus = "-"
			}
			fmt.Fprintf(atw, "%s\t%s\t%s\t%.2f\t%.2f\t%d\t%s\n",
				acc, ca.Source, cliColabStatus(ca.Status), ca.CurrentBalance, ca.ConsumptionRateHourly, ca.AssignmentsCount, gpus)
		}
		_ = atw.Flush()
	} else {
		if !snap.OK {
			fmt.Printf("状态: %s\n", cliColabStatus(snap.Status))
			if snap.LastError != "" {
				fmt.Printf("详情: %s\n", snap.LastError)
			}
			return
		}
		acc := snap.Account
		if acc == "" {
			acc = "<未知账号>"
		}
		fmt.Printf("账号: %s | 状态: %s | 活跃实例: %d 台 (孤儿: %d 台)\n",
			acc, cliColabStatus(snap.Status), snap.AssignmentsCount, snap.OrphanCount)
		fmt.Printf("余额: %.2f CCU (等值 $%.2f USD / 月基准 %.0f CCU) | 速率: %.2f CCU/小时 ($%.2f/小时)\n",
			snap.CurrentBalance, snap.EstimatedValueUSD, snap.MonthlyAllowance,
			snap.ConsumptionRateHourly, snap.BurnUSDPerHour)
		if snap.HoursUntilEmpty > 0 {
			fmt.Printf("预计耗尽: %.1f 小时 | 已持续运行: %d 分钟\n", snap.HoursUntilEmpty, snap.ActiveMinutes)
		}
		if len(snap.EligibleGPUs) > 0 || len(snap.EligibleTPUs) > 0 {
			fmt.Printf("加速池: GPU=[%s] | TPU=[%s]\n",
				strings.Join(snap.EligibleGPUs, ", "), strings.Join(snap.EligibleTPUs, ", "))
		}
	}
	if len(snap.Sessions) == 0 {
		return
	}
	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "账号\t实例名\t孤儿实例\t加速规格\t类型\t算力档位\t终端节点")
	for _, s := range snap.Sessions {
		acc := s.Account
		if acc == "" {
			acc = "-"
		}
		orphanTxt := "否"
		if s.Orphan {
			orphanTxt = "是"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			acc, s.Name, orphanTxt, s.Accelerator, s.Variant, s.Shape, s.Endpoint)
	}
	_ = tw.Flush()
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
				Instance string            `json:"instance"`
				Account  string            `json:"account"`
				Plan     string            `json:"plan"`
				Buckets  []rpc.QuotaBucket `json:"buckets"`
				Models   []rpc.ModelQuota  `json:"models"`
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
			fmt.Fprintln(tw, "实例\t套餐\t配额项\t剩余\t重置倒计时\t预计耗尽")
			for _, e := range out {
				for _, b := range e.Buckets {
					if b.Disabled {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
							e.Instance, e.Plan, b.DisplayName, "不适用", "-", "受上级窗口限制")
						continue
					}
					runway := "-"
					if b.ExhaustsBeforeReset && b.HoursUntilEmpty > 0 {
						runway = fmt.Sprintf("⚠ 约 %.1f 小时耗尽 (%.1f%%/小时)", b.HoursUntilEmpty, b.BurnPctPerHour)
					} else if b.BurnPctPerHour > 0 {
						runway = fmt.Sprintf("%.1f%%/小时", b.BurnPctPerHour)
					}
					resetTxt := cdp.FormatResetCountdown(b.ResetUnix)
					if resetTxt == "" {
						resetTxt = "-"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f%%\t%s\t%s\n",
						e.Instance, e.Plan, b.DisplayName, b.RemainingPct, resetTxt, runway)
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
			subUSD := cfg.Pricing.UltraMonthlyUSD
			if subUSD <= 0 {
				st := config.LoadState("")
				for _, snap := range st.AllAccountSnapshots(time.Now()) {
					low := strings.ToLower(snap.PlanName + " " + snap.TierID)
					if strings.Contains(low, "ultra") {
						subUSD += 200
					} else if strings.Contains(low, "pro") {
						subUSD += 20
					}
				}
			}
			rep := store.QueryTelemetry(instList, preset, mode, instFilter, subUSD)
			if asCSV {
				os.Stdout.Write(store.ExportCSV(rep))
				return
			}
			if asJSON {
				printJSON(rep)
				return
			}
			fmt.Printf("区间: %s | 计价口径: %s | 调用次数: %d | 扫描耗时: %.1fms\n",
				rep.PresetLabel, cliPricingMode(rep.Mode), rep.Summary.Calls, rep.ScanMs)
			fmt.Printf("Token 总计: %d（输入: %d | 缓存读取: %d [命中率 %.1f%%] | 输出: %d）\n",
				rep.Summary.TotalTokens, rep.Summary.InputTokens, rep.Summary.CacheTokens,
				rep.Summary.CacheHitRate, rep.Summary.OutputTokens)
			roiText := fmt.Sprintf("本月累计: $%.2f", rep.MonthROI.AllMonthUSD)
			if rep.MonthROI.SubUSD > 0 {
				roiText = fmt.Sprintf("本月回本: $%.2f / $%.0f (%.1fx)",
					rep.MonthROI.AllMonthUSD, rep.MonthROI.SubUSD, rep.MonthROI.ROIMultiple)
			}
			fmt.Printf("等值账单: $%.2f USD (¥%.2f CNY) | 缓存节省: $%.2f | %s\n\n",
				rep.Summary.TotalUSD, rep.Summary.TotalUSD*cfg.Pricing.USDToCNY,
				rep.Summary.SavedByCacheUSD, roiText)

			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "模型\t调用次数\t输入\t缓存读取\t输出\t等值账单")
			for _, m := range rep.ByModel {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\n",
					m.ModelName, m.Calls, m.Input, m.Cache, m.Output, cliMoney(m.TotalUSD, cfg.Pricing))
			}
			_ = tw.Flush()
		},
	}
	cmd.Flags().StringVar(&preset, "preset", "today", "today|yesterday|last_5h|last_24h|since_monday|last_7d|last_30d|this_month|all")
	cmd.Flags().StringVar(&mode, "mode", "", "std|promo|nc_std|nc_promo")
	cmd.Flags().StringVar(&instFilter, "instance", "all", "all|main|ide|<profile>")
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
				fmt.Printf("[%s] %s (%s · %d 步 · %s)\n", h.InstanceID, h.Title, h.Workspace, h.StepCount, h.ModifiedAt)
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
		Short: "隔离实例沙盒生命周期管理 (拒绝操作 main 与 ide)",
	}

	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "列出全部实例与沙盒状态",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "实例ID\t类型\t运行状态\tPID\t绑定账号\t凭证状态\t损坏链接")
			for _, inst := range instances.DiscoverAll(cfg) {
				runTxt := "已停止"
				if inst.Running {
					runTxt = "运行中"
				}
				acct := inst.Account
				if acct == "" {
					acct = "-"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%d\n",
					inst.ID, cliInstKind(inst.Kind), runTxt, inst.MainPID, acct, cliAuthState(inst.AuthState), inst.BrokenSymlinks)
			}
			_ = tw.Flush()
		},
	})

	var ws string
	var forceHeal bool
	launchCmd := &cobra.Command{
		Use:   "launch <profile>",
		Short: "启动指定隔离实例",
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
		Short: "安全停止指定隔离实例 (严格禁止 main / ide)",
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
	healCmd.Flags().BoolVar(&healAll, "all", false, "修复全部隔离实例")
	healCmd.Flags().BoolVar(&healForce, "force", false, "强制清理已停止实例的临时渲染缓存")

	var opts instances.CreateProfileOpts
	var noMCP bool
	createCmd := &cobra.Command{
		Use:   "create <profile>",
		Short: "创建新的隔离实例沙盒",
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
				fmt.Printf("已启动新实例 %s (PID %d)\n", opts.Name, pid)
			}
			return nil
		},
	}
	createCmd.Flags().StringVar(&opts.Description, "desc", "", "实例备注描述")
	createCmd.Flags().StringVar(&opts.LinksPolicy, "links", "minimal", "符号链接策略: minimal|full|none")
	createCmd.Flags().StringVar(&opts.InheritFrom, "inherit", "host", "配置继承源: host 或空")
	createCmd.Flags().BoolVar(&noMCP, "no-mcp", false, "跳过脱敏继承 mcp_config.json")
	createCmd.Flags().BoolVar(&opts.Launch, "launch", false, "创建后立即启动")
	createCmd.Flags().StringVar(&opts.WorkspacePath, "workspace", "", "启动时打开的工作区路径")

	var forceDelete bool
	deleteCmd := &cobra.Command{
		Use:   "delete <profile>",
		Short: "删除隔离实例目录及启动器 (严格禁止 main / ide)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := instances.DeleteProfile(loadCfg(), args[0], forceDelete); err != nil {
				return err
			}
			fmt.Printf("已删除隔离实例 %s\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&forceDelete, "force", false, "若实例仍在运行先停止再删除")

	root.AddCommand(launchCmd, stopCmd, healCmd, createCmd, deleteCmd)
	return root
}

func cmdDoctor() *cobra.Command {
	var fix, asJSON, redact bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "执行健康体检 (客户端版本/URL Scheme/Keychain只读元数据/断链/OAuth/Hooks+Sidecar)",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			instList := instances.DiscoverAll(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			targets := cdp.NewWatcher().ProbeTargetStatus(ctx, instList)

			if fix {
				frep := doctor.FixSafe(cfg, setup.ResolveCLIBin(""))
				instList = instances.DiscoverAll(cfg)
				rep := doctor.RunChecks(cfg, instList, targets)
				if redact {
					rep = redactDoctorReport(rep)
				}
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
			if redact {
				if asJSON {
					printJSON(doctorDiagnosticBundle(rep))
					return
				}
				printDoctorReport(redactDoctorReport(rep))
				return
			}
			if asJSON {
				printJSON(rep)
				return
			}
			printDoctorReport(rep)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "执行一键安全修复")
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	cmd.Flags().BoolVar(&redact, "redact", false, "脱敏路径与邮箱（便于直接附在 GitHub Issue 中排障）")
	return cmd
}

func printDoctorReport(rep doctor.Report) {
	if rep.IDEVersion != "" {
		fmt.Printf("Antigravity: %s | IDE: %s\n", rep.MainVersion, rep.IDEVersion)
	} else {
		fmt.Printf("Antigravity: %s\n", rep.MainVersion)
	}
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

func cmdZh() *cobra.Command {
	root := &cobra.Command{
		Use:   "zh",
		Short: "Antigravity 客户端中文汉化与 Shadow-DOM 右上角悬浮窗管理",
	}

	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "探测全部 Antigravity 窗口的 CDP 汉化与悬浮窗状态",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			instList := instances.DiscoverAll(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			targets := cdp.NewWatcher().ProbeTargetStatus(ctx, instList)
			if targets == nil {
				targets = []cdp.TargetStatus{}
			}
			zhSource, ext := cdp.ResolveZhSource(cfg.CDP.ZhScriptPath)
			printJSON(map[string]any{
				"cdpEnabled":       cfg.CDP.Enabled,
				"zhEnabled":        cfg.CDP.ZhInject,
				"windowHud":        cfg.CDP.WindowHUD,
				"hudPosition":      cfg.CDP.HUDPosition,
				"zhSource":         zhSource,
				"externalOverride": ext,
				"targets":          targets,
			})
		},
	})

	var onScript string
	var useBuiltin bool
	onCmd := &cobra.Command{
		Use:   "on",
		Short: "启用 Antigravity 客户端动态中文汉化（默认使用内置引擎）",
		RunE: func(_ *cobra.Command, _ []string) error {
			var scriptPtr *string
			if useBuiltin {
				empty := ""
				scriptPtr = &empty
			} else if strings.TrimSpace(onScript) != "" {
				s := strings.TrimSpace(onScript)
				scriptPtr = &s
			}
			synced, src, err := updateZhViaHubOrConfig(true, scriptPtr)
			if err != nil {
				return err
			}
			fmt.Printf("已启用 Antigravity 客户端中文汉化（来源: %s）\n", src)
			if synced {
				fmt.Println("已通知本地 Hub 立即向全部 CDP 窗口同步汉化。")
			} else {
				fmt.Printf("已写入配置 %s（未检测到运行中的 Hub，重启服务后自动生效）。\n", config.ConfigPath())
			}
			fmt.Println("提示：Antigravity 2.33+ 官方客户端请通过 `agymate launch`（或「Antigravity (AgyMate).app」）启动，隔离多开实例则会自动附加 CDP 端口。")
			return nil
		},
	}
	onCmd.Flags().StringVar(&onScript, "script", "", "指定外部自定义 inject.js 路径覆盖内置汉化引擎")
	onCmd.Flags().BoolVar(&useBuiltin, "builtin", false, "清除外部脚本覆盖，切回 AgyMate 内置汉化引擎")

	offCmd := &cobra.Command{
		Use:   "off",
		Short: "关闭 Antigravity 客户端中文汉化（已汉化窗口重启后恢复原版英文）",
		RunE: func(_ *cobra.Command, _ []string) error {
			synced, _, err := updateZhViaHubOrConfig(false, nil)
			if err != nil {
				return err
			}
			fmt.Println("已关闭 Antigravity 客户端中文汉化。")
			if synced {
				fmt.Println("已同步至本地 Hub（当前已注入窗口重启或刷新后将恢复原版英文）。")
			}
			return nil
		},
	}

	root.AddCommand(onCmd, offCmd)

	root.AddCommand(&cobra.Command{
		Use:     "inject",
		Aliases: []string{"sync"},
		Short:   "立即向所有运行中的 Antigravity CDP 窗口同步中文汉化与右上角悬浮窗",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			// If local Hub is running, ask Hub to execute SyncCDP so HUD carries fresh telemetry.
			for _, ep := range []string{"/api/cdp/sync", "/api/cdp/inject-now"} {
				resp, err := hubRequest(http.MethodPost, "http://"+cfg.ListenAddr+ep, []byte("{}"), 4*time.Second)
				if err != nil {
					break
				}
				var res map[string]any
				ok := resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&res) == nil
				resp.Body.Close()
				if ok {
					printJSON(res)
					return
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
		Short: "无损合并安装 Antigravity Hooks（保留已有第三方 Hooks）并注册原生 Sidecar",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadCfg()
			cliBin := setup.ResolveCLIBin("")
			updated, err := hooks.InstallHooks(cfg, cliBin)
			if err != nil {
				return err
			}
			scPaths, err := doctor.EnsureSidecarForProfiles(cfg, cliBin)
			if err != nil {
				return err
			}
			fmt.Printf("Hooks 已无损更新 (%d 个目标):\n", len(updated))
			for _, p := range updated {
				fmt.Println("  -", p)
			}
			fmt.Printf("原生 Sidecar 已注册 (%d 个目标):\n", len(scPaths))
			for _, p := range scPaths {
				fmt.Println("  -", p)
			}
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

func cmdMCP() *cobra.Command {
	return &cobra.Command{
		Use:    "mcp",
		Short:  "以 Antigravity 原生 stdio MCP Server & MCP App 模式运行",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := loadCfg()
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return mcpapp.NewServer(cfg).ServeStdio(ctx)
		},
	}
}

func cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出 AgyMate 版本号",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("agymate %s\n", version.Full())
		},
	}
}

func cmdService() *cobra.Command {
	root := &cobra.Command{
		Use:   "service",
		Short: "管理 AgyMate Hub 后台常驻服务 (macOS LaunchAgent / Linux systemd)",
	}

	var binPath string
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "安装并启动 AgyMate Hub 后台服务（自动迁移旧服务配置）",
		RunE: func(_ *cobra.Command, _ []string) error {
			msg, err := service.Install(binPath)
			if err != nil {
				return err
			}
			fmt.Println(msg)
			return nil
		},
	}
	installCmd.Flags().StringVar(&binPath, "bin", "", "agymate 可执行文件路径（默认当前二进制）")

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "停止并移除 AgyMate Hub 后台服务",
		RunE: func(_ *cobra.Command, _ []string) error {
			msg, err := service.Uninstall()
			if err != nil {
				return err
			}
			fmt.Println(msg)
			return nil
		},
	}

	restartCmd := &cobra.Command{
		Use:   "restart",
		Short: "重启 AgyMate Hub 后台服务",
		RunE: func(_ *cobra.Command, _ []string) error {
			msg, err := service.Restart()
			if err != nil {
				return err
			}
			fmt.Println(msg)
			return nil
		},
	}

	var asJSON bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "查看 AgyMate Hub 后台服务运行状态",
		RunE: func(_ *cobra.Command, _ []string) error {
			info, err := service.Status()
			if err != nil {
				return err
			}
			if asJSON {
				printJSON(info)
				return nil
			}
			instTxt := "未安装"
			if info.Installed {
				instTxt = "已安装"
			}
			runTxt := "未运行"
			if info.Running {
				runTxt = "运行中"
			}
			fmt.Printf("服务管理: %s (%s)\n安装状态: %s\n运行状态: %s\n",
				info.Manager, info.Label, instTxt, runTxt)
			if info.UnitPath != "" {
				fmt.Printf("服务文件: %s\n", info.UnitPath)
			}
			if info.Detail != "" {
				fmt.Printf("运行详情: %s\n", info.Detail)
			}
			return nil
		},
	}
	statusCmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")

	root.AddCommand(installCmd, uninstallCmd, restartCmd, statusCmd)
	return root
}

func cliLiveState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "waiting_approval", "waiting":
		return "待确认"
	case "running":
		return "运行中"
	case "offline":
		return "离线"
	default:
		return "空闲"
	}
}

func cliColabStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "authenticated":
		return "已授权"
	case "unauthenticated":
		return "未授权"
	case "missing_cli":
		return "缺少CLI"
	case "disabled":
		return "已禁用"
	case "empty":
		return "未绑定"
	case "error":
		return "连接异常"
	default:
		if status == "" {
			return "-"
		}
		return status
	}
}

func cliPricingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "promo":
		return "优惠价+缓存折扣"
	case "nc_std":
		return "标准价无缓存"
	case "nc_promo":
		return "优惠价无缓存"
	default:
		return "标准价+缓存折扣"
	}
}

func cliInstKind(kind string) string {
	switch kind {
	case instances.KindMain:
		return "主客户端"
	case instances.KindIDE:
		return "IDE客户端"
	case instances.KindProfile:
		return "隔离沙盒"
	case instances.KindCustom:
		return "外部目录"
	default:
		return kind
	}
}

func cliAuthState(auth string) string {
	switch auth {
	case "ok":
		return "有效"
	case "missing":
		return "未登录"
	case "expired":
		return "已过期"
	case "no_refresh_token":
		return "无刷新令牌"
	case "keychain_managed":
		return "钥匙串托管"
	default:
		return auth
	}
}

func cliMoney(usd float64, pricing config.PricingConfig) string {
	if strings.EqualFold(pricing.Currency, "CNY") {
		rate := pricing.USDToCNY
		if rate <= 0 {
			rate = 7.20
		}
		return fmt.Sprintf("¥%.2f", usd*rate)
	}
	return fmt.Sprintf("$%.2f", usd)
}
