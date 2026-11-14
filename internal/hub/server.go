// Package hub implements AgyMate's loopback HTTP API server, SSE event broadcaster,
// background RPC/SQLite polling loop, and 3-second CDP injection watcher.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/notify"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"github.com/realchendahuang/AgyMate/internal/version"
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

// OverviewPayload is served by GET /api/overview for both the dashboard and menu bar.
type OverviewPayload struct {
	Version      string                     `json:"version"`
	UpdatedAt    int64                      `json:"updatedAt"`
	PollMs       float64                    `json:"pollMs"`
	Pricing      config.PricingConfig       `json:"pricing"`
	DoctorCounts map[string]int             `json:"doctorCounts"`
	Instances    []InstanceView             `json:"instances"`
	TodaySummary telemetry.TelemetrySummary `json:"todaySummary"`
	BurnRate15m  telemetry.BurnRate         `json:"burnRate15m"`
	MonthROI     telemetry.MonthlyROI       `json:"monthRoi"`
	LiveSessions []hooks.LiveSession        `json:"liveSessions"`
	CDPTargets   []cdp.TargetStatus         `json:"cdpTargets"`
}

// ContextEntry pairs one running instance ID with its active ContextWindow.
type ContextEntry struct {
	InstanceID string             `json:"instanceId"`
	Label      string             `json:"label"`
	Context    *rpc.ContextWindow `json:"context"`
}

// Server orchestrates AgyMate's local HTTP server and background telemetry loops.
type Server struct {
	cfgMu      sync.RWMutex
	cfg        config.Config
	state      *config.PersistentState
	rpcClient  *rpc.Client
	store      *telemetry.Store
	tracker    *hooks.Tracker
	cdpWatcher *cdp.Watcher
	notifier   *notify.Client
	frontendFS fs.FS
	startedAt  int64

	pollMu       sync.Mutex
	mu           sync.RWMutex
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
	s := &Server{
		cfg:        cfg,
		state:      config.LoadState(""),
		rpcClient:  rpcCli,
		store:      telemetry.NewStore(rpcCli.ModelLabel),
		cdpWatcher: cdp.NewWatcher(),
		notifier:   notify.NewClient(),
		frontendFS: frontendFS,
		startedAt:  time.Now().Unix(),
		snapshots:  make(map[string]rpc.InstanceSnapshot),
		instToday:  make(map[string]telemetry.InstAggregate),
		subs:       make(map[chan string]struct{}),
	}
	s.tracker = hooks.NewTracker(s.Config)
	return s
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
	s.PollOnce(ctx)
	s.SyncCDPOnce(ctx, false)

	go func() {
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
				_ = s.state.Save()
				return
			case <-timer.C:
				s.PollOnce(ctx)
			}
		}
	}()

	go func() {
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

// PollOnce refreshes instance discovery, Connect-RPC snapshots, hook reconciliation,
// low-quota Telegram alerts, and today's SQLite cost summary.
func (s *Server) PollOnce(ctx context.Context) {
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
	wg.Wait()
	close(resCh)

	snaps := make(map[string]rpc.InstanceSnapshot, len(instList))
	for r := range resCh {
		snaps[r.id] = r.snap
	}

	now := time.Now()
	for i := range instList {
		inst := &instList[i]
		if snap, ok := snaps[inst.ID]; ok {
			if snap.Email != "" && inst.Account == "" {
				inst.Account = snap.Email
			}
			s.tracker.ReconcileRPC(*inst, snap)

			if cfg.Telegram.Enabled && cfg.Telegram.NotifyLowQuota && cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
				for _, b := range snap.Buckets {
					if b.RemainingPct <= float64(cfg.QuotaWarnPct) {
						key := inst.ID + ":" + b.BucketID
						if s.state.ShouldNotifyLowQuota(key, b.ResetUnix, now) {
							msg := notify.FormatLowQuotaAlert(inst.ID, b.DisplayName, b.Window, b.RemainingPct, b.ResetTime)
							go func(tok, chat, m string) {
								tctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
								defer cancel()
								_ = s.notifier.SendHTML(tctx, tok, chat, m)
							}(cfg.Telegram.BotToken, cfg.Telegram.ChatID, msg)
						}
					}
				}
			}
		}
	}

	todayRep := s.store.QueryTelemetry(instList, "today", cfg.Pricing.Mode, "all", cfg.Pricing.UltraMonthlyUSD)
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
	shouldSave := now.Unix()-s.lastStateSav >= 60
	if shouldSave {
		s.lastStateSav = now.Unix()
	}
	s.mu.Unlock()

	if shouldSave {
		_ = s.state.Save()
	}
	s.broadcast("overview")
}

