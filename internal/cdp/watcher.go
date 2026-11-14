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

// HUDData is pushed into each window's Shadow-DOM micro-capsule.
type HUDData struct {
	Badge          string         `json:"badge"`
	Account        string         `json:"account"`
	State          string         `json:"state"`
	Position       string         `json:"position"`
	HubURL         string         `json:"hubUrl"`
	ContextUsed    int64          `json:"contextUsed"`
	ContextMax     int64          `json:"contextMax"`
	ContextPct     float64        `json:"contextPct"`
	ContextWarnPct int            `json:"contextWarnPct"`
	QuotaLabel     string         `json:"quotaLabel"`
	QuotaPct       float64        `json:"quotaPct"`
	TodayCostLabel string         `json:"todayCostLabel"`
	TodayTokens    int64          `json:"todayTokens"`
	CacheHitRate   float64        `json:"cacheHitRate"`
	Buckets        []HUDBucket    `json:"buckets,omitempty"`
	Breakdown      []HUDBreakdown `json:"breakdown,omitempty"`
}

// HUDBucket displays one rate-limit row inside the HUD hover popover.
type HUDBucket struct {
	Name      string  `json:"name"`
	Pct       float64 `json:"pct"`
	ResetLeft string  `json:"resetLeft"`
}

// HUDBreakdown displays one Context group row inside the HUD hover popover.
type HUDBreakdown struct {
	Name   string `json:"name"`
	Tokens int64  `json:"tokens"`
}

// Watcher maintains CDP target state and caches inject.js by mtime/hash.
type Watcher struct {
	httpClient *http.Client
	mu         sync.Mutex
	zhPath     string
	zhMtime    int64
	zhSize     int64
	zhSrc      string
	zhFP       string
	hudFP      string
	lastSeen   map[string]time.Time
	lastStatus []TargetStatus
}

// NewWatcher creates a CDP manager.
func NewWatcher() *Watcher {
	sum := sha256.Sum256([]byte(hudScript))
	return &Watcher{
		httpClient: &http.Client{Timeout: 1500 * time.Millisecond},
		hudFP:      "agy-hud-" + hex.EncodeToString(sum[:])[:16],
		lastSeen:   make(map[string]time.Time),
	}
}

type cdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func (w *Watcher) loadZhScript(path string) (src string, fp string, err error) {
	resolved := config.ExpandPath(path)
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.zhPath == resolved && w.zhMtime == info.ModTime().UnixNano() && w.zhSize == info.Size() && w.zhSrc != "" {
		return w.zhSrc, w.zhFP, nil
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	w.zhPath = resolved
	w.zhMtime = info.ModTime().UnixNano()
	w.zhSize = info.Size()
	w.zhSrc = string(raw)
	w.zhFP = "agy-zh-" + hex.EncodeToString(sum[:])[:16]
	return w.zhSrc, w.zhFP, nil
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
				expr := fmt.Sprintf(`(()=>{
					if (window.__agymateHudFp !== %s) {
						window.__agymateHudFp = %s;
						%s
					}
					if (typeof window.__agymateHudUpdate === 'function') {
						window.__agymateHudUpdate(%s);
						return true;
					}
					return false;
				})()`, hfpJSON, hfpJSON, hudScript, dataJSON)
				if val, err := Eval(ctx, tgt.WSURL, expr); err != nil {
					if tgt.LastError == "" {
						tgt.LastError = err.Error()
					}
				} else if b, ok := val.(bool); ok && b {
					tgt.HUDActive = true
				}
			}
		} else if !cfg.CDP.WindowHUD {
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
		if cfg.CDP.WindowHUD {
			w.mu.Lock()
			delete(w.lastSeen, "hudoff:"+key)
			w.mu.Unlock()
		}
	}

	w.mu.Lock()
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

// FormatResetCountdown renders a compact human-readable reset countdown such as "1h17m" or "6d20h".
func FormatResetCountdown(resetUnix int64) string {
	if resetUnix <= 0 {
		return ""
	}
	diff := resetUnix - time.Now().Unix()
	if diff <= 0 {
		return "即将重置"
	}
	mins := diff / 60
	hrs := mins / 60
	days := hrs / 24
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hrs%24)
	}
	if hrs > 0 {
		return fmt.Sprintf("%dh%dm", hrs, mins%60)
	}
	return fmt.Sprintf("%dm", mins)
}

func init() {
	_ = strings.TrimSpace // keep strings imported if expanded
}
