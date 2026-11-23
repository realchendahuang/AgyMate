// Package cdp manages DevToolsActivePort discovery, idempotent Chinese localization
// injection, and Shadow-DOM micro-capsule HUD rendering inside Antigravity windows.
package cdp

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

//go:embed hud.js
var hudScript string

//go:embed zh_inject.js
var builtinZhScript string

var localPageURLRe = regexp.MustCompile(`^https?://(127\.0\.0\.1|localhost):\d+/`)

// TargetStatus summarizes the CDP state of one Antigravity page target.
type TargetStatus struct {
	InstanceID string `json:"instanceId"`
	Port       int    `json:"port"`
	TargetID   string `json:"targetId"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	WSURL      string `json:"wsUrl"`
	ZhInjected bool   `json:"zhInjected"`
	HUDActive  bool   `json:"hudActive"`
	LastError  string `json:"lastError,omitempty"`
}

// HUDData is pushed into each window's Shadow-DOM HUD (titlebar + composer ring + popover).
type HUDData struct {
	InstanceID       string                     `json:"instanceId,omitempty"`
	Lang             string                     `json:"lang,omitempty"`
	Badge            string                     `json:"badge"`
	Account          string                     `json:"account"`
	AccountCount     int                        `json:"accountCount,omitempty"`
	PlanName         string                     `json:"planName,omitempty"`
	State            string                     `json:"state"`
	StateLabel       string                     `json:"stateLabel,omitempty"`
	Position         string                     `json:"position"`
	HubURL           string                     `json:"hubUrl"`
	ActiveCascadeID  string                     `json:"activeCascadeId,omitempty"`
	ActiveTitle      string                     `json:"activeTitle,omitempty"`
	ActiveModel      string                     `json:"activeModel,omitempty"`
	BurnHint         string                     `json:"burnHint,omitempty"`
	CachedQuota      bool                       `json:"cachedQuota,omitempty"`
	Compacted        bool                       `json:"compacted,omitempty"`
	CheckpointIndex  int                        `json:"checkpointIndex"`
	TruncationReason string                     `json:"truncationReason,omitempty"`
	RequestBudget    int64                      `json:"requestBudget,omitempty"`
	ContextUsed      int64                      `json:"contextUsed"`
	ContextMax       int64                      `json:"contextMax"`
	ContextPct       float64                    `json:"contextPct"`
	ContextWarnPct   int                        `json:"contextWarnPct"`
	QuotaLabel       string                     `json:"quotaLabel"`
	QuotaPct         float64                    `json:"quotaPct"`
	TodayCostLabel   string                     `json:"todayCostLabel"`
	CurrencySymbol   string                     `json:"currencySymbol,omitempty"`
	TodayTokens      int64                      `json:"todayTokens"`
	CacheHitRate     float64                    `json:"cacheHitRate"`
	CacheSavedLabel  string                     `json:"cacheSavedLabel,omitempty"`
	LastTPS          float64                    `json:"lastTps,omitempty"`
	AvgTPS           float64                    `json:"avgTps,omitempty"`
	PeakTPS          float64                    `json:"peakTps,omitempty"`
	LastTTFTMs       int64                      `json:"lastTtftMs,omitempty"`
	LastPrefillTPS   float64                    `json:"lastPrefillTps,omitempty"`
	LastOutputTokens int64                      `json:"lastOutputTokens,omitempty"`
	ColabLabel       string                     `json:"colabLabel,omitempty"`
	ColabBurning     bool                       `json:"colabBurning,omitempty"`
	Buckets          []HUDBucket                `json:"buckets,omitempty"`
	Models           []HUDModelQuota            `json:"models,omitempty"`
	Breakdown        []HUDBreakdown             `json:"breakdown,omitempty"`
	CascadeContexts  map[string]HUDContextBrief `json:"cascadeContexts,omitempty"`
	RecentTurns      []HUDTurnBrief             `json:"recentTurns,omitempty"`
	Accounts         []HUDAccountBrief          `json:"accounts,omitempty"`
	Heatmap          []HUDHeatDay               `json:"heatmap,omitempty"`
}

// HUDBucket displays one rate-limit row & trend sparkline inside the HUD popover.
type HUDBucket struct {
	ID                  string    `json:"id,omitempty"`
	Name                string    `json:"name"`
	Group               string    `json:"group,omitempty"`
	Window              string    `json:"window,omitempty"`
	Description         string    `json:"description,omitempty"`
	Disabled            bool      `json:"disabled,omitempty"`
	Pct                 float64   `json:"pct"`
	ResetLeft           string    `json:"resetLeft"`
	BurnHint            string    `json:"burnHint,omitempty"`
	BurnPctPerHour      float64   `json:"burnPctPerHour,omitempty"`
	HoursUntilEmpty     float64   `json:"hoursUntilEmpty,omitempty"`
	ExhaustsBeforeReset bool      `json:"exhaustsBeforeReset,omitempty"`
	Trend               []float64 `json:"trend,omitempty"`
}

// HUDModelQuota displays per-model quota inside the quota tab.
type HUDModelQuota struct {
	Label     string  `json:"label"`
	ModelID   string  `json:"modelId,omitempty"`
	Pct       float64 `json:"pct"`
	ResetLeft string  `json:"resetLeft,omitempty"`
}

// HUDBreakdown displays one Context group row (and optional children) inside the /context tab.
type HUDBreakdown struct {
	Key      string         `json:"key,omitempty"`
	Name     string         `json:"name"`
	Tokens   int64          `json:"tokens"`
	Children []HUDBreakdown `json:"children,omitempty"`
}

// HUDContextBrief provides per-conversation context & speed isolation across tabs/split panes.
type HUDContextBrief struct {
	CascadeID        string         `json:"cascadeId"`
	Title            string         `json:"title,omitempty"`
	Model            string         `json:"model,omitempty"`
	Used             int64          `json:"used"`
	Max              int64          `json:"max"`
	RequestBudget    int64          `json:"requestBudget,omitempty"`
	Pct              float64        `json:"pct"`
	Compacted        bool           `json:"compacted,omitempty"`
	CheckpointIndex  int            `json:"checkpointIndex"`
	TruncationReason string         `json:"truncationReason,omitempty"`
	LastTPS          float64        `json:"lastTps,omitempty"`
	LastTTFTMs       int64          `json:"lastTtftMs,omitempty"`
	LastPrefillTPS   float64        `json:"lastPrefillTps,omitempty"`
	LastOutputTokens int64          `json:"lastOutputTokens,omitempty"`
	CacheHitRate     float64        `json:"cacheHitRate,omitempty"`
	Breakdown        []HUDBreakdown `json:"breakdown,omitempty"`
}

// HUDTurnBrief provides lightweight turn curve & inspector preview points.
type HUDTurnBrief struct {
	Turn           int     `json:"turn"`
	TimeStr        string  `json:"timeStr,omitempty"`
	Model          string  `json:"model,omitempty"`
	PromptTokens   int64   `json:"promptTokens"`
	ContextDelta   int64   `json:"contextDelta"`
	OutputTokens   int64   `json:"outputTokens"`
	ThinkingTokens int64   `json:"thinkingTokens,omitempty"`
	ResponseTokens int64   `json:"responseTokens,omitempty"`
	TTFTMs         int64   `json:"ttftMs,omitempty"`
	StreamMs       int64   `json:"streamMs,omitempty"`
	TPS            float64 `json:"tps,omitempty"`
	PrefillTPS     float64 `json:"prefillTps,omitempty"`
	CacheHitPct    float64 `json:"cacheHitPct,omitempty"`
	Compacted      bool    `json:"compacted,omitempty"`
	UserSnippet    string  `json:"userSnippet,omitempty"`
}

// HUDAccountBrief summarizes an account's tightest quota in the multi-account strip.
type HUDAccountBrief struct {
	Email        string  `json:"email"`
	PlanName     string  `json:"planName,omitempty"`
	TightestPct  float64 `json:"tightestPct"`
	Active       bool    `json:"active,omitempty"`
	RunningSlots int     `json:"runningSlots,omitempty"`
}

// HUDHeatDay represents a compact daily activity cell for the 12-week heatmap.
type HUDHeatDay struct {
	Date   string  `json:"date"`
	Tokens int64   `json:"tokens"`
	Calls  int     `json:"calls"`
	USD    float64 `json:"usd,omitempty"`
	Level  int     `json:"level"`
}

// Watcher maintains CDP target state and caches inject.js by mtime/hash.
type Watcher struct {
	httpClient  *http.Client
	mu          sync.Mutex
	zhPath      string
	zhMtime     int64
	zhSize      int64
	zhSrc       string
	zhFP        string
	builtinZhFP string
	hudFP       string
	lastSeen    map[string]time.Time
	lastStatus  []TargetStatus
}

// NewWatcher creates a CDP manager.
func NewWatcher() *Watcher {
	hudSum := sha256.Sum256([]byte(hudScript))
	zhSum := sha256.Sum256([]byte(builtinZhScript))
	return &Watcher{
		httpClient:  &http.Client{Timeout: 1500 * time.Millisecond},
		hudFP:       "agy-hud-" + hex.EncodeToString(hudSum[:])[:16],
		builtinZhFP: "agy-zh-builtin-" + hex.EncodeToString(zhSum[:])[:16],
		lastSeen:    make(map[string]time.Time),
	}
}

// HasBuiltinZhScript reports whether the binary embeds the built-in Antigravity Chinese localization script.
func HasBuiltinZhScript() bool {
	return len(strings.TrimSpace(builtinZhScript)) > 0
}

// ResolveZhSource describes whether localization currently loads from a user override file or the built-in engine.
func ResolveZhSource(path string) (label string, external bool) {
	resolved := config.ExpandPath(strings.TrimSpace(path))
	if resolved != "" {
		if info, err := os.Stat(resolved); err == nil && !info.IsDir() && info.Size() > 0 {
			return resolved, true
		}
	}
	if HasBuiltinZhScript() {
		return "builtin", false
	}
	return resolved, false
}

type cdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func (w *Watcher) loadZhScript(path string) (src string, fp string, err error) {
	resolved := config.ExpandPath(strings.TrimSpace(path))
	if resolved != "" {
		if info, statErr := os.Stat(resolved); statErr == nil && !info.IsDir() && info.Size() > 0 {
			w.mu.Lock()
			if w.zhPath == resolved && w.zhMtime == info.ModTime().UnixNano() && w.zhSize == info.Size() && w.zhSrc != "" {
				cachedSrc, cachedFP := w.zhSrc, w.zhFP
				w.mu.Unlock()
				return cachedSrc, cachedFP, nil
			}
			w.mu.Unlock()

			raw, readErr := os.ReadFile(resolved)
			if readErr == nil && len(strings.TrimSpace(string(raw))) > 0 {
				sum := sha256.Sum256(raw)
				nextFP := "agy-zh-" + hex.EncodeToString(sum[:])[:16]
				w.mu.Lock()
				w.zhPath = resolved
				w.zhMtime = info.ModTime().UnixNano()
				w.zhSize = info.Size()
				w.zhSrc = string(raw)
				w.zhFP = nextFP
				w.mu.Unlock()
				return string(raw), nextFP, nil
			}
		}
	}
	if HasBuiltinZhScript() {
		return builtinZhScript, w.builtinZhFP, nil
	}
	return "", "", os.ErrNotExist
}

// ScanTargets discovers live Antigravity page targets on instances with DevToolsActivePort.
func (w *Watcher) ScanTargets(ctx context.Context, instList []instances.Instance) []TargetStatus {
	var out []TargetStatus
	for _, inst := range instList {
		if inst.CDPPort <= 0 {
			continue
		}
		targets, err := w.listJSON(ctx, inst.CDPPort)
		if err != nil {
			continue
		}
		for _, t := range targets {
			if t.Type != "page" || !localPageURLRe.MatchString(t.URL) || t.WebSocketDebuggerURL == "" {
				continue
			}
			out = append(out, TargetStatus{
				InstanceID: inst.ID,
				Port:       inst.CDPPort,
				TargetID:   t.ID,
				Title:      t.Title,
				URL:        t.URL,
				WSURL:      t.WebSocketDebuggerURL,
			})
		}
	}
	return out
}

func (w *Watcher) listJSON(ctx context.Context, port int) ([]cdpTarget, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", port), nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out []cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// SyncAll scans live targets and injects Chinese localization + HUD when enabled.
// When force is true, it bypasses the 30s per-target throttle.
func (w *Watcher) SyncAll(ctx context.Context, instList []instances.Instance, cfg config.Config, hudFn func(instID string) *HUDData, force bool) []TargetStatus {
	if !cfg.CDP.Enabled {
		return nil
	}
	targets := w.ScanTargets(ctx, instList)
	var zhSrc, zhFP string
	var zhErr error
	if cfg.CDP.ZhInject {
		zhSrc, zhFP, zhErr = w.loadZhScript(cfg.CDP.ZhScriptPath)
	}

	hudAllowed := make(map[string]bool)
	for _, id := range cfg.CDP.HUDInstances {
		hudAllowed[id] = true
	}

	now := time.Now()
	for i := range targets {
		tgt := &targets[i]
		key := tgt.InstanceID + "|" + tgt.TargetID

		if cfg.CDP.ZhInject && zhErr == nil && zhSrc != "" {
			w.mu.Lock()
			last := w.lastSeen["zh:"+key]
			w.mu.Unlock()
			if force || now.Sub(last) >= 45*time.Second {
				fpJSON, _ := json.Marshal(zhFP)
				expr := fmt.Sprintf(`(()=>{
					if (window.__agyZhFp === %s) return 'skip';
					window.__agyZhFp = %s;
					%s
					return 'ok';
				})()`, fpJSON, fpJSON, zhSrc)
				if _, err := Eval(ctx, tgt.WSURL, expr); err != nil {
					tgt.LastError = err.Error()
				} else {
					tgt.ZhInjected = true
					w.mu.Lock()
					w.lastSeen["zh:"+key] = now
					w.mu.Unlock()
				}
			} else {
				tgt.ZhInjected = true
			}
		}

		wantHUD := cfg.CDP.WindowHUD && (len(hudAllowed) == 0 || hudAllowed[tgt.InstanceID])
		if wantHUD && hudFn != nil {
			if data := hudFn(tgt.InstanceID); data != nil {
				dataJSON, _ := json.Marshal(data)
				hfpJSON, _ := json.Marshal(w.hudFP)
				// Fast path: update existing HUD in place without resending & reparsing ~60KB hud.js every 3s.
				fastExpr := fmt.Sprintf(`(()=>{
					if (window.__agymateHudFp === %s && typeof window.__agymateHudUpdate === 'function') {
						window.__agymateHudUpdate(%s);
						return 'ok';
					}
					return 'install';
				})()`, hfpJSON, dataJSON)
				val, err := Eval(ctx, tgt.WSURL, fastExpr)
				if err == nil && (val == "ok" || val == true) {
					tgt.HUDActive = true
				} else if err == nil && val == "install" {
					installExpr := fmt.Sprintf(`(()=>{
						if (typeof window.__agymateHudRemove === 'function') {
							try { window.__agymateHudRemove(); } catch (e) {}
						}
						window.__agymateHudFp = %s;
						%s
						if (typeof window.__agymateHudUpdate === 'function') {
							window.__agymateHudUpdate(%s);
							return true;
						}
						return false;
					})()`, hfpJSON, hudScript, dataJSON)
					if ival, ierr := Eval(ctx, tgt.WSURL, installExpr); ierr != nil {
						if tgt.LastError == "" {
							tgt.LastError = ierr.Error()
						}
					} else if b, ok := ival.(bool); ok && b {
						tgt.HUDActive = true
					}
				} else if err != nil {
					if tgt.LastError == "" {
						tgt.LastError = err.Error()
					}
				}
			}
			w.mu.Lock()
			delete(w.lastSeen, "hudoff:"+key)
			w.mu.Unlock()
		} else if !wantHUD {
			w.mu.Lock()
			hadHUD := !w.lastSeen["hudoff:"+key].IsZero()
			w.mu.Unlock()
			if force || !hadHUD {
				_, _ = Eval(ctx, tgt.WSURL, `if (typeof window.__agymateHudRemove === 'function') { window.__agymateHudRemove(); delete window.__agymateHudFp; }`)
				w.mu.Lock()
				w.lastSeen["hudoff:"+key] = now
				w.mu.Unlock()
			}
		}
	}

	activeKeys := make(map[string]struct{}, len(targets))
	for _, tgt := range targets {
		activeKeys[tgt.InstanceID+"|"+tgt.TargetID] = struct{}{}
	}
	w.mu.Lock()
	for k := range w.lastSeen {
		rawKey := strings.TrimPrefix(strings.TrimPrefix(k, "zh:"), "hudoff:")
		if _, ok := activeKeys[rawKey]; !ok {
			delete(w.lastSeen, k)
		}
	}
	w.lastStatus = targets
	w.mu.Unlock()
	return targets
}

// ProbeTargetStatus inspects live window variables (__agyZhFp and #agymate-hud-host shadowRoot)
// on every reachable CDP page target.
func (w *Watcher) ProbeTargetStatus(ctx context.Context, instList []instances.Instance) []TargetStatus {
	targets := w.ScanTargets(ctx, instList)
	for i := range targets {
		val, err := Eval(ctx, targets[i].WSURL, `({zh: Boolean(window.__agyZhFp), hud: Boolean(document.getElementById('agymate-hud-host')?.shadowRoot)})`)
		if err != nil {
			targets[i].LastError = err.Error()
			continue
		}
		if m, ok := val.(map[string]any); ok {
			if b, ok := m["zh"].(bool); ok {
				targets[i].ZhInjected = b
			}
			if b, ok := m["hud"].(bool); ok {
				targets[i].HUDActive = b
			}
		}
	}
	return targets
}

// LastStatus returns the latest cached CDP target states.
func (w *Watcher) LastStatus() []TargetStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make([]TargetStatus, len(w.lastStatus))
	copy(cp, w.lastStatus)
	return cp
}

// Eval evaluates a JavaScript expression in a CDP WebSocket target and returns the decoded value.
func Eval(ctx context.Context, wsURL, expression string) (any, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(cctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 * 1024 * 1024)

	req, err := json.Marshal(map[string]any{
		"id":     1,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expression,
			"returnByValue": true,
			"awaitPromise":  true,
		},
	})
	if err != nil {
		return nil, err
	}
	if err := conn.Write(cctx, websocket.MessageText, req); err != nil {
		return nil, err
	}

	for {
		_, msgBytes, err := conn.Read(cctx)
		if err != nil {
			return nil, err
		}
		var resp struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				Result struct {
					Value any `json:"value"`
				} `json:"result"`
				ExceptionDetails *struct {
					Text string `json:"text"`
				} `json:"exceptionDetails"`
			} `json:"result"`
		}
		if json.Unmarshal(msgBytes, &resp) != nil || resp.ID != 1 {
			continue
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
		if resp.Error != nil {
			return nil, fmt.Errorf("cdp error: %s", resp.Error.Message)
		}
		if resp.Result.ExceptionDetails != nil {
			return nil, fmt.Errorf("js exception: %s", resp.Result.ExceptionDetails.Text)
		}
		return resp.Result.Result.Value, nil
	}
}

// FormatResetCountdown renders a localized Chinese reset countdown such as "1小时17分", "6天20小时", or "15分钟".
func FormatResetCountdown(resetUnix int64) string {
	if resetUnix <= 0 {
		return ""
	}
	return formatResetDiff(resetUnix - time.Now().Unix())
}

func formatResetDiff(diff int64) string {
	if diff <= 0 {
		return "即将重置"
	}
	if diff < 60 {
		return "不足1分钟"
	}
	mins := diff / 60
	hrs := mins / 60
	days := hrs / 24
	if days > 0 {
		remHrs := hrs % 24
		if remHrs > 0 {
			return fmt.Sprintf("%d天%d小时", days, remHrs)
		}
		return fmt.Sprintf("%d天", days)
	}
	if hrs > 0 {
		remMins := mins % 60
		if remMins > 0 {
			return fmt.Sprintf("%d小时%d分", hrs, remMins)
		}
		return fmt.Sprintf("%d小时", hrs)
	}
	return fmt.Sprintf("%d分钟", mins)
}