// SyncCDPOnce pushes Chinese localization & HUD state across reachable CDP targets.
func (s *Server) SyncCDPOnce(ctx context.Context, force bool) []cdp.TargetStatus {
	cfg := s.Config()
	s.mu.RLock()
	instList := make([]instances.Instance, len(s.instList))
	copy(instList, s.instList)
	snaps := make(map[string]rpc.InstanceSnapshot, len(s.snapshots))
	for k, v := range s.snapshots {
		snaps[k] = v
	}
	instToday := make(map[string]telemetry.InstAggregate, len(s.instToday))
	for k, v := range s.instToday {
		instToday[k] = v
	}
	s.mu.RUnlock()

	if len(instList) == 0 {
		instList = instances.DiscoverAll(cfg)
	}
	liveSessions := s.tracker.Snapshot()

	instByID := make(map[string]instances.Instance, len(instList))
	for _, item := range instList {
		instByID[item.ID] = item
	}

	hudFn := func(instID string) *cdp.HUDData {
		inst, ok := instByID[instID]
		if !ok {
			inst = instances.Instance{ID: instID, Running: true}
		}
		snap := snaps[inst.ID]
		today := instToday[inst.ID]
		badge := strings.ToUpper(inst.ID)
		if inst.ID == instances.KindMain {
			badge = "ULTRA"
		}

		state := "idle"
		if !inst.Running {
			state = "offline"
		} else {
			for _, ls := range liveSessions {
				if ls.InstanceID == inst.ID {
					if ls.State == "waiting_approval" {
						state = "waiting"
						break
					}
					if ls.State == "running" {
						state = "running"
					}
				}
			}
			if state == "idle" && snap.RunningCount > 0 {
				state = "running"
			}
		}

		pos := cfg.CDP.HUDPosition
		if pos == "" {
			pos = "bottom-right"
		}
		account := inst.Account
		if account == "" {
			account = snap.Email
		}

		hd := cdp.HUDData{
			Badge:          badge,
			Account:        account,
			State:          state,
			Position:       pos,
			HubURL:         "http://" + cfg.ListenAddr,
			ContextWarnPct: cfg.ContextWarnPct,
			TodayCostLabel: formatMoney(today.TotalUSD, cfg.Pricing),
			TodayTokens:    today.Total,
			CacheHitRate:   today.CacheRate,
		}
		if snap.Context != nil {
			hd.ContextUsed = snap.Context.EstimatedTokensUsed
			hd.ContextMax = snap.Context.MaxContextTokens
			hd.ContextPct = snap.Context.UsagePct
			for _, grp := range snap.Context.TokenBreakdown {
				hd.Breakdown = append(hd.Breakdown, cdp.HUDBreakdown{
					Name:   grp.Name,
					Tokens: grp.NumTokens,
				})
			}
		}
		for i, b := range snap.Buckets {
			if i == 0 {
				hd.QuotaLabel = b.DisplayName
				hd.QuotaPct = b.RemainingPct
			}
			hd.Buckets = append(hd.Buckets, cdp.HUDBucket{
				Name:      b.DisplayName,
				Pct:       b.RemainingPct,
				ResetLeft: cdp.FormatResetCountdown(b.ResetUnix),
			})
		}
		return &hd
	}

	targets := s.cdpWatcher.SyncAll(ctx, instList, cfg, hudFn, force)
	s.broadcast("cdp")
	return targets
}

