// Package hub implements AgyMate's loopback HTTP API server, SSE event broadcaster,
// background RPC/SQLite polling loop, and 3-second CDP injection watcher.
package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/colab"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/notify"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"
)

// InstanceView combines discovered instance metadata with live RPC telemetry and today's cost.
type InstanceView struct {
	instances.Instance
	Snapshot          *rpc.InstanceSnapshot `json:"snapshot,omitempty"`
	TodayCalls        int                   `json:"todayCalls"`
	TodayTokens       int64                 `json:"todayTokens"`
	TodayUSD          float64               `json:"todayUsd"`
	TodayCacheHitRate float64               `json:"todayCacheHitRate"`
	LiveState         string                `json:"liveState"` // waiting_approval | running | idle | offline
	ActiveSessions    int                   `json:"activeSessions"`
	CDPTargets        []cdp.TargetStatus    `json:"cdpTargets,omitempty"`
}

// AccountView aggregates client slots, live or cached quota, context, and today's cost per Google account.
type AccountView struct {
	Key               string             `json:"key"` // "acct:<lower-email>" or slot ID when unbound
	Email             string             `json:"email,omitempty"`
	DisplayName       string             `json:"displayName"`
	UserName          string             `json:"userName,omitempty"`
	PlanName          string             `json:"planName,omitempty"`
	TierID            string             `json:"tierId,omitempty"`
	LiveState         string             `json:"liveState"` // waiting_approval | running | idle | offline
	OnlineCount       int                `json:"onlineCount"`
	TotalSlots        int                `json:"totalSlots"`
	ActiveSessions    int                `json:"activeSessions"`
	CachedQuota       bool               `json:"cachedQuota,omitempty"`
	QuotaUpdatedAt    int64              `json:"quotaUpdatedAt,omitempty"`
	Buckets           []rpc.QuotaBucket  `json:"buckets,omitempty"`
	Context           *rpc.ContextWindow `json:"context,omitempty"`
	TodayCalls        int                `json:"todayCalls"`
	TodayTokens       int64              `json:"todayTokens"`
	TodayUSD          float64            `json:"todayUsd"`
	TodayCacheHitRate float64            `json:"todayCacheHitRate"`
	Instances         []InstanceView     `json:"instances"`
}

// OverviewPayload is served by GET /api/overview for both the dashboard and menu bar.
type OverviewPayload struct {
	Version       string                     `json:"version"`
	Language      string                     `json:"language,omitempty"`
	UpdatedAt     int64                      `json:"updatedAt"`
	PollMs        float64                    `json:"pollMs"`
	Pricing       config.PricingConfig       `json:"pricing"`
	CDPZhEnabled  bool                       `json:"cdpZhEnabled"`
	DoctorCounts  map[string]int             `json:"doctorCounts"`
	Accounts      []AccountView              `json:"accounts"`
	Instances     []InstanceView             `json:"instances"`
	TodaySummary  telemetry.TelemetrySummary `json:"todaySummary"`
	BurnRate15m   telemetry.BurnRate         `json:"burnRate15m"`
	MonthROI      telemetry.MonthlyROI       `json:"monthRoi"`
	LiveSessions  []hooks.LiveSession        `json:"liveSessions"`
	CDPTargets    []cdp.TargetStatus         `json:"cdpTargets"`
	Colab         colab.Snapshot             `json:"colab"`
	ColabAccounts []colab.Snapshot           `json:"colabAccounts,omitempty"`
}

// ContextEntry pairs one running instance ID with its active ContextWindow.
type ContextEntry struct {
	InstanceID string             `json:"instanceId"`
	Account    string             `json:"account,omitempty"`
	Label      string             `json:"label"`
	Context    *rpc.ContextWindow `json:"context"`
}

// Server orchestrates AgyMate's local HTTP server and background telemetry loops.
type Server struct {
	cfgMu          sync.RWMutex
	cfg            config.Config
	state          *config.PersistentState
	rpcClient      *rpc.Client
	store          *telemetry.Store
	tracker        *hooks.Tracker
	cdpWatcher     *cdp.Watcher
	colabCollector *colab.Collector
	notifier       *notify.Client
	frontendFS     fs.FS
	authToken      string
	startedAt      int64

	pollMu       sync.Mutex
	mu           sync.RWMutex
	bootDone     chan struct{}
	instList     []instances.Instance
	snapshots    map[string]rpc.InstanceSnapshot
	todayRep     telemetry.TelemetryReport
	instToday    map[string]telemetry.InstAggregate
	doctorRep    doctor.Report
	lastPollAt   int64
	lastPollMs   float64
	lastStateSav int64
	lastDocCheck int64

	sseMu sync.Mutex
	subs  map[chan string]struct{}
}

