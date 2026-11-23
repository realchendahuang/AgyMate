// Package hub: isolated profile lifecycle, CDP sync, and hook ingestion HTTP handlers.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

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
	cfg := s.Config()

	needOAuthHint := false
	if runtime.GOOS == "darwin" {
		var mainRunning, targetMissingAuth bool
		for _, inst := range s.cachedInstancesOrDiscover(cfg) {
			if inst.ID == instances.KindMain && inst.Running {
				mainRunning = true
			}
			if inst.ID == id && inst.AuthState == "missing" {
				targetMissingAuth = true
			}
		}
		needOAuthHint = mainRunning && targetMissingAuth
	}

	pid, err := instances.LaunchProfile(cfg, id, req.Workspace, req.ForceHeal)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, instances.ErrReadOnlyLifecycle) {
			status = http.StatusForbidden
		}
		writeJSONError(w, status, err.Error())
		return
	}
	go s.PollOnce(context.Background())
	resp := map[string]any{"ok": true, "id": id, "pid": pid}
	if needOAuthHint {
		resp["oauthHint"] = fmt.Sprintf("已启动 %s · 首次登录若浏览器回调跳回默认 Antigravity 客户端，请先退出该客户端完成一次授权", id)
	}
	writeJSON(w, http.StatusOK, resp)
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

func (s *Server) handleInstanceDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isProtectedLifecycle(id) {
		writeJSONError(w, http.StatusForbidden, instances.ErrReadOnlyLifecycle.Error())
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	_ = decodeOptionalJSON(r, &req)
	cfg := s.Config()
	if err := instances.DeleteProfile(cfg, id, req.Force); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, instances.ErrReadOnlyLifecycle) {
			status = http.StatusForbidden
		}
		writeJSONError(w, status, err.Error())
		return
	}
	_, _ = hooks.InstallHooks(cfg, "")
	go s.PollOnce(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
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

func (s *Server) handleCDPInjectNow(w http.ResponseWriter, r *http.Request) {
	s.waitBootPoll(r.Context())
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
	if ev.InstanceID == "" {
		ev.InstanceID = hooks.InferInstanceIDForDir(s.Config().Paths.ProfilesDir, ev)
	}
	sess := s.tracker.Ingest(ev)
	s.broadcast("sessions")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"session": sess,
	})
}

func isProtectedLifecycle(id string) bool {
	low := strings.ToLower(strings.TrimSpace(id))
	return low == instances.KindMain || low == instances.KindIDE
}