// BuildOverview composes a complete OverviewPayload snapshot.
func (s *Server) BuildOverview() OverviewPayload {
	cfg := s.Config()
	s.mu.RLock()
	instList := make([]instances.Instance, len(s.instList))
	copy(instList, s.instList)
	snaps := make(map[string]rpc.InstanceSnapshot, len(s.snapshots))
	for k, v := range s.snapshots {
		snaps[k] = v
	}
	todayRep := s.todayRep
	instToday := make(map[string]telemetry.InstAggregate, len(s.instToday))
	for k, v := range s.instToday {
		instToday[k] = v
	}
	docRep := s.doctorRep
	updatedAt := s.lastPollAt
	pollMs := s.lastPollMs
	s.mu.RUnlock()

	liveSessions := s.tracker.Snapshot()
	cdpTargets := s.cdpWatcher.LastStatus()
	cdpByInst := make(map[string][]cdp.TargetStatus)
	for _, t := range cdpTargets {
		cdpByInst[t.InstanceID] = append(cdpByInst[t.InstanceID], t)
	}

	views := make([]InstanceView, 0, len(instList))
	for _, inst := range instList {
		iv := InstanceView{
			Instance:   inst,
			LiveState:  "offline",
			CDPTargets: cdpByInst[inst.ID],
		}
		if snap, ok := snaps[inst.ID]; ok {
			snapCopy := snap
			iv.Snapshot = &snapCopy
		}
		if ag, ok := instToday[inst.ID]; ok {
			iv.TodayCalls = ag.Calls
			iv.TodayTokens = ag.Total
			iv.TodayUSD = ag.TotalUSD
			iv.TodayCacheHitRate = ag.CacheRate
		}
		if inst.Running {
			iv.LiveState = "idle"
			for _, ls := range liveSessions {
				if ls.InstanceID == inst.ID && (ls.State == "running" || ls.State == "waiting_approval") {
					iv.ActiveSessions++
					if ls.State == "waiting_approval" {
						iv.LiveState = "waiting_approval"
					} else if iv.LiveState != "waiting_approval" {
						iv.LiveState = "running"
					}
				}
			}
			if iv.Snapshot != nil && iv.Snapshot.RunningCount > iv.ActiveSessions {
				iv.ActiveSessions = iv.Snapshot.RunningCount
			}
			if iv.ActiveSessions > 0 && iv.LiveState == "idle" {
				iv.LiveState = "running"
			}
		}
		views = append(views, iv)
	}

	return OverviewPayload{
		Version: version.Version,
		UpdatedAt: updatedAt,
		PollMs:    pollMs,
		Pricing:   cfg.Pricing,
		DoctorCounts: map[string]int{
			"ok":    docRep.OKCount,
			"warn":  docRep.WarnCount,
			"error": docRep.ErrorCount,
		},
		Instances:    views,
		TodaySummary: todayRep.Summary,
		BurnRate15m:  todayRep.BurnRate15m,
		MonthROI:     todayRep.MonthROI,
		LiveSessions: liveSessions,
		CDPTargets:   cdpTargets,
	}
}

// MenuBarSummary builds a compact macOS Menu Bar label + tooltip from the current state.
func (s *Server) MenuBarSummary() (string, string) {
	ov := s.BuildOverview()
	quotaStr := "--%"
	var tipParts []string
	for _, iv := range ov.Instances {
		if iv.ID == instances.KindMain && iv.Snapshot != nil && len(iv.Snapshot.Buckets) > 0 {
			quotaStr = fmt.Sprintf("%.0f%%", iv.Snapshot.Buckets[0].RemainingPct)
		}
		if iv.Running {
			tipParts = append(tipParts, fmt.Sprintf("%s(%s)", iv.ID, iv.LiveState))
		}
	}
	label := fmt.Sprintf("U:%s · %s", quotaStr, formatMoney(ov.TodaySummary.TotalUSD, ov.Pricing))
	tooltip := "AgyMate — Antigravity 五开伴侣"
	if len(tipParts) > 0 {
		tooltip += " | 运行中: " + strings.Join(tipParts, ", ")
	}
	return label, tooltip
}

