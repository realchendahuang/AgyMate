// Package config manages AgyMate's user configuration (~/.config/agymate/config.json)
// and 14-day quota history state (~/.local/share/agymate/state.json).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config defines all persistent AgyMate settings.
type Config struct {
	ListenAddr      string         `json:"listenAddr"`
	PollIntervalSec int            `json:"pollIntervalSec"`
	ContextWarnPct  int            `json:"contextWarnPct"`
	QuotaWarnPct    int            `json:"quotaWarnPct"`
	CDP             CDPConfig      `json:"cdp"`
	Telegram        TelegramConfig `json:"telegram"`
	Pricing         PricingConfig  `json:"pricing"`
	Box             BoxConfig      `json:"box"`
}

// CDPConfig controls DevToolsActivePort localization and Shadow-DOM HUD injection.
type CDPConfig struct {
	Enabled      bool     `json:"enabled"`
	ZhInject     bool     `json:"zhInject"`
	ZhScriptPath string   `json:"zhScriptPath"`
	WindowHUD    bool     `json:"windowHud"`
	HUDPosition  string   `json:"hudPosition"`
	HUDInstances []string `json:"hudInstances"`
}

// TelegramConfig defines dedicated Telegram Bot notifications.
type TelegramConfig struct {
	Enabled              bool   `json:"enabled"`
	BotToken             string `json:"botToken"`
	ChatID               string `json:"chatId"`
	NotifyOnStop         bool   `json:"notifyOnStop"`
	NotifyOnError        bool   `json:"notifyOnError"`
	NotifyMinDurationSec int    `json:"notifyMinDurationSec"`
	NotifyOnStuckSec     int    `json:"notifyOnStuckSec"`
	NotifyLowQuota       bool   `json:"notifyLowQuota"`
}

// PricingConfig controls API value estimation and display currency.
type PricingConfig struct {
	Mode             string  `json:"mode"` // promo, std, nc_promo, nc_std
	USDToCNY         float64 `json:"usdToCny"`
	Currency         string  `json:"currency"` // USD, CNY
	UltraMonthlyUSD  float64 `json:"ultraMonthlyUsd"`
}

// BoxConfig defines paths to Antigravity main app, Box clone, and ParaGravity profiles.
type BoxConfig struct {
	MainAppPath      string `json:"mainAppPath"`
	IDEAppPath       string `json:"ideAppPath"`
	BoxAppPath       string `json:"boxAppPath"`
	UpdateScriptPath string `json:"updateScriptPath"`
	ProfilesDir      string `json:"profilesDir"`
}

// DefaultConfig returns safe defaults tailored for the user's 5-instance setup.
func DefaultConfig() Config {
	return Config{
		ListenAddr:      "127.0.0.1:3778",
		PollIntervalSec: 15,
		ContextWarnPct:  80,
		QuotaWarnPct:    20,
		CDP: CDPConfig{
			Enabled:      true,
			ZhInject:     true,
			ZhScriptPath: "~/.antigravity-zh/inject.js",
			WindowHUD:    true,
			HUDPosition:  "bottom-right",
			HUDInstances: []string{"main", "p1", "p2", "p3", "p4"},
		},
		Telegram: TelegramConfig{
			Enabled:              false,
			BotToken:             "",
			ChatID:               "",
			NotifyOnStop:         true,
			NotifyOnError:        true,
			NotifyMinDurationSec: 60,
			NotifyOnStuckSec:     45,
			NotifyLowQuota:       true,
		},
		Pricing: PricingConfig{
			Mode:            "std",
			USDToCNY:        7.20,
			Currency:        "USD",
			UltraMonthlyUSD: 100.0,
		},
		Box: BoxConfig{
			MainAppPath:      "/Applications/Antigravity.app",
			IDEAppPath:       "/Applications/Antigravity IDE.app",
			BoxAppPath:       "/Applications/Antigravity-Box.app",
			UpdateScriptPath: "~/.antigravity-boxes/update-box.sh",
			ProfilesDir:      "~/.antigravity-profiles",
		},
	}
}

