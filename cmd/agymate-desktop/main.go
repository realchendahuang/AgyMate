// Command agymate-desktop provides the native macOS Wails v3 desktop window and Menu Bar companion.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/realchendahuang/AgyMate/frontend"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/hub"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

//go:embed icon.png
var appIconPNG []byte

//go:embed tray.png
var trayIconPNG []byte

func main() {
	runPreflight()

	cfg, _ := config.Load()
	baseURL := "http://" + cfg.ListenAddr
	hc := &http.Client{Timeout: 2500 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Reuse LaunchAgent Hub if already alive on cfg.ListenAddr; otherwise run an embedded Hub.
	var localHTTP *http.Server
	if !hubHealthy(hc, baseURL) {
		srv := hub.NewServer(cfg, frontend.FS())
		srv.Start(ctx)
		localHTTP = &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if err := localHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "embedded hub stopped: %v\n", err)
			}
		}()
		waitForHub(hc, baseURL, 2*time.Second)
	}

	app := application.New(application.Options{
		Name:        "AgyMate",
		Description: "Antigravity 五开伴侣",
		Icon:        appIconPNG,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	startHidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--hidden" {
			startHidden = true
		}
	}

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "AgyMate — Antigravity 五开伴侣",
		Width:            1240,
		Height:           820,
		MinWidth:         940,
		MinHeight:        620,
		URL:              baseURL,
		Hidden:           startHidden,
		BackgroundColour: application.NewRGB(12, 16, 21),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarDefault,
		},
	})

	var quitting atomic.Bool
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if quitting.Load() {
			return
		}
		window.Hide()
		event.Cancel()
	})

	tray := app.SystemTray.New()
	tray.SetTemplateIcon(trayIconPNG)
	tray.SetTooltip("AgyMate — Antigravity 五开伴侣")
	tray.SetLabel("U:--%")

	menu := application.NewMenu()
	menu.Add("打开 AgyMate 控制台").OnClick(func(_ *application.Context) {
		window.Show()
		window.Focus()
	})
	menu.Add("立即刷新配额与注入 HUD").OnClick(func(_ *application.Context) {
		go func() {
			_, _ = hc.Post(baseURL+"/api/cdp/inject-now", "application/json", strings.NewReader("{}"))
			if resp, err := hc.Get(baseURL + "/api/overview?refresh=1"); err == nil {
				resp.Body.Close()
			}
			lbl, tip := fetchMenuBarSummary(hc, baseURL)
			tray.SetLabel(lbl)
			tray.SetTooltip(tip)
		}()
	})
	menu.Add("在浏览器中打开 (" + cfg.ListenAddr + ")").OnClick(func(_ *application.Context) {
		_ = app.Browser.OpenURL(baseURL)
	})
	menu.AddSeparator()

	for _, inst := range instances.DiscoverAll(cfg) {
		id := inst.ID
		label := fmt.Sprintf("切换 / 聚焦 %s (%s)", inst.ID, inst.Label)
		isRO := inst.ReadOnlyLifecycle
		menu.Add(label).OnClick(func(_ *application.Context) {
			go func() {
				resp, err := hc.Post(baseURL+"/api/instances/"+id+"/focus", "application/json", strings.NewReader("{}"))
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK || isRO {
						return
					}
				}
				if !isRO {
					if r2, e2 := hc.Post(baseURL+"/api/instances/"+id+"/launch", "application/json", strings.NewReader("{}")); e2 == nil {
						r2.Body.Close()
					}
				}
			}()
		})
	}

	menu.AddSeparator()
	menu.Add("退出 AgyMate").OnClick(func(_ *application.Context) {
		quitting.Store(true)
		cancel()
		if localHTTP != nil {
			sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = localHTTP.Shutdown(sctx)
			scancel()
		}
		app.Quit()
	})
	tray.SetMenu(menu)

	go func() {
		update := func() {
			lbl, tip := fetchMenuBarSummary(hc, baseURL)
			if lbl != "" {
				tray.SetLabel(lbl)
				tray.SetTooltip(tip)
			}
		}
		time.Sleep(700 * time.Millisecond)
		update()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				update()
			}
		}
	}()

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "agymate-desktop error: %v\n", err)
		os.Exit(1)
	}
}

func hubHealthy(hc *http.Client, baseURL string) bool {
	c := *hc
	c.Timeout = 500 * time.Millisecond
	resp, err := c.Get(baseURL + "/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func waitForHub(hc *http.Client, baseURL string, maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if hubHealthy(hc, baseURL) {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func fetchMenuBarSummary(hc *http.Client, baseURL string) (string, string) {
	resp, err := hc.Get(baseURL + "/api/overview")
	if err != nil {
		return "U:--%", "AgyMate — 未连接到 Hub"
	}
	defer resp.Body.Close()
	var ov hub.OverviewPayload
	if json.NewDecoder(resp.Body).Decode(&ov) != nil {
		return "U:--%", "AgyMate"
	}
	qStr := "--%"
	var running []string
	for _, iv := range ov.Instances {
		if iv.ID == instances.KindMain && iv.Snapshot != nil && len(iv.Snapshot.Buckets) > 0 {
			qStr = fmt.Sprintf("%.0f%%", iv.Snapshot.Buckets[0].RemainingPct)
		}
		if iv.Running {
			running = append(running, iv.ID)
		}
	}
	cost := fmt.Sprintf("$%.2f", ov.TodaySummary.TotalUSD)
	if strings.EqualFold(ov.Pricing.Currency, "CNY") {
		rate := ov.Pricing.USDToCNY
		if rate <= 0 {
			rate = 7.20
		}
		cost = fmt.Sprintf("¥%.2f", ov.TodaySummary.TotalUSD*rate)
	}
	tip := "AgyMate — Antigravity 五开伴侣"
	if len(running) > 0 {
		tip += " | 在线: " + strings.Join(running, ", ")
	}
	return fmt.Sprintf("U:%s · %s", qStr, cost), tip
}