// Handler returns the HTTP router with loopback enforcement on every route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/telemetry", s.handleTelemetry)
	mux.HandleFunc("GET /api/telemetry/export.csv", s.handleTelemetryCSV)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/doctor", s.handleDoctor)
	mux.HandleFunc("POST /api/doctor/fix", s.handleDoctorFix)
	mux.HandleFunc("POST /api/doctor/sync-box", s.handleDoctorSyncBox)
	mux.HandleFunc("POST /api/instances/{id}/focus", s.handleInstanceFocus)
	mux.HandleFunc("POST /api/instances/{id}/launch", s.handleInstanceLaunch)
	mux.HandleFunc("POST /api/instances/{id}/stop", s.handleInstanceStop)
	mux.HandleFunc("POST /api/instances/{id}/heal", s.handleInstanceHeal)
	mux.HandleFunc("POST /api/profiles/create", s.handleProfileCreate)
	mux.HandleFunc("GET /api/cache", s.handleCacheScan)
	mux.HandleFunc("POST /api/cache/clean", s.handleCacheClean)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/notify/test", s.handleNotifyTest)
	mux.HandleFunc("POST /api/cdp/inject-now", s.handleCDPInjectNow)
	mux.HandleFunc("POST /api/hooks/ingest", s.handleHooksIngest)

	if s.frontendFS != nil {
		fileSrv := http.FileServer(http.FS(s.frontendFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSONError(w, http.StatusNotFound, "unknown API route")
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			if r.URL.Path == "/" {
				r2 := *r
				u := *r.URL
				u.Path = "/index.html"
				r2.URL = &u
				fileSrv.ServeHTTP(w, &r2)
				return
			}
			fileSrv.ServeHTTP(w, r)
		})
	}

	return loopbackGuard(mux)
}

func loopbackGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemote(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden, "forbidden: loopback access only")
			return
		}
		if r.Header.Get("Origin") != "" && !isLoopbackOrigin(r.Header.Get("Origin")) {
			writeJSONError(w, http.StatusForbidden, "forbidden origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRemote(remoteAddr string) bool {
	if remoteAddr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isLoopbackOrigin(origin string) bool {
	if origin == "null" || strings.HasPrefix(origin, "wails://") {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	cfg := s.Config()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"version":    version.Version,
		"listenAddr": cfg.ListenAddr,
		"startedAt":  s.startedAt,
	})
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

	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case ev := <-ch:
			_, _ = fmt.Fprintf(w, "event: update\ndata: %q\n\n", ev)
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

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.PollOnce(r.Context())
	}
	writeJSON(w, http.StatusOK, s.BuildOverview())
}

func (s *Server) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	q := r.URL.Query()
	preset := q.Get("preset")
	if preset == "" {
		preset = "today"
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = cfg.Pricing.Mode
	}
	instFilter := q.Get("instance")
	if instFilter == "" {
		instFilter = "all"
	}

	instList := s.cachedInstancesOrDiscover(cfg)
	rep := s.store.QueryTelemetry(instList, preset, mode, instFilter, cfg.Pricing.UltraMonthlyUSD)
	writeJSON(w, http.StatusOK, map[string]any{
		"report":  rep,
		"pricing": cfg.Pricing,
	})
}