// ExpandPath replaces a leading "~/" with the user's real home directory.
func ExpandPath(p string) string {
	if p == "" {
		return ""
	}
	if p == "~" {
		h, _ := os.UserHomeDir()
		return h
	}
	if strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

// ConfigPath returns ~/.config/agymate/config.json (or AGYMATE_CONFIG override).
func ConfigPath() string {
	if env := os.Getenv("AGYMATE_CONFIG"); env != "" {
		return env
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "agymate", "config.json")
}

// StatePath returns ~/.local/share/agymate/state.json (or AGYMATE_STATE override).
func StatePath() string {
	if env := os.Getenv("AGYMATE_STATE"); env != "" {
		return env
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "agymate", "state.json")
}

// Load reads config from disk, filling missing fields from DefaultConfig.
func Load() (Config, error) {
	cfg := DefaultConfig()
	p := ConfigPath()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	def := DefaultConfig()
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = def.ListenAddr
	}
	if cfg.PollIntervalSec < 3 {
		cfg.PollIntervalSec = def.PollIntervalSec
	}
	if cfg.ContextWarnPct <= 0 || cfg.ContextWarnPct > 100 {
		cfg.ContextWarnPct = def.ContextWarnPct
	}
	if cfg.QuotaWarnPct <= 0 || cfg.QuotaWarnPct > 100 {
		cfg.QuotaWarnPct = def.QuotaWarnPct
	}
	if cfg.CDP.ZhScriptPath == "" {
		cfg.CDP.ZhScriptPath = def.CDP.ZhScriptPath
	}
	if cfg.CDP.HUDPosition == "" {
		cfg.CDP.HUDPosition = def.CDP.HUDPosition
	}
	if cfg.Pricing.Mode == "" {
		cfg.Pricing.Mode = def.Pricing.Mode
	}
	if cfg.Pricing.USDToCNY <= 0 {
		cfg.Pricing.USDToCNY = def.Pricing.USDToCNY
	}
	if cfg.Pricing.Currency == "" {
		cfg.Pricing.Currency = def.Pricing.Currency
	}
	if cfg.Pricing.UltraMonthlyUSD <= 0 {
		cfg.Pricing.UltraMonthlyUSD = def.Pricing.UltraMonthlyUSD
	}
	if cfg.Box.MainAppPath == "" {
		cfg.Box.MainAppPath = def.Box.MainAppPath
	}
	if cfg.Box.IDEAppPath == "" {
		cfg.Box.IDEAppPath = def.Box.IDEAppPath
	}
	if cfg.Box.BoxAppPath == "" {
		cfg.Box.BoxAppPath = def.Box.BoxAppPath
	}
	if cfg.Box.UpdateScriptPath == "" {
		cfg.Box.UpdateScriptPath = def.Box.UpdateScriptPath
	}
	if cfg.Box.ProfilesDir == "" {
		cfg.Box.ProfilesDir = def.Box.ProfilesDir
	}
}

// Save atomically writes cfg to disk with mode 0600 (never leaving backup files).
func Save(cfg Config) error {
	applyDefaults(&cfg)
	p := ConfigPath()
	return writeJSONAtomic(p, cfg, 0o600)
}

func writeJSONAtomic(path string, v any, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// QuotaPoint stores one historical quota reading for runway estimation.
type QuotaPoint struct {
	TS        int64   `json:"ts"`        // Unix seconds
	Remaining float64 `json:"remaining"` // 0.0 - 1.0
	ResetTS   int64   `json:"resetTs"`   // Unix seconds of resetTime
}

// PersistentState stores 14-day quota samples keyed by "<instanceId>:<bucketId>"
// and alert dedup keys.
type PersistentState struct {
	mu           sync.Mutex
	path         string
	QuotaHistory map[string][]QuotaPoint `json:"quotaHistory"`
	LowQuotaSent map[string]int64        `json:"lowQuotaSent"`
}

// LoadState opens or initializes the persistent state file.
func LoadState(path string) *PersistentState {
	if path == "" {
		path = StatePath()
	}
	s := &PersistentState{
		path:         path,
		QuotaHistory: make(map[string][]QuotaPoint),
		LowQuotaSent: make(map[string]int64),
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, s)
		if s.QuotaHistory == nil {
			s.QuotaHistory = make(map[string][]QuotaPoint)
		}
		if s.LowQuotaSent == nil {
			s.LowQuotaSent = make(map[string]int64)
		}
	}
	return s
}

