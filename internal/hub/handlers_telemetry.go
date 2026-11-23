// Package hub: SQLite telemetry, CSV export, conversation detail, session search, and cache HTTP handlers.
package hub

import (
	"fmt"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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
	rep := s.store.QueryTelemetry(instList, preset, mode, instFilter, s.effectiveMonthlySubUSD(cfg))
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
	rep := s.store.QueryTelemetry(instList, preset, mode, instFilter, s.effectiveMonthlySubUSD(cfg))
	csvBytes := s.store.ExportCSV(rep)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="agymate-telemetry-%s.csv"`, preset))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csvBytes)
}

func (s *Server) handleTelemetrySaveCSV(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	q := r.URL.Query()
	var req struct {
		Preset   string `json:"preset"`
		Mode     string `json:"mode"`
		Instance string `json:"instance"`
	}
	_ = decodeOptionalJSON(r, &req)

	preset := firstNonEmpty(req.Preset, q.Get("preset"), "last_7d")
	mode := firstNonEmpty(req.Mode, q.Get("mode"), cfg.Pricing.Mode)
	instFilter := firstNonEmpty(req.Instance, q.Get("instance"), "all")
	safePreset := sanitizeFilenameToken(preset)

	instList := s.cachedInstancesOrDiscover(cfg)
	rep := s.store.QueryTelemetry(instList, preset, mode, instFilter, s.effectiveMonthlySubUSD(cfg))
	csvBytes := s.store.ExportCSV(rep)

	outDir := os.TempDir()
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dl := filepath.Join(home, "Downloads")
		if mkErr := os.MkdirAll(dl, 0o755); mkErr == nil {
			outDir = dl
		}
	}
	filename := fmt.Sprintf("agymate-telemetry-%s-%s.csv", safePreset, time.Now().Format("20060102-150405"))
	outPath := filepath.Join(outDir, filename)

	tmpFile, err := os.CreateTemp(outDir, ".agymate-csv-*")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(csvBytes); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"path":  outPath,
		"bytes": len(csvBytes),
	})
}

func sanitizeFilenameToken(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
	if cleaned == "" {
		return "export"
	}
	return cleaned
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
				Account:    firstNonEmpty(inst.Account, snap.Email),
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

func (s *Server) handleConversation(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	q := r.URL.Query()
	instID := strings.TrimSpace(q.Get("instance"))
	cascadeID := strings.TrimSpace(q.Get("cascadeId"))
	mode := firstNonEmpty(q.Get("mode"), cfg.Pricing.Mode)

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

	if instID == "" {
		instID = "all"
	}
	if cascadeID == "" {
		// Pick the active conversation from the requested (or first running) instance.
		for _, inst := range instList {
			if instID != "all" && inst.ID != instID {
				continue
			}
			if snap, ok := snaps[inst.ID]; ok && snap.Context != nil && snap.Context.CascadeID != "" {
				cascadeID = snap.Context.CascadeID
				instID = inst.ID
				break
			}
		}
	}

	var rpcCtx *rpc.ContextWindow
	for _, inst := range instList {
		if instID != "all" && inst.ID != instID {
			continue
		}
		if snap, ok := snaps[inst.ID]; ok {
			if snap.Context != nil && snap.Context.CascadeID == cascadeID {
				cp := *snap.Context
				rpcCtx = &cp
				break
			}
			if cw, ok := snap.TrajectoryContexts[cascadeID]; ok && cw != nil {
				cp := *cw
				rpcCtx = &cp
				break
			}
		}
		if cascadeID != "" && inst.Hub != nil && inst.Hub.HTTPPort > 0 && s.rpcClient != nil {
			if cw := s.rpcClient.FetchConversationContext(r.Context(), inst.ID, inst.Hub, rpc.TrajectoryInfo{CascadeID: cascadeID}); cw != nil {
				rpcCtx = cw
				break
			}
		}
	}

	var detail telemetry.ConversationDetail
	if s.store != nil && cascadeID != "" {
		detail = s.store.QueryConversationDetail(r.Context(), instList, instID, cascadeID, mode)
		if detail.Title == "" && rpcCtx != nil {
			detail.Title = rpcCtx.Title
		}
		if detail.ModelName == "" && rpcCtx != nil {
			detail.ModelName = rpcCtx.Model
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"instanceId": instID,
		"cascadeId":  cascadeID,
		"context":    rpcCtx,
		"detail":     detail,
		"pricing":    cfg.Pricing,
	})
}

func (s *Server) handleTelemetryHeatmap(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	q := r.URL.Query()
	instFilter := firstNonEmpty(q.Get("instance"), "all")
	mode := firstNonEmpty(q.Get("mode"), cfg.Pricing.Mode)
	weeks := 26
	if wNum, err := strconv.Atoi(q.Get("weeks")); err == nil && wNum > 0 && wNum <= 53 {
		weeks = wNum
	}
	instList := s.cachedInstancesOrDiscover(cfg)
	var days []telemetry.HeatmapDay
	if s.store != nil {
		days = s.store.BuildHeatmap(instList, instFilter, mode, weeks)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"instance": instFilter,
		"weeks":    weeks,
		"days":     days,
	})
}

func (s *Server) handleQuotaHistory(w http.ResponseWriter, r *http.Request) {
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	maxPts := 72
	if pNum, err := strconv.Atoi(r.URL.Query().Get("max")); err == nil && pNum >= 8 && pNum <= 240 {
		maxPts = pNum
	}
	var series map[string][]config.QuotaPoint
	if s.state != nil {
		series = s.state.QuotaSeries(prefix, maxPts)
	} else {
		series = make(map[string][]config.QuotaPoint)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"series": series,
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