// NewServer constructs an AgyMate hub server.
func NewServer(cfg config.Config, frontendFS fs.FS) *Server {
	rpcCli := rpc.NewClient()
	authTok, err := config.LoadOrCreateHubToken()
	if err != nil || strings.TrimSpace(authTok) == "" {
		// Never fail open: fall back to a strong ephemeral in-memory token if disk persistence fails.
		buf := make([]byte, 32)
		if _, rerr := rand.Read(buf); rerr == nil {
			authTok = hex.EncodeToString(buf)
		} else {
			authTok = fmt.Sprintf("ephemeral-%d", time.Now().UnixNano())
		}
	}
	s := &Server{
		cfg:            cfg,
		state:          config.LoadState(""),
		rpcClient:      rpcCli,
		store:          telemetry.NewStore(rpcCli.ModelLabel),
		cdpWatcher:     cdp.NewWatcher(),
		colabCollector: colab.NewCollector(),
		notifier:       notify.NewClient(),
		frontendFS:     frontendFS,
		authToken:      authTok,
		startedAt:      time.Now().Unix(),
		snapshots:      make(map[string]rpc.InstanceSnapshot),
		instToday:      make(map[string]telemetry.InstAggregate),
		subs:           make(map[chan string]struct{}),
	}
	s.tracker = hooks.NewTracker(s.Config)
	s.colabCollector.SetHiddenChecker(s.state.IsColabHidden)
	return s
}

// AuthToken returns the local CSRF/auth token required by mutating API endpoints.
func (s *Server) AuthToken() string {
	return s.authToken
}

// Config returns a copy of the active runtime configuration.
func (s *Server) Config() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// SetConfig updates runtime config in memory.
func (s *Server) SetConfig(cfg config.Config) {
	s.cfgMu.Lock()
	s.cfg = cfg
	s.cfgMu.Unlock()
}

// Start runs the initial poll and background telemetry + CDP loops until ctx is canceled.
func (s *Server) Start(ctx context.Context) {
	bootCh := make(chan struct{})
	s.mu.Lock()
	s.bootDone = bootCh
	s.mu.Unlock()

	go func() {
		s.PollOnce(ctx)
		close(bootCh)
		if s.Config().CDP.Enabled {
			s.SyncCDPOnce(ctx, false)
		}

		for {
			sec := s.Config().PollIntervalSec
			if sec < 5 {
				sec = 5
			}
			if sec > 120 {
				sec = 120
			}
			timer := time.NewTimer(time.Duration(sec) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				_ = s.state.SaveIfDirty()
				return
			case <-timer.C:
				s.PollOnce(ctx)
			}
		}
	}()

	go func() {
		select {
		case <-bootCh:
		case <-ctx.Done():
			return
		}
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.Config().CDP.Enabled {
					s.SyncCDPOnce(ctx, false)
				}
			}
		}
	}()
}

func (s *Server) waitBootPoll(ctx context.Context) {
	s.mu.RLock()
	ch := s.bootDone
	s.mu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-ctx.Done():
	}
}

// PollOnce refreshes instance discovery, Connect-RPC snapshots, hook reconciliation,
// Colab telemetry, low-quota Telegram alerts, and today's SQLite cost summary.
func (s *Server) PollOnce(ctx context.Context) {
	s.PollOnceWithForce(ctx, false)
}

