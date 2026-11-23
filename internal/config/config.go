// Package config manages AgyMate's user configuration (~/.config/agymate/config.json)
// and 14-day quota history state (~/.local/share/agymate/state.json).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ConfigUpdate defines partial AgyMate settings updates.
type ConfigUpdate struct {
	ListenAddr      *string         `json:"listenAddr"`
	Language        *string         `json:"language"`
	PollIntervalSec *int            `json:"pollIntervalSec"`
	ContextWarnPct  *int            `json:"contextWarnPct"`
	QuotaWarnPct    *int            `json:"quotaWarnPct"`
	CDP             *CDPConfig      `json:"cdp"`
	Telegram        *TelegramConfig `json:"telegram"`
	Webhook         *WebhookConfig  `json:"webhook"`
	Pricing         *PricingConfig  `json:"pricing"`
	Paths           *PathsConfig    `json:"paths"`
	Colab           *ColabConfig    `json:"colab"`
}

// ApplyUpdate applies non-nil fields from update to the config.
func (c *Config) ApplyUpdate(upd ConfigUpdate) {
	if upd.ListenAddr != nil {
		c.ListenAddr = *upd.ListenAddr
	}
	if upd.Language != nil {
		c.Language = *upd.Language
	}
	if upd.PollIntervalSec != nil {
		c.PollIntervalSec = *upd.PollIntervalSec
	}
	if upd.ContextWarnPct != nil {
		c.ContextWarnPct = *upd.ContextWarnPct
	}
	if upd.QuotaWarnPct != nil {
		c.QuotaWarnPct = *upd.QuotaWarnPct
	}
	if upd.CDP != nil {
		c.CDP = *upd.CDP
	}
	if upd.Telegram != nil {
		c.Telegram = *upd.Telegram
	}
	if upd.Webhook != nil {
		c.Webhook = *upd.Webhook
	}
	if upd.Pricing != nil {
		c.Pricing = *upd.Pricing
	}
	if upd.Paths != nil {
		c.Paths = *upd.Paths
	}
	if upd.Colab != nil {
		c.Colab = *upd.Colab
	}
}

// Config defines all persistent AgyMate settings.
type Config struct {
	ListenAddr      string         `json:"listenAddr"`
	Language        string         `json:"language"` // auto | zh | en
	PollIntervalSec int            `json:"pollIntervalSec"`
	ContextWarnPct  int            `json:"contextWarnPct"`
	QuotaWarnPct    int            `json:"quotaWarnPct"`
	CDP             CDPConfig      `json:"cdp"`
	Telegram        TelegramConfig `json:"telegram"`
	Webhook         WebhookConfig  `json:"webhook"`
	Pricing         PricingConfig  `json:"pricing"`
	Paths           PathsConfig    `json:"paths"`
	Colab           ColabConfig    `json:"colab"`
}

// WebhookConfig defines optional generic/platform webhook notifications
// (auto-detects Slack, Discord, Feishu/Lark, DingTalk, WeCom, Bark, or generic JSON).
type WebhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Format  string `json:"format"` // auto | generic | slack | discord | feishu | dingtalk | wecom | bark
	Secret  string `json:"secret,omitempty"`
}

// ColabConfig controls Google Colab CCU and cloud VM monitoring via colab-safe.
type ColabConfig struct {
	Enabled           bool    `json:"enabled"`
	CLIPath           string  `json:"cliPath"`
	PollIntervalSec   int     `json:"pollIntervalSec"`
	MonthlyAllowance  float64 `json:"monthlyAllowance"`
	WarnActiveMinutes int     `json:"warnActiveMinutes"`
	LowBalanceCCU     float64 `json:"lowBalanceCcu"`
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
	Mode            string  `json:"mode"` // promo, std, nc_promo, nc_std
	USDToCNY        float64 `json:"usdToCny"`
	Currency        string  `json:"currency"` // USD, CNY
	UltraMonthlyUSD float64 `json:"ultraMonthlyUsd"`
}

