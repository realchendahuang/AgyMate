// Package hub: health, doctor, configuration, and notification test HTTP handlers.
package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/version"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	cfg := s.Config()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"version":     version.Version,
		"fullVersion": version.Full(),
		"commit":      version.EffectiveCommit(),
		"buildTime":   version.BuildTime,
		"listenAddr":  cfg.ListenAddr,
		"startedAt":   s.startedAt,
	})
}

func (s *Server) handleDoctor(w http.ResponseWriter, _ *http.Request) {
	cfg := s.Config()
	instList := instances.DiscoverAll(cfg)
	rep := doctor.RunChecks(cfg, instList, s.cdpWatcher.LastStatus())
	s.mu.Lock()
	s.doctorRep = rep
	s.lastDocCheck = time.Now().Unix()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleDoctorFix(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	fixRep := doctor.FixSafe(cfg, "")
	s.PollOnce(r.Context())
	s.mu.RLock()
	docRep := s.doctorRep
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"fix":    fixRep,
		"doctor": docRep,
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	wantMask := r.URL.Query().Get("masked") == "1" || !s.hasValidAuthToken(r)
	if wantMask {
		cfg = cfg.MaskedCopy()
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var upd config.ConfigUpdate
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&upd); err != nil {
		writeJSONError(w, http.StatusBadRequest, "配置文件 JSON 解析失败: "+err.Error())
		return
	}

	s.cfgMu.Lock()
	cur := s.cfg
	next := cur
	next.ApplyUpdate(upd)

	if next.ListenAddr == "" {
		next.ListenAddr = cur.ListenAddr
	}
	next.Language = config.NormalizeLanguage(next.Language)
	if next.PollIntervalSec < 5 {
		next.PollIntervalSec = 5
	}
	if next.ContextWarnPct <= 0 || next.ContextWarnPct > 100 {
		next.ContextWarnPct = 80
	}
	if next.QuotaWarnPct <= 0 || next.QuotaWarnPct > 100 {
		next.QuotaWarnPct = 20
	}
	next.CDP.ZhScriptPath = strings.TrimSpace(next.CDP.ZhScriptPath)
	next.CDP.HUDPosition = config.NormalizeHUDPosition(next.CDP.HUDPosition)
	next.Pricing.Mode = config.NormalizePricingMode(next.Pricing.Mode)
	next.Pricing.Currency = config.NormalizeCurrency(next.Pricing.Currency)
	next.Webhook.Format = config.NormalizeWebhookFormat(next.Webhook.Format)
	if next.Pricing.USDToCNY <= 0 {
		next.Pricing.USDToCNY = 7.20
	}
	if next.Pricing.UltraMonthlyUSD < 0 {
		next.Pricing.UltraMonthlyUSD = 0
	}
	if next.Paths.MainAppPath == "" {
		next.Paths.MainAppPath = cur.Paths.MainAppPath
	}
	if next.Paths.IDEAppPath == "" {
		next.Paths.IDEAppPath = cur.Paths.IDEAppPath
	}
	if next.Paths.ProfilesDir == "" {
		next.Paths.ProfilesDir = cur.Paths.ProfilesDir
	}
	if !next.Colab.Enabled && next.Colab.CLIPath == "" && next.Colab.PollIntervalSec == 0 {
		next.Colab = cur.Colab
	} else {
		if next.Colab.CLIPath == "" {
			next.Colab.CLIPath = cur.Colab.CLIPath
		}
		if next.Colab.CLIPath == "" {
			next.Colab.CLIPath = "~/.local/bin/colab-safe"
		}
		if next.Colab.PollIntervalSec < 15 {
			next.Colab.PollIntervalSec = 60
		}
		if next.Colab.MonthlyAllowance <= 0 {
			next.Colab.MonthlyAllowance = 2000
		}
		if next.Colab.WarnActiveMinutes <= 0 {
			next.Colab.WarnActiveMinutes = 30
		}
		if next.Colab.LowBalanceCCU < 0 {
			next.Colab.LowBalanceCCU = 0
		}
	}
	if strings.Contains(next.Telegram.BotToken, "...") {
		next.Telegram.BotToken = cur.Telegram.BotToken
	}
	if strings.Contains(next.Webhook.Secret, "...") {
		next.Webhook.Secret = cur.Webhook.Secret
	}
	if err := config.Save(next); err != nil {
		s.cfgMu.Unlock()
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.cfg = next
	s.cfgMu.Unlock()
	s.broadcast("overview")

	cdpWasEnabled := cur.CDP.Enabled
	go func() {
		bgCtx := context.Background()
		if next.CDP.Enabled {
			// Push immediate forced CDP sync first so position/currency/HUD toggle reflects without waiting on RPC/Colab poll.
			s.SyncCDPOnce(bgCtx, true)
		} else if cdpWasEnabled {
			// User disabled CDP entirely: tear down any existing window HUDs cleanly.
			instList := s.cachedInstancesOrDiscover(next)
			teardownCfg := next
			teardownCfg.CDP.Enabled = true
			teardownCfg.CDP.ZhInject = false
			teardownCfg.CDP.WindowHUD = false
			s.cdpWatcher.SyncAll(bgCtx, instList, teardownCfg, nil, true)
			s.broadcast("cdp")
		}
		s.PollOnceWithForce(bgCtx, true)
		if next.CDP.Enabled {
			s.SyncCDPOnce(bgCtx, true)
		}
	}()
	writeJSON(w, http.StatusOK, next.MaskedCopy())
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel       string `json:"channel"` // "" (auto) | "telegram" | "webhook"
		BotToken      string `json:"botToken"`
		ChatID        string `json:"chatId"`
		WebhookURL    string `json:"webhookUrl"`
		WebhookFormat string `json:"webhookFormat"`
		WebhookSecret string `json:"webhookSecret"`
	}
	_ = decodeOptionalJSON(r, &req)
	cfg := s.Config()

	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	if channel == "" && strings.TrimSpace(req.WebhookURL) != "" && strings.TrimSpace(req.BotToken) == "" && strings.TrimSpace(req.ChatID) == "" {
		channel = "webhook"
	}

	if channel == "webhook" {
		whURL := firstNonEmpty(req.WebhookURL, cfg.Webhook.URL)
		whFmt := firstNonEmpty(req.WebhookFormat, cfg.Webhook.Format)
		whSec := req.WebhookSecret
		if whSec == "" || strings.Contains(whSec, "...") {
			whSec = cfg.Webhook.Secret
		}
		msg := fmt.Sprintf("🛸 <b>AgyMate · Webhook 通道验证成功</b>\n实例监听: <code>%s</code>\n时间: <code>%s</code>",
			cfg.ListenAddr, time.Now().Format("2006-01-02 15:04:05"))
		if err := s.notifier.SendWebhook(r.Context(), whURL, whFmt, whSec, "AgyMate Webhook Test", msg); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": "webhook"})
		return
	}

	tok := req.BotToken
	if tok == "" || strings.Contains(tok, "...") {
		tok = cfg.Telegram.BotToken
	}
	chatID := req.ChatID
	if chatID == "" {
		chatID = cfg.Telegram.ChatID
	}
	msg := fmt.Sprintf("🛸 <b>AgyMate · Telegram 通道验证成功</b>\n实例监听: <code>%s</code>\n时间: <code>%s</code>",
		cfg.ListenAddr, time.Now().Format("2006-01-02 15:04:05"))
	if err := s.notifier.SendHTML(r.Context(), tok, chatID, msg); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": "telegram"})
}