// PollOnceWithForce refreshes telemetry and optionally bypasses Colab's PollIntervalSec cache.
func (s *Server) PollOnceWithForce(ctx context.Context, forceColab bool) {
	s.pollMu.Lock()
	defer s.pollMu.Unlock()

	t0 := time.Now()
	cfg := s.Config()
	instList := instances.DiscoverAll(cfg)

	type rpcRes struct {
		id   string
		snap rpc.InstanceSnapshot
	}
	var wg sync.WaitGroup
	resCh := make(chan rpcRes, len(instList))

	for _, inst := range instList {
		if inst.Hub == nil {
			continue
		}
		wg.Add(1)
		go func(in instances.Instance) {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			snap := s.rpcClient.FetchInstance(rctx, in.ID, in.Hub, s.state)
			resCh <- rpcRes{id: in.ID, snap: snap}
		}(inst)
	}

	wg.Add(1)
	go func(ccfg config.ColabConfig) {
		defer wg.Done()
		_ = s.colabCollector.Probe(ctx, ccfg, forceColab)
	}(cfg.Colab)

	wg.Wait()
	close(resCh)

	snaps := make(map[string]rpc.InstanceSnapshot, len(instList))
	for r := range resCh {
		snaps[r.id] = r.snap
	}

	now := time.Now()
	for i := range instList {
		inst := &instList[i]
		snap, hasSnap := snaps[inst.ID]
		if hasSnap && strings.TrimSpace(snap.Email) != "" {
			inst.Account = strings.TrimSpace(snap.Email)
		} else if strings.TrimSpace(inst.Account) == "" {
			inst.Account = s.state.LastInstanceAccount(inst.ID)
		}
		if hasSnap {
			s.tracker.ReconcileRPC(*inst, snap)

			email := strings.TrimSpace(firstNonEmpty(snap.Email, inst.Account))
			if email != "" && (len(snap.Buckets) > 0 || snap.PlanName != "" || snap.TierID != "") {
				s.state.RecordAccountSnapshot(inst.ID, config.CachedAccountState{
					Email:     email,
					UserName:  snap.UserName,
					PlanName:  snap.PlanName,
					TierID:    snap.TierID,
					Buckets:   toCachedBuckets(snap.Buckets),
					UpdatedAt: now.Unix(),
				})
			}

			if cfg.HasAnyNotificationChannel() && cfg.Telegram.NotifyLowQuota {
				acctKey := strings.ToLower(firstNonEmpty(inst.Account, snap.Email, inst.ID))
				acctLabel := firstNonEmpty(inst.Account, snap.Email, inst.Label, inst.ID)
				for _, b := range snap.Buckets {
					if b.Disabled {
						continue
					}
					if b.RemainingPct <= float64(cfg.QuotaWarnPct) {
						key := acctKey + ":" + b.BucketID
						if s.state.ShouldNotifyLowQuota(key, b.ResetUnix, now) {
							msg := notify.FormatLowQuotaAlert(acctLabel, b.DisplayName, b.Window, b.RemainingPct, b.ResetTime)
							s.sendAlertAsync(cfg, "AgyMate Quota Alert", msg)
						}
					}
				}
			}
		}
	}

	if cfg.HasAnyNotificationChannel() {
		for _, al := range s.colabCollector.EvaluateAccountAlerts(cfg.Colab, now) {
			colabSnap := al.Snapshot
			if al.BurnMsgNeeded {
				hwParts := make([]string, 0, len(colabSnap.Sessions))
				for _, sess := range colabSnap.Sessions {
					tag := sess.Name
					if sess.Orphan {
						tag = "孤儿[?]"
					}
					hwParts = append(hwParts, fmt.Sprintf("%s:%s", tag, sess.Accelerator))
				}
				msg := notify.FormatColabBurnAlert(
					colabSnap.Account,
					colabSnap.AssignmentsCount,
					colabSnap.OrphanCount,
					colabSnap.ActiveMinutes,
					colabSnap.ConsumptionRateHourly,
					colabSnap.CurrentBalance,
					strings.Join(hwParts, ", "),
				)
				s.sendAlertAsync(cfg, "AgyMate Colab Burn Alert", msg)
			}
			if al.LowMsgNeeded && cfg.Telegram.NotifyLowQuota {
				msg := notify.FormatColabLowBalanceAlert(colabSnap.Account, colabSnap.CurrentBalance, colabSnap.ConsumptionRateHourly)
				s.sendAlertAsync(cfg, "AgyMate Colab Low Balance", msg)
			}
		}
	}

	todayRep := s.store.QueryTelemetry(instList, "today", cfg.Pricing.Mode, "all", effectiveMonthlySubUSDWithState(cfg, snaps, s.state, now))
	instToday := make(map[string]telemetry.InstAggregate, len(todayRep.ByInstance))
	for _, a := range todayRep.ByInstance {
		instToday[a.InstanceID] = a
	}

	s.mu.RLock()
	needDoctor := now.Unix()-s.lastDocCheck >= 60 || s.doctorRep.CheckedAt == 0
	s.mu.RUnlock()

	var docRep doctor.Report
	if needDoctor {
		docRep = doctor.RunChecks(cfg, instList, s.cdpWatcher.LastStatus())
	}

	s.mu.Lock()
	shouldCompact := s.lastStateSav == 0
	s.mu.Unlock()
	if shouldCompact {
		s.state.CompactQuotaHistory(now)
	}

	s.mu.Lock()
	s.instList = instList
	s.snapshots = snaps
	s.todayRep = todayRep
	s.instToday = instToday
	if needDoctor {
		s.doctorRep = docRep
		s.lastDocCheck = now.Unix()
	}
	s.lastPollAt = now.Unix()
	s.lastPollMs = float64(time.Since(t0).Microseconds()) / 1000.0
	shouldSave := s.state.IsDirty() && (s.lastStateSav == 0 || now.Unix()-s.lastStateSav >= 60)
	if shouldSave {
		s.lastStateSav = now.Unix()
	}
	s.mu.Unlock()

	if shouldSave {
		_ = s.state.SaveIfDirty()
	}
	s.broadcast("overview")
}