// PathsConfig defines paths to official Antigravity clients and the isolated profiles directory.
type PathsConfig struct {
	MainAppPath string `json:"mainAppPath"`
	IDEAppPath  string `json:"ideAppPath"`
	ProfilesDir string `json:"profilesDir"`
}

// DefaultConfig returns portable, deterministic defaults that work for single-instance
// and multi-profile setups alike without requiring optional third-party tools.
func DefaultConfig() Config {
	return Config{
		ListenAddr:      "127.0.0.1:3778",
		Language:        "auto",
		PollIntervalSec: 15,
		ContextWarnPct:  80,
		QuotaWarnPct:    20,
		CDP: CDPConfig{
			Enabled:      true,
			ZhInject:     false,
			ZhScriptPath: "~/.antigravity-zh/inject.js",
			WindowHUD:    true,
			HUDPosition:  "top-right",
			HUDInstances: []string{},
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
		Webhook: WebhookConfig{
			Enabled: false,
			URL:     "",
			Format:  "auto",
			Secret:  "",
		},
		Pricing: PricingConfig{
			Mode:            "std",
			USDToCNY:        7.20,
			Currency:        "USD",
			UltraMonthlyUSD: 0,
		},
		Paths: defaultPathsConfig(runtime.GOOS),
		Colab: ColabConfig{
			Enabled:           false,
			CLIPath:           "~/.local/bin/colab-safe",
			PollIntervalSec:   60,
			MonthlyAllowance:  2000.0,
			WarnActiveMinutes: 30,
			LowBalanceCCU:     200.0,
		},
	}
}

func defaultPathsConfig(goos string) PathsConfig {
	switch goos {
	case "linux":
		return PathsConfig{
			MainAppPath: "/usr/bin/antigravity",
			ProfilesDir: "~/.antigravity-profiles",
		}
	case "windows":
		return PathsConfig{
			MainAppPath: "~/AppData/Local/Programs/Antigravity/Antigravity.exe",
			ProfilesDir: "~/.antigravity-profiles",
		}
	default:
		return PathsConfig{
			MainAppPath: "/Applications/Antigravity.app",
			IDEAppPath:  "/Applications/Antigravity IDE.app",
			ProfilesDir: "~/.antigravity-profiles",
		}
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

// ColabAccountsPath returns ~/.config/agymate/colab_accounts.json (or AGYMATE_COLAB_ACCOUNTS override).
func ColabAccountsPath() string {
	if env := strings.TrimSpace(os.Getenv("AGYMATE_COLAB_ACCOUNTS")); env != "" {
		return env
	}
	return filepath.Join(filepath.Dir(ConfigPath()), "colab_accounts.json")
}

// PricingPath returns ~/.config/agymate/pricing.json (or AGYMATE_PRICING override).
func PricingPath() string {
	if env := strings.TrimSpace(os.Getenv("AGYMATE_PRICING")); env != "" {
		return env
	}
	return filepath.Join(filepath.Dir(ConfigPath()), "pricing.json")
}

// MaskSecret redacts a token or secret while keeping short head/tail hints for UI recognition.
func MaskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 8 {
		return s[:4] + "..." + s[len(s)-4:]
	}
	return "..."
}

// MaskedCopy returns a shallow copy of Config with sensitive credentials redacted.
func (c Config) MaskedCopy() Config {
	cp := c
	cp.Telegram.BotToken = MaskSecret(cp.Telegram.BotToken)
	cp.Webhook.Secret = MaskSecret(cp.Webhook.Secret)
	return cp
}

// HasAnyNotificationChannel reports whether Telegram or Webhook delivery is configured and enabled.
func (c Config) HasAnyNotificationChannel() bool {
	tgOK := c.Telegram.Enabled && strings.TrimSpace(c.Telegram.BotToken) != "" && strings.TrimSpace(c.Telegram.ChatID) != ""
	whOK := c.Webhook.Enabled && strings.TrimSpace(c.Webhook.URL) != ""
	return tgOK || whOK
}

// HubTokenPath returns ~/.config/agymate/hub.token (or AGYMATE_HUB_TOKEN override).
func HubTokenPath() string {
	if env := strings.TrimSpace(os.Getenv("AGYMATE_HUB_TOKEN")); env != "" {
		return env
	}
	return filepath.Join(filepath.Dir(ConfigPath()), "hub.token")
}

// LoadHubToken reads the local Hub auth token from disk, returning "" if absent.
func LoadHubToken() string {
	raw, err := os.ReadFile(HubTokenPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// LoadOrCreateHubToken returns the persisted local Hub auth token, creating one atomically with mode 0600 if absent.
func LoadOrCreateHubToken() (string, error) {
	p := HubTokenPath()
	if raw, err := os.ReadFile(p); err == nil {
		if tok := strings.TrimSpace(string(raw)); tok != "" {
			return tok, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)

	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, werr := f.WriteString(tok + "\n")
		cerr := f.Close()
		if werr != nil {
			_ = os.Remove(p)
			return "", werr
		}
		if cerr != nil {
			return "", cerr
		}
		return tok, nil
	}
	if os.IsExist(err) {
		if raw, rerr := os.ReadFile(p); rerr == nil {
			if existing := strings.TrimSpace(string(raw)); existing != "" {
				return existing, nil
			}
		}
		// Existing file was empty; replace atomically.
		if werr := writeBytesAtomic(p, []byte(tok+"\n"), 0o600); werr != nil {
			return "", werr
		}
		return tok, nil
	}
	return "", err
}

// Load reads config from disk, filling missing fields from DefaultConfig.
// When no config file exists yet on disk, optional local integrations
// (such as zh-inject or colab-safe) are auto-enabled only if already present.
func Load() (Config, error) {
	cfg := DefaultConfig()
	p := ConfigPath()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvironmentDefaults(&cfg)
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	var legacy struct {
		Box   PathsConfig `json:"box"`
		Paths PathsConfig `json:"paths"`
	}
	if json.Unmarshal(data, &legacy) == nil {
		if legacy.Paths.MainAppPath == "" && legacy.Box.MainAppPath != "" {
			cfg.Paths.MainAppPath = legacy.Box.MainAppPath
		}
		if legacy.Paths.IDEAppPath == "" && legacy.Box.IDEAppPath != "" {
			cfg.Paths.IDEAppPath = legacy.Box.IDEAppPath
		}
		if legacy.Paths.ProfilesDir == "" && legacy.Box.ProfilesDir != "" {
			cfg.Paths.ProfilesDir = legacy.Box.ProfilesDir
		}
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func applyEnvironmentDefaults(cfg *Config) {
	if zh := ExpandPath(cfg.CDP.ZhScriptPath); zh != "" {
		if st, err := os.Stat(zh); err == nil && !st.IsDir() {
			cfg.CDP.ZhInject = true
		}
	}
	if hasExecutable(ExpandPath(cfg.Colab.CLIPath)) {
		cfg.Colab.Enabled = true
	} else if _, err := exec.LookPath("colab-safe"); err == nil {
		cfg.Colab.Enabled = true
	} else if ca := ColabAccountsPath(); ca != "" {
		if st, err := os.Stat(ca); err == nil && !st.IsDir() && st.Size() > 0 {
			cfg.Colab.Enabled = true
		}
	}
}

func hasExecutable(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return st.Mode().Perm()&0o111 != 0
}

// NormalizeHUDPosition validates and normalizes a window HUD corner position,
// falling back to "top-right" whenever empty or unrecognized.
func NormalizeHUDPosition(pos string) string {
	switch strings.ToLower(strings.TrimSpace(pos)) {
	case "top-right", "top-left", "bottom-right", "bottom-left":
		return strings.ToLower(strings.TrimSpace(pos))
	default:
		return "top-right"
	}
}

// NormalizePricingMode validates pricing valuation mode, falling back to "std".
func NormalizePricingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "std", "promo", "nc_std", "nc_promo":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "std"
	}
}

// NormalizeCurrency validates display currency ("USD" or "CNY"), falling back to "USD".
func NormalizeCurrency(cur string) string {
	if strings.EqualFold(strings.TrimSpace(cur), "CNY") {
		return "CNY"
	}
	return "USD"
}

// NormalizeLanguage validates UI language ("auto", "zh", "en"), falling back to "auto".
func NormalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "zh-cn", "zh_cn", "zh-tw":
		return "zh"
	case "en", "en-us", "en_us", "en-gb":
		return "en"
	default:
		return "auto"
	}
}

// NormalizeWebhookFormat validates webhook payload format, falling back to "auto".
func NormalizeWebhookFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "auto", "generic", "slack", "discord", "feishu", "dingtalk", "wecom", "bark":
		return strings.ToLower(strings.TrimSpace(format))
	default:
		return "auto"
	}
}