// RecordQuota appends a quota observation with 14-day retention and 5-minute downsampling
// for samples older than 24 hours.
func (s *PersistentState) RecordQuota(key string, remaining float64, resetTS int64, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowUnix := now.Unix()
	pts := s.QuotaHistory[key]
	if n := len(pts); n > 0 {
		last := pts[n-1]
		// Avoid excessive churn if nothing changed within 60s
		if nowUnix-last.TS < 60 && last.Remaining == remaining && last.ResetTS == resetTS {
			return
		}
	}
	pts = append(pts, QuotaPoint{
		TS:        nowUnix,
		Remaining: remaining,
		ResetTS:   resetTS,
	})

	cutoff14d := nowUnix - 14*86400
	cutoff24h := nowUnix - 86400
	pruned := make([]QuotaPoint, 0, len(pts))
	var lastBucket int64 = -1
	var prevReset int64
	for _, pt := range pts {
		if pt.TS < cutoff14d {
			continue
		}
		isReset := prevReset > 0 && pt.ResetTS > prevReset
		prevReset = pt.ResetTS
		if pt.TS >= cutoff24h || isReset {
			pruned = append(pruned, pt)
			continue
		}
		bucket := pt.TS / 300
		if bucket != lastBucket {
			lastBucket = bucket
			pruned = append(pruned, pt)
		}
	}
	s.QuotaHistory[key] = pruned
}

// RunwayEstimate describes burn rate and estimated hours until empty.
type RunwayEstimate struct {
	BurnPctPerHour  float64 `json:"burnPctPerHour"`
	HoursUntilEmpty float64 `json:"hoursUntilEmpty"` // -1 if idle/not burning
	ExhaustsBeforeReset bool `json:"exhaustsBeforeReset"`
}

// EstimateRunway computes the burn rate since the last reset (up to the past 2 hours)
// and predicts when the bucket will reach 0%.
func (s *PersistentState) EstimateRunway(key string, currentRemaining float64, resetTS int64, now time.Time) RunwayEstimate {
	s.mu.Lock()
	defer s.mu.Unlock()

	pts := s.QuotaHistory[key]
	if len(pts) < 2 || currentRemaining <= 0 {
		return RunwayEstimate{HoursUntilEmpty: -1}
	}

	nowUnix := now.Unix()
	windowStart := nowUnix - 2*3600
	var earliest *QuotaPoint
	for i := range pts {
		pt := &pts[i]
		if pt.TS < windowStart {
			continue
		}
		// Stay within the same reset cycle if resetTS is known
		if resetTS > 0 && pt.ResetTS > 0 && abs64(pt.ResetTS-resetTS) > 120 {
			continue
		}
		// Ignore points after a refill jump
		if pt.Remaining+0.005 < currentRemaining {
			earliest = nil
			continue
		}
		if earliest == nil {
			earliest = pt
		}
	}
	if earliest == nil || nowUnix-earliest.TS < 120 {
		return RunwayEstimate{HoursUntilEmpty: -1}
	}

	drop := earliest.Remaining - currentRemaining
	if drop <= 0.0005 {
		return RunwayEstimate{HoursUntilEmpty: -1}
	}
	hours := float64(nowUnix-earliest.TS) / 3600.0
	burnPctPerHour := (drop * 100.0) / hours
	if burnPctPerHour <= 0.01 {
		return RunwayEstimate{HoursUntilEmpty: -1}
	}
	hoursUntilEmpty := (currentRemaining * 100.0) / burnPctPerHour
	exhausts := resetTS > nowUnix && hoursUntilEmpty < float64(resetTS-nowUnix)/3600.0
	return RunwayEstimate{
		BurnPctPerHour:      burnPctPerHour,
		HoursUntilEmpty:     hoursUntilEmpty,
		ExhaustsBeforeReset: exhausts,
	}
}

// ShouldNotifyLowQuota marks low-quota alerts once per reset window.
func (s *PersistentState) ShouldNotifyLowQuota(key string, resetTS int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, ok := s.LowQuotaSent[key]
	if ok && (prev == resetTS || (resetTS == 0 && now.Unix()-prev < 3600)) {
		return false
	}
	if resetTS > 0 {
		s.LowQuotaSent[key] = resetTS
	} else {
		s.LowQuotaSent[key] = now.Unix()
	}
	return true
}

// Save persists state atomically.
func (s *PersistentState) Save() error {
	s.mu.Lock()
	snap := struct {
		QuotaHistory map[string][]QuotaPoint `json:"quotaHistory"`
		LowQuotaSent map[string]int64        `json:"lowQuotaSent"`
	}{
		QuotaHistory: s.QuotaHistory,
		LowQuotaSent: s.LowQuotaSent,
	}
	path := s.path
	s.mu.Unlock()
	return writeJSONAtomic(path, snap, 0o600)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
