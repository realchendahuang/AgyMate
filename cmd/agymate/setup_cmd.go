package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	agymate "github.com/realchendahuang/AgyMate"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/setup"
	"github.com/realchendahuang/AgyMate/internal/version"
)

func cmdSetup() *cobra.Command {
	var opts setup.Options
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "一键幂等初始化与部署（配置迁移、后台服务、Hooks、原生插件 MCP App、Sidecar）",
		RunE: func(_ *cobra.Command, _ []string) error {
			opts.AntigravityPluginFS = agymate.AntigravityPluginFS()
			opts.MagpiePluginFS = agymate.MagpiePluginFS()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			rep := setup.Run(ctx, opts)
			if asJSON {
				printJSON(rep)
				if !rep.OK {
					return fmt.Errorf("setup encountered errors")
				}
				return nil
			}
			printSetupReport(rep, "部署")
			if !rep.OK {
				return fmt.Errorf("部分步骤执行失败，请查看上方提示或运行 `agymate doctor`")
			}
			if !rep.DryRun {
				fmt.Printf("\n==> 完成！Web 控制台: http://%s | 插件 MCP App: agymate_hub | CLI: %s\n", rep.ListenAddr, rep.BinPath)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.BinPath, "bin", "", "指定注册使用的 agymate 二进制路径（默认当前可执行文件）")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "仅预览将要修改的路径，不写入磁盘")
	cmd.Flags().BoolVar(&opts.Minimal, "minimal", false, "精简模式：仅初始化配置与后台服务，不修改 Antigravity/Magpie")
	cmd.Flags().BoolVar(&opts.NoService, "no-service", false, "跳过后台常驻服务安装")
	cmd.Flags().BoolVar(&opts.NoHooks, "no-hooks", false, "跳过 Antigravity 生命周期 Hooks 合并")
	cmd.Flags().BoolVar(&opts.NoSidecar, "no-sidecar", false, "跳过 Antigravity Sidecar 注册")
	cmd.Flags().BoolVar(&opts.NoPlugin, "no-plugin", false, "跳过 Antigravity 原生插件安装")
	cmd.Flags().BoolVar(&opts.WithMagpie, "with-magpie", false, "强制释放 Magpie 页面插件文件")
	cmd.Flags().BoolVar(&opts.NoMagpie, "no-magpie", false, "跳过 Magpie 页面插件检测与注册")
	cmd.Flags().StringVar(&opts.MagpieDevDir, "magpie-dev-dir", "", "开发模式：直接向 Magpie 注册指定本地 magpie-plugin 目录")
	cmd.Flags().BoolVar(&opts.NoWait, "no-wait", false, "跳过等待本地 Hub 就绪与运行时刷新")
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func cmdUninstall() *cobra.Command {
	var opts setup.UninstallOptions
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "一键干净卸载后台服务、Hooks（保留第三方配置）、Sidecar 与原生插件",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			rep := setup.Uninstall(ctx, opts)
			if asJSON {
				printJSON(rep)
				if !rep.OK {
					return fmt.Errorf("uninstall encountered errors")
				}
				return nil
			}
			printSetupReport(rep, "卸载")
			if !rep.OK {
				return fmt.Errorf("卸载过程中出现异常")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "仅预览将要移除的项目，不执行删除")
	cmd.Flags().BoolVar(&opts.PurgeData, "purge", false, "同时删除 ~/.config/agymate 与 ~/.local/share/agymate 数据目录")
	cmd.Flags().BoolVar(&opts.NoService, "no-service", false, "跳过后台服务卸载")
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func printSetupReport(rep setup.Report, action string) {
	mode := ""
	if rep.DryRun {
		mode = " [预览模式 --dry-run]"
	}
	fmt.Printf("AgyMate %s%s\n", action, mode)
	for i, step := range rep.Steps {
		icon := "✓"
		switch step.Status {
		case "skipped":
			icon = "-"
		case "dry_run":
			icon = "~"
		case "warn":
			icon = "!"
		case "error":
			icon = "✗"
		}
		fmt.Printf("[%d/%d] [%s] %-28s %s\n", i+1, len(rep.Steps), icon, step.Title, step.Summary)
		if rep.DryRun && len(step.Paths) > 0 {
			for _, p := range step.Paths {
				fmt.Printf("      • %s\n", p)
			}
		}
	}
}

func cmdConfig() *cobra.Command {
	root := &cobra.Command{
		Use:   "config",
		Short: "查看、修改或重置 AgyMate 本地配置 (~/.config/agymate/config.json)",
	}

	root.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "打印配置文件、状态文件与自定义定价文件路径",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("config:  %s\n", config.ConfigPath())
			fmt.Printf("state:   %s\n", config.StatePath())
			fmt.Printf("token:   %s\n", config.HubTokenPath())
			fmt.Printf("pricing: %s\n", config.PricingPath())
		},
	})

	var unmasked bool
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "打印当前生效的配置（默认脱敏密钥）",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := loadCfg()
			if !unmasked {
				cfg = cfg.MaskedCopy()
			}
			printJSON(cfg)
		},
	}
	showCmd.Flags().BoolVar(&unmasked, "unmasked", false, "显示完整明文 Token/Secret")

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "按点分键名修改配置项并原子落盘（如 pricing.currency CNY）",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg := loadCfg()
			key, val := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
			if err := applyConfigKeyValue(&cfg, key, val); err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("已更新 %s = %s（保存至 %s）\n", key, val, config.ConfigPath())
			if strings.EqualFold(key, "listenAddr") {
				fmt.Println("提示：修改 listenAddr 后请执行 `agymate service restart` 生效。")
			}
			return nil
		},
	}

	var confirmReset bool
	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "重置为默认配置（需传 --yes 确认）",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !confirmReset {
				return fmt.Errorf("此操作将覆盖 %s，请附加 --yes 确认执行", config.ConfigPath())
			}
			if err := config.Save(config.DefaultConfig()); err != nil {
				return err
			}
			fmt.Printf("已重置默认配置: %s\n", config.ConfigPath())
			return nil
		},
	}
	resetCmd.Flags().BoolVar(&confirmReset, "yes", false, "确认重置")

	root.AddCommand(showCmd, setCmd, resetCmd)
	return root
}