func (s *Server) handleTelemetryCSV(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	q := r.URL.Query()
	preset := q.Get("preset")
	if preset == "" {
		preset = "last_7d"
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = cfg.Pricing.Mode
	}
	instFilter := q.Get("instance")
	if instFilter == "" {
		instFilter = "all"
	}

	instList := s.cachedInstancesOrDiscover(cfg)
	rep := s.store.QueryTelemetry(instList, preset, mode, instFilter, cfg.Pricing.UltraMonthlyUSD)
	csvBytes := s.store.ExportCSV(rep)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="agymate-telemetry-%s.csv"`, preset))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csvBytes)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	q := r.URL.Query()
	query := q.Get("q")
	instFilter := q.Get("instance")
	if instFilter == "" {
		instFilter = "all"
	}
	limit := 40
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	s.mu.RLock()
	instList := make([]instances.Instance, len(s.instList))
	copy(instList, s.instList)
	snaps := make(map[string]rpc.InstanceSnapshot, len(s.snapshots))
	for k, v := range s.snapshots {
		snaps[k] = v
	}
	s.mu.RUnlock()
	if len(instList) == 0 {
		instList = instances.DiscoverAll(cfg)
	}

	var contexts []ContextEntry
	for _, inst := range instList {
		if snap, ok := snaps[inst.ID]; ok && snap.Context != nil {
			ctxCopy := *snap.Context
			contexts = append(contexts, ContextEntry{
				InstanceID: inst.ID,
				Label:      inst.Label,
				Context:    &ctxCopy,
			})
		}
	}

	hits := s.store.SearchConversations(r.Context(), instList, query, instFilter, limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"live":     s.tracker.Snapshot(),
		"contexts": contexts,
		"hits":     hits,
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

func (s *Server) handleDoctorSyncBox(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	out, err := doctor.SyncBox(r.Context(), cfg)
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	s.PollOnce(r.Context())
	s.mu.RLock()
	docRep := s.doctorRep
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"output": out,
		"doctor": docRep,
	})
}

func (s *Server) handleInstanceFocus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	instList := s.cachedInstancesOrDiscover(s.Config())
	for _, inst := range instList {
		if inst.ID == id {
			if err := instances.FocusWindow(inst); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
			return
		}
	}
	writeJSONError(w, http.StatusNotFound, "instance not found: "+id)
}

func (s *Server) handleInstanceLaunch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isProtectedLifecycle(id) {
		writeJSONError(w, http.StatusForbidden, instances.ErrReadOnlyLifecycle.Error())
		return
	}
	var req struct {
		Workspace string `json:"workspace"`
		ForceHeal bool   `json:"forceHeal"`
	}
	_ = decodeOptionalJSON(r, &req)
	pid, err := instances.LaunchProfile(s.Config(), id, req.Workspace, req.ForceHeal)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, instances.ErrReadOnlyLifecycle) {
			status = http.StatusForbidden
		}
		writeJSONError(w, status, err.Error())
		return
	}
	go s.PollOnce(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "pid": pid})
}

func (s *Server) handleInstanceStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isProtectedLifecycle(id) {
		writeJSONError(w, http.StatusForbidden, instances.ErrReadOnlyLifecycle.Error())
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	_ = decodeOptionalJSON(r, &req)
	if err := instances.StopProfile(s.Config(), id, req.Force); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, instances.ErrReadOnlyLifecycle) {
			status = http.StatusForbidden
		}
		writeJSONError(w, status, err.Error())
		return
	}
	go s.PollOnce(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

func (s *Server) handleInstanceHeal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isProtectedLifecycle(id) {
		writeJSONError(w, http.StatusForbidden, instances.ErrReadOnlyLifecycle.Error())
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	_ = decodeOptionalJSON(r, &req)
	rep, err := instances.HealProfile(s.Config(), id, req.Force)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, instances.ErrReadOnlyLifecycle) {
			status = http.StatusForbidden
		}
		writeJSONError(w, status, err.Error())
		return
	}
	go s.PollOnce(context.Background())
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var opts instances.CreateProfileOpts
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&opts); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if isProtectedLifecycle(opts.Name) {
		writeJSONError(w, http.StatusForbidden, instances.ErrReadOnlyLifecycle.Error())
		return
	}
	cfg := s.Config()
	meta, err := instances.CreateProfile(cfg, opts)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, _ = hooks.InstallHooks(cfg, "")
	var pid int
	if opts.Launch {
		pid, _ = instances.LaunchProfile(cfg, opts.Name, opts.WorkspacePath, false)
	}
	go s.PollOnce(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"meta": meta,
		"pid":  pid,
	})
}

func (s *Server) handleCacheScan(w http.ResponseWriter, r *http.Request) {
	keep := 20
	if k, err := strconv.Atoi(r.URL.Query().Get("keep")); err == nil && k >= 1 && k <= 500 {
		keep = k
	}
	instList := s.cachedInstancesOrDiscover(s.Config())
	sum := s.store.ScanCache(instList, keep, s.activeCascadeIDs())
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleCacheClean(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KeepCount int    `json:"keepCount"`
		Instance  string `json:"instance"`
	}
	_ = decodeOptionalJSON(r, &req)
	if req.KeepCount < 1 {
		req.KeepCount = 20
	}
	if req.Instance == "" {
		req.Instance = "all"
	}
	instList := s.cachedInstancesOrDiscover(s.Config())
	res := s.store.CleanCache(instList, req.KeepCount, req.Instance, s.activeCascadeIDs())
	scan := s.store.ScanCache(instList, req.KeepCount, s.activeCascadeIDs())
	writeJSON(w, http.StatusOK, map[string]any{
		"cleaned": res,
		"scan":    scan,
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	if r.URL.Query().Get("masked") == "1" && len(cfg.Telegram.BotToken) > 8 {
		cfg.Telegram.BotToken = cfg.Telegram.BotToken[:4] + "..." + cfg.Telegram.BotToken[len(cfg.Telegram.BotToken)-4:]
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var next config.Config
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&next); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid config JSON: "+err.Error())
		return
	}
	cur := s.Config()
	if next.ListenAddr == "" {
		next.ListenAddr = cur.ListenAddr
	}
	if next.PollIntervalSec < 5 {
		next.PollIntervalSec = 5
	}
	if next.ContextWarnPct <= 0 {
		next.ContextWarnPct = 80
	}
	if next.QuotaWarnPct <= 0 {
		next.QuotaWarnPct = 20
	}
	if next.Pricing.USDToCNY <= 0 {
		next.Pricing.USDToCNY = 7.20
	}
	if next.Pricing.UltraMonthlyUSD <= 0 {
		next.Pricing.UltraMonthlyUSD = 100.0
	}
	if strings.Contains(next.Telegram.BotToken, "...") {
		next.Telegram.BotToken = cur.Telegram.BotToken
	}
	if err := config.Save(next); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.SetConfig(next)
	go func() {
		s.PollOnce(context.Background())
		if next.CDP.Enabled {
			s.SyncCDPOnce(context.Background(), true)
		}
	}()
	writeJSON(w, http.StatusOK, next)
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BotToken string `json:"botToken"`
		ChatID   string `json:"chatId"`
	}
	_ = decodeOptionalJSON(r, &req)
	cfg := s.Config()
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCDPInjectNow(w http.ResponseWriter, r *http.Request) {
	targets := s.SyncCDPOnce(r.Context(), true)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"targets": targets,
	})
}

func (s *Server) handleHooksIngest(w http.ResponseWriter, r *http.Request) {
	var ev hooks.EventPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&ev); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	sess := s.tracker.Ingest(ev)
	s.broadcast("sessions")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"session": sess,
	})
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

func isProtectedLifecycle(id string) bool {
	low := strings.ToLower(strings.TrimSpace(id))
	return low == instances.KindMain || low == instances.KindIDE
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

func formatMoney(usd float64, pricing config.PricingConfig) string {
	if strings.EqualFold(pricing.Currency, "CNY") {
		rate := pricing.USDToCNY
		if rate <= 0 {
			rate = 7.20
		}
		return fmt.Sprintf("¥%.2f", usd*rate)
	}
	if usd >= 100 {
		return fmt.Sprintf("$%.1f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