func applyDefaults(cfg *Config) {
	def := DefaultConfig()
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = def.ListenAddr
	}
	cfg.Language = NormalizeLanguage(cfg.Language)
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
	cfg.CDP.HUDPosition = NormalizeHUDPosition(cfg.CDP.HUDPosition)
	cfg.Webhook.Format = NormalizeWebhookFormat(cfg.Webhook.Format)
	cfg.Pricing.Mode = NormalizePricingMode(cfg.Pricing.Mode)
	if cfg.Pricing.USDToCNY <= 0 {
		cfg.Pricing.USDToCNY = def.Pricing.USDToCNY
	}
	cfg.Pricing.Currency = NormalizeCurrency(cfg.Pricing.Currency)
	if cfg.Pricing.UltraMonthlyUSD < 0 {
		cfg.Pricing.UltraMonthlyUSD = 0
	}
	if cfg.Paths.MainAppPath == "" {
		cfg.Paths.MainAppPath = def.Paths.MainAppPath
	}
	if cfg.Paths.IDEAppPath == "" {
		cfg.Paths.IDEAppPath = def.Paths.IDEAppPath
	}
	if cfg.Paths.ProfilesDir == "" {
		cfg.Paths.ProfilesDir = def.Paths.ProfilesDir
	}
	if cfg.Colab.CLIPath == "" {
		cfg.Colab.CLIPath = def.Colab.CLIPath
	}
	if cfg.Colab.PollIntervalSec < 15 {
		cfg.Colab.PollIntervalSec = def.Colab.PollIntervalSec
	}
	if cfg.Colab.MonthlyAllowance <= 0 {
		cfg.Colab.MonthlyAllowance = def.Colab.MonthlyAllowance
	}
	if cfg.Colab.WarnActiveMinutes <= 0 {
		cfg.Colab.WarnActiveMinutes = def.Colab.WarnActiveMinutes
	}
	if cfg.Colab.LowBalanceCCU < 0 {
		cfg.Colab.LowBalanceCCU = def.Colab.LowBalanceCCU
	}
}