// Handler returns the HTTP router with loopback enforcement on every route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/colab", s.handleColabGet)
	mux.HandleFunc("POST /api/colab/nuke", s.handleColabNuke)
	mux.HandleFunc("POST /api/colab/accounts/login", s.handleColabAccountLogin)
	mux.HandleFunc("GET /api/colab/oauth/callback", s.handleColabOAuthCallback)
	mux.HandleFunc("POST /api/colab/accounts/remove", s.handleColabAccountRemove)
	mux.HandleFunc("GET /api/telemetry", s.handleTelemetry)
	mux.HandleFunc("GET /api/telemetry/heatmap", s.handleTelemetryHeatmap)
	mux.HandleFunc("GET /api/telemetry/export.csv", s.handleTelemetryCSV)
	mux.HandleFunc("POST /api/telemetry/save-csv", s.handleTelemetrySaveCSV)
	mux.HandleFunc("GET /api/conversation", s.handleConversation)
	mux.HandleFunc("GET /api/quota/history", s.handleQuotaHistory)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/doctor", s.handleDoctor)
	mux.HandleFunc("POST /api/doctor/fix", s.handleDoctorFix)
	mux.HandleFunc("POST /api/instances/{id}/focus", s.handleInstanceFocus)
	mux.HandleFunc("POST /api/instances/{id}/launch", s.handleInstanceLaunch)
	mux.HandleFunc("POST /api/instances/{id}/stop", s.handleInstanceStop)
	mux.HandleFunc("POST /api/instances/{id}/heal", s.handleInstanceHeal)
	mux.HandleFunc("POST /api/instances/{id}/delete", s.handleInstanceDelete)
	mux.HandleFunc("POST /api/profiles/create", s.handleProfileCreate)
	mux.HandleFunc("GET /api/cache", s.handleCacheScan)
	mux.HandleFunc("POST /api/cache/clean", s.handleCacheClean)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/notify/test", s.handleNotifyTest)
	mux.HandleFunc("POST /api/cdp/inject-now", s.handleCDPInjectNow)
	mux.HandleFunc("POST /api/cdp/sync", s.handleCDPInjectNow)
	mux.HandleFunc("POST /api/hooks/ingest", s.handleHooksIngest)

	if s.frontendFS != nil {
		fileSrv := http.FileServerFS(s.frontendFS)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSONError(w, http.StatusNotFound, "unknown API route")
				return
			}
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) && (r.URL.Path == "/" || r.URL.Path == "/index.html") {
				if s.serveIndexHTML(w, r) {
					return
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			fileSrv.ServeHTTP(w, r)
		})
	}

	return s.securityGuard(mux)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 16)
	s.sseMu.Lock()
	s.subs[ch] = struct{}{}
	s.sseMu.Unlock()
	defer func() {
		s.sseMu.Lock()
		delete(s.subs, ch)
		s.sseMu.Unlock()
	}()

	if _, err := fmt.Fprintf(w, "event: connected\ndata: {\"ok\":true}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-ch:
			if _, err := fmt.Fprintf(w, "event: update\ndata: %q\n\n", ev); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) broadcast(kind string) {
	s.sseMu.Lock()
	for ch := range s.subs {
		select {
		case ch <- kind:
		default:
		}
	}
	s.sseMu.Unlock()
}

func (s *Server) sendAlertAsync(cfg config.Config, title, msg string) {
	go func(c config.Config, t, m string) {
		tctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		_ = s.notifier.Dispatch(tctx, c, t, m)
	}(cfg, title, msg)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (s *Server) cachedInstancesOrDiscover(cfg config.Config) []instances.Instance {
	s.mu.RLock()
	instList := make([]instances.Instance, len(s.instList))
	copy(instList, s.instList)
	s.mu.RUnlock()
	if len(instList) == 0 {
		return instances.DiscoverAll(cfg)
	}
	return instList
}

func (s *Server) activeCascadeIDs() map[string]bool {
	out := make(map[string]bool)
	for _, sess := range s.tracker.Snapshot() {
		if sess.State == "running" || sess.State == "waiting_approval" {
			out[sess.ConversationID] = true
		}
	}
	s.mu.RLock()
	for _, snap := range s.snapshots {
		for _, tr := range snap.Trajectories {
			if tr.Running {
				out[tr.CascadeID] = true
			}
		}
		if snap.Context != nil && snap.Context.CascadeID != "" {
			out[snap.Context.CascadeID] = true
		}
	}
	s.mu.RUnlock()
	return out
}

func decodeOptionalJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": msg,
	})
}