func applyConfigKeyValue(cfg *config.Config, key, val string) error {
	parseBool := func(s string) (bool, error) {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "1", "true", "yes", "on":
			return true, nil
		case "0", "false", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("布尔值无效: %q（期望 true/false）", s)
		}
	}

	switch strings.ToLower(key) {
	case "listenaddr":
		if val == "" {
			return fmt.Errorf("listenAddr 不能为空")
		}
		cfg.ListenAddr = val
	case "language", "lang":
		cfg.Language = config.NormalizeLanguage(val)
	case "pollintervalsec":
		n, err := strconv.Atoi(val)
		if err != nil || n < 5 {
			return fmt.Errorf("pollIntervalSec 需为 >= 5 的整数")
		}
		cfg.PollIntervalSec = n
	case "contextwarnpct":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > 100 {
			return fmt.Errorf("contextWarnPct 需为 1..100 的整数")
		}
		cfg.ContextWarnPct = n
	case "quotawarnpct":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > 100 {
			return fmt.Errorf("quotaWarnPct 需为 1..100 的整数")
		}
		cfg.QuotaWarnPct = n
	case "pricing.currency":
		cfg.Pricing.Currency = config.NormalizeCurrency(val)
	case "pricing.mode":
		cfg.Pricing.Mode = config.NormalizePricingMode(val)
	case "pricing.usdtocny":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil || f <= 0 {
			return fmt.Errorf("pricing.usdToCny 需为正数")
		}
		cfg.Pricing.USDToCNY = f
	case "pricing.ultramonthlyusd":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("pricing.ultraMonthlyUsd 需为 >= 0 的数字")
		}
		cfg.Pricing.UltraMonthlyUSD = f
	case "cdp.enabled":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		cfg.CDP.Enabled = b
	case "cdp.windowhud":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		cfg.CDP.WindowHUD = b
	case "cdp.hudposition":
		cfg.CDP.HUDPosition = config.NormalizeHUDPosition(val)
	case "cdp.zhinject":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		cfg.CDP.ZhInject = b
	case "cdp.zhscriptpath":
		cfg.CDP.ZhScriptPath = val
	case "telegram.enabled":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		cfg.Telegram.Enabled = b
	case "telegram.bottoken":
		cfg.Telegram.BotToken = val
	case "telegram.chatid":
		cfg.Telegram.ChatID = val
	case "webhook.enabled":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		cfg.Webhook.Enabled = b
	case "webhook.url":
		cfg.Webhook.URL = val
	case "webhook.format":
		cfg.Webhook.Format = config.NormalizeWebhookFormat(val)
	case "webhook.secret":
		cfg.Webhook.Secret = val
	case "colab.enabled":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		cfg.Colab.Enabled = b
	case "colab.clipath":
		cfg.Colab.CLIPath = val
	case "paths.mainapppath":
		cfg.Paths.MainAppPath = val
	case "paths.ideapppath":
		cfg.Paths.IDEAppPath = val
	case "paths.profilesdir":
		cfg.Paths.ProfilesDir = val
	default:
		return fmt.Errorf("不支持的配置键 %q（可用 `agymate config show` 查看结构）", key)
	}
	return nil
}

var emailRedactRe = regexp.MustCompile(`([A-Za-z0-9._%+-])([A-Za-z0-9._%+-]*)(@[A-Za-z0-9.-]+\.[A-Za-z]{2,})`)

func redactText(s, home string) string {
	if s == "" {
		return s
	}
	if home != "" {
		s = strings.ReplaceAll(s, home, "~")
	}
	return emailRedactRe.ReplaceAllString(s, `$1***$3`)
}

func redactDoctorReport(rep doctor.Report) doctor.Report {
	home, _ := os.UserHomeDir()
	out := rep
	out.Checks = make([]doctor.CheckResult, len(rep.Checks))
	for i, c := range rep.Checks {
		c.Summary = redactText(c.Summary, home)
		c.Detail = redactText(c.Detail, home)
		out.Checks[i] = c
	}
	return out
}

func doctorDiagnosticBundle(rep doctor.Report) map[string]any {
	return map[string]any{
		"agymateVersion": version.Full(),
		"os":             runtime.GOOS,
		"arch":           runtime.GOARCH,
		"goVersion":      runtime.Version(),
		"redacted":       true,
		"doctor":         redactDoctorReport(rep),
	}
}