// Save atomically writes cfg to disk with mode 0600 (never leaving backup files).
func Save(cfg Config) error {
	applyDefaults(&cfg)
	p := ConfigPath()
	return writeJSONAtomic(p, cfg, 0o600)
}

func writeJSONAtomic(path string, v any, perm os.FileMode) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeBytesAtomic(path, raw, perm)
}

func writeBytesAtomic(path string, raw []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
	}
	if _, err := tmpFile.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := tmpFile.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// QuotaPoint stores one historical quota reading for runway estimation.
type QuotaPoint struct {
	TS        int64   `json:"ts"`        // Unix seconds
	Remaining float64 `json:"remaining"` // 0.0 - 1.0
	ResetTS   int64   `json:"resetTs"`   // Unix seconds of resetTime
}

// CachedBucket persists one quota bucket snapshot for offline account display.
type CachedBucket struct {
	GroupName         string  `json:"groupName"`
	BucketID          string  `json:"bucketId"`
	DisplayName       string  `json:"displayName"`
	Description       string  `json:"description,omitempty"`
	Window            string  `json:"window"`
	Disabled          bool    `json:"disabled,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	RemainingPct      float64 `json:"remainingPct"`
	ResetTime         string  `json:"resetTime"`
	ResetUnix         int64   `json:"resetUnix"`
}

// CachedAccountState persists an account's latest subscription tier and quota buckets.
type CachedAccountState struct {
	Email     string         `json:"email"`
	UserName  string         `json:"userName,omitempty"`
	PlanName  string         `json:"planName,omitempty"`
	TierID    string         `json:"tierId,omitempty"`
	Buckets   []CachedBucket `json:"buckets,omitempty"`
	UpdatedAt int64          `json:"updatedAt"`
}

// PersistentState stores 14-day quota samples keyed by "<instanceId>:<bucketId>",
// alert dedup keys, last-known account quota snapshots, and Colab visibility flags.
type PersistentState struct {
	mu                  sync.Mutex
	saveMu              sync.Mutex
	dirty               bool
	path                string
	QuotaHistory        map[string][]QuotaPoint       `json:"quotaHistory"`
	LowQuotaSent        map[string]int64              `json:"lowQuotaSent"`
	AccountCache        map[string]CachedAccountState `json:"accountCache,omitempty"`
	InstanceAccounts    map[string]string             `json:"instanceAccounts,omitempty"`
	HiddenColabAccounts map[string]bool               `json:"hiddenColabAccounts,omitempty"`
}

// LoadState opens or initializes the persistent state file.
func LoadState(path string) *PersistentState {
	if path == "" {
		path = StatePath()
	}
	s := &PersistentState{
		path:                path,
		QuotaHistory:        make(map[string][]QuotaPoint),
		LowQuotaSent:        make(map[string]int64),
		AccountCache:        make(map[string]CachedAccountState),
		InstanceAccounts:    make(map[string]string),
		HiddenColabAccounts: make(map[string]bool),
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
		if s.AccountCache == nil {
			s.AccountCache = make(map[string]CachedAccountState)
		}
		if s.InstanceAccounts == nil {
			s.InstanceAccounts = make(map[string]string)
		}
		if s.HiddenColabAccounts == nil {
			s.HiddenColabAccounts = make(map[string]bool)
		}
	}
	return s
}

func sameResetWindow(a, b int64) bool {
	return a == b || (a > 0 && b > 0 && abs64(a-b) <= 60)
}

func sameQuotaState(a, b QuotaPoint) bool {
	if a.Remaining != b.Remaining {
		return false
	}
	if a.Remaining >= 0.999 {
		// Upstream servers slide resetTime = now + window while quota is idle at 100%.
		return true
	}
	return sameResetWindow(a.ResetTS, b.ResetTS)
}

func compactQuotaPoints(pts []QuotaPoint, nowUnix int64) []QuotaPoint {
	if len(pts) == 0 {
		return nil
	}
	cutoff14d := nowUnix - 14*86400
	cutoff24h := nowUnix - 86400
	cutoff2h := nowUnix - 2*3600
	pruned := make([]QuotaPoint, 0, min(len(pts), 256))
	var lastBucket int64 = -1
	var prevReset int64
	var prevRemaining float64
	for _, pt := range pts {
		if pt.TS < cutoff14d {
			continue
		}
		isReset := prevReset > 0 && pt.ResetTS > prevReset+60 && !(prevRemaining >= 0.999 && pt.Remaining >= 0.999)
		prevReset = pt.ResetTS
		prevRemaining = pt.Remaining

		if pt.TS >= cutoff2h {
			if m := len(pruned); m > 0 && !isReset {
				prev := pruned[m-1]
				if sameQuotaState(prev, pt) && pt.TS-prev.TS < 300 {
					continue
				}
			}
			pruned = append(pruned, pt)
			continue
		}

		bucketSec := int64(3600)
		if pt.TS >= cutoff24h {
			bucketSec = 900
		}
		bucket := pt.TS / bucketSec

		m := len(pruned)
		sameAsLast := m > 0 && sameQuotaState(pruned[m-1], pt)
		if !isReset && sameAsLast {
			if m >= 2 && pruned[m-2].TS < cutoff2h && sameQuotaState(pruned[m-2], pt) {
				pruned[m-1].TS = pt.TS
				pruned[m-1].ResetTS = pt.ResetTS
				lastBucket = bucket
				continue
			}
			if bucket != lastBucket {
				lastBucket = bucket
				pruned = append(pruned, pt)
			}
			continue
		}

		if isReset || !sameAsLast || bucket != lastBucket {
			lastBucket = bucket
			pruned = append(pruned, pt)
		}
	}
	if len(pruned) == 0 {
		return nil
	}
	return pruned
}

func quotaPointsEqual(a, b []QuotaPoint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CompactQuotaHistory prunes expired (>14d) and downsamples legacy bloated quota histories
// across all keys (including offline or deleted instances). Returns true if any entry changed.
func (s *PersistentState) CompactQuotaHistory(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.QuotaHistory) == 0 {
		return false
	}
	nowUnix := now.Unix()
	changed := false
	for k, pts := range s.QuotaHistory {
		pruned := compactQuotaPoints(pts, nowUnix)
		if len(pruned) == 0 {
			delete(s.QuotaHistory, k)
			changed = true
			continue
		}
		if !quotaPointsEqual(pts, pruned) {
			s.QuotaHistory[k] = pruned
			changed = true
		}
	}
	if changed {
		s.dirty = true
	}
	return changed
}

// RecordQuota appends a quota observation with 14-day retention:
// - within the past 2 hours (runway window): records all changes and 5-minute idle heartbeats
// - 2h..24h: 15-minute downsampling with flat-plateau collapsing
// - 24h..14d: 1-hour downsampling with flat-plateau collapsing and reset transition preservation
func (s *PersistentState) RecordQuota(key string, remaining float64, resetTS int64, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.QuotaHistory == nil {
		s.QuotaHistory = make(map[string][]QuotaPoint)
	}
	nowUnix := now.Unix()
	pts := s.QuotaHistory[key]
	appendNeeded := true
	if n := len(pts); n > 0 {
		last := pts[n-1]
		cur := QuotaPoint{TS: nowUnix, Remaining: remaining, ResetTS: resetTS}
		sameVal := sameQuotaState(last, cur)
		if sameVal && nowUnix-last.TS < 300 {
			// Fast path: skip redundant unchanged samples unless legacy history needs compaction.
			if n <= 300 {
				return
			}
			appendNeeded = false
		} else if last.TS == nowUnix {
			pts[n-1] = QuotaPoint{
				TS:        nowUnix,
				Remaining: remaining,
				ResetTS:   resetTS,
			}
			appendNeeded = false
		}
	}
	if appendNeeded {
		pts = append(pts, QuotaPoint{
			TS:        nowUnix,
			Remaining: remaining,
			ResetTS:   resetTS,
		})
	}

	s.QuotaHistory[key] = compactQuotaPoints(pts, nowUnix)
	s.dirty = true
}

// RunwayEstimate describes burn rate and estimated hours until empty.
type RunwayEstimate struct {
	BurnPctPerHour      float64 `json:"burnPctPerHour"`
	HoursUntilEmpty     float64 `json:"hoursUntilEmpty"` // -1 if idle/not burning
	ExhaustsBeforeReset bool    `json:"exhaustsBeforeReset"`
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

	if s.LowQuotaSent == nil {
		s.LowQuotaSent = make(map[string]int64)
	}
	prev, ok := s.LowQuotaSent[key]
	if ok && (prev == resetTS || (resetTS == 0 && now.Unix()-prev < 3600)) {
		return false
	}
	if resetTS > 0 {
		s.LowQuotaSent[key] = resetTS
	} else {
		s.LowQuotaSent[key] = now.Unix()
	}
	s.dirty = true
	return true
}

// RecordAccountSnapshot persists an account's latest subscription tier, quota buckets,
// and its mapping from instID so offline instances retain identity and cached quotas.
func (s *PersistentState) RecordAccountSnapshot(instID string, snap CachedAccountState) {
	email := strings.TrimSpace(snap.Email)
	if email == "" {
		return
	}
	lower := strings.ToLower(email)

	cp := CachedAccountState{
		Email:     email,
		UserName:  snap.UserName,
		PlanName:  snap.PlanName,
		TierID:    snap.TierID,
		UpdatedAt: snap.UpdatedAt,
	}
	if len(snap.Buckets) > 0 {
		cp.Buckets = append([]CachedBucket(nil), snap.Buckets...)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.InstanceAccounts == nil {
		s.InstanceAccounts = make(map[string]string)
	}
	if s.AccountCache == nil {
		s.AccountCache = make(map[string]CachedAccountState)
	}
	if instID != "" {
		s.InstanceAccounts[instID] = email
	}
	if prev, ok := s.AccountCache[lower]; ok {
		if cp.UserName == "" {
			cp.UserName = prev.UserName
		}
		if cp.PlanName == "" {
			cp.PlanName = prev.PlanName
			if cp.TierID == "" {
				cp.TierID = prev.TierID
			}
		}
		if len(cp.Buckets) == 0 && len(prev.Buckets) > 0 {
			cp.Buckets = append([]CachedBucket(nil), prev.Buckets...)
		}
	}
	s.AccountCache[lower] = cp
	s.dirty = true
}

// GetAccountSnapshot returns a cloned cached snapshot for email, rolling over any quota
// bucket whose ResetUnix has already passed relative to now.
func (s *PersistentState) GetAccountSnapshot(email string, now time.Time) (CachedAccountState, bool) {
	lower := strings.ToLower(strings.TrimSpace(email))
	if lower == "" {
		return CachedAccountState{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.AccountCache[lower]
	if !ok {
		return CachedAccountState{}, false
	}
	return cloneAndRolloverCachedAccount(snap, now.Unix()), true
}

// AllAccountSnapshots returns a copy of all cached account snapshots with reset rollover applied.
func (s *PersistentState) AllAccountSnapshots(now time.Time) map[string]CachedAccountState {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowUnix := now.Unix()
	out := make(map[string]CachedAccountState, len(s.AccountCache))
	for k, v := range s.AccountCache {
		out[k] = cloneAndRolloverCachedAccount(v, nowUnix)
	}
	return out
}

// LastInstanceAccount returns the last known Google account email associated with instID.
func (s *PersistentState) LastInstanceAccount(instID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.InstanceAccounts[instID]
}

// BucketTrendPct returns a chronologically ordered slice of downsampled remaining % points (0..100) for bucketKey.
func (s *PersistentState) BucketTrendPct(bucketKey string, maxPoints int) []float64 {
	if maxPoints <= 0 {
		maxPoints = 24
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pts := s.QuotaHistory[bucketKey]
	if len(pts) == 0 {
		return nil
	}
	sampled := downsampleQuotaPoints(pts, maxPoints)
	out := make([]float64, 0, len(sampled))
	for _, pt := range sampled {
		pct := pt.Remaining * 100.0
		if pct < 0 {
			pct = 0
		} else if pct > 100 {
			pct = 100
		}
		out = append(out, float64(int(pct*10+0.5))/10.0)
	}
	return out
}

// QuotaSeries returns downsampled historical quota samples grouped by bucket key (optionally filtered by prefix).
func (s *PersistentState) QuotaSeries(prefix string, maxPoints int) map[string][]QuotaPoint {
	if maxPoints <= 0 {
		maxPoints = 72
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]QuotaPoint)
	for k, pts := range s.QuotaHistory {
		if prefix != "" && !strings.HasPrefix(k, prefix) {
			continue
		}
		if len(pts) == 0 {
			continue
		}
		out[k] = downsampleQuotaPoints(pts, maxPoints)
	}
	return out
}

func downsampleQuotaPoints(pts []QuotaPoint, maxPoints int) []QuotaPoint {
	if len(pts) <= maxPoints || maxPoints < 2 {
		cp := make([]QuotaPoint, len(pts))
		copy(cp, pts)
		return cp
	}
	out := make([]QuotaPoint, 0, maxPoints)
	lastIdx := len(pts) - 1
	for i := 0; i < maxPoints; i++ {
		idx := i * lastIdx / (maxPoints - 1)
		out = append(out, pts[idx])
	}
	return out
}

// SetColabHidden records whether a non-OAuth (colab-safe) Colab account was hidden by the user.
func (s *PersistentState) SetColabHidden(email string, hidden bool) {
	lower := strings.ToLower(strings.TrimSpace(email))
	if lower == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.HiddenColabAccounts == nil {
		s.HiddenColabAccounts = make(map[string]bool)
	}
	if hidden {
		if !s.HiddenColabAccounts[lower] {
			s.HiddenColabAccounts[lower] = true
			s.dirty = true
		}
	} else {
		if s.HiddenColabAccounts[lower] {
			delete(s.HiddenColabAccounts, lower)
			s.dirty = true
		}
	}
}

// IsColabHidden reports whether email was marked hidden from the Colab pool.
func (s *PersistentState) IsColabHidden(email string) bool {
	lower := strings.ToLower(strings.TrimSpace(email))
	if lower == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.HiddenColabAccounts[lower]
}

func cloneAndRolloverCachedAccount(src CachedAccountState, nowUnix int64) CachedAccountState {
	dst := src
	if len(src.Buckets) > 0 {
		dst.Buckets = make([]CachedBucket, len(src.Buckets))
		for i, b := range src.Buckets {
			if b.ResetUnix > 0 && nowUnix >= b.ResetUnix {
				b.RemainingFraction = 1.0
				b.RemainingPct = 100.0
				b.ResetTime = ""
				b.ResetUnix = 0
			}
			dst.Buckets[i] = b
		}
	}
	return dst
}

// IsDirty reports whether in-memory state has unsaved changes.
func (s *PersistentState) IsDirty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirty
}

// SaveIfDirty persists state atomically only when in-memory state changed since the last Save.
func (s *PersistentState) SaveIfDirty() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	raw, path, err := s.marshalLocked()
	if err == nil {
		s.dirty = false
	}
	s.mu.Unlock()
	if err != nil || path == "" {
		return err
	}
	if err := writeBytesAtomic(path, raw, 0o600); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return err
	}
	return nil
}

// Save persists state atomically using compact JSON.
// Serialization happens under s.mu while file I/O is serialized under s.saveMu,
// preventing concurrent map iteration/write panics and out-of-order file writes.
func (s *PersistentState) Save() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	s.mu.Lock()
	raw, path, err := s.marshalLocked()
	if err == nil {
		s.dirty = false
	}
	s.mu.Unlock()
	if err != nil || path == "" {
		return err
	}
	if err := writeBytesAtomic(path, raw, 0o600); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *PersistentState) marshalLocked() ([]byte, string, error) {
	snap := struct {
		QuotaHistory        map[string][]QuotaPoint       `json:"quotaHistory"`
		LowQuotaSent        map[string]int64              `json:"lowQuotaSent"`
		AccountCache        map[string]CachedAccountState `json:"accountCache,omitempty"`
		InstanceAccounts    map[string]string             `json:"instanceAccounts,omitempty"`
		HiddenColabAccounts map[string]bool               `json:"hiddenColabAccounts,omitempty"`
	}{
		QuotaHistory:        s.QuotaHistory,
		LowQuotaSent:        s.LowQuotaSent,
		AccountCache:        s.AccountCache,
		InstanceAccounts:    s.InstanceAccounts,
		HiddenColabAccounts: s.HiddenColabAccounts,
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, s.path, err
	}
	raw = append(raw, '\n')
	return raw, s.path, nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
