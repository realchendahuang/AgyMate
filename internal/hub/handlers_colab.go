// Package hub: Google Colab OAuth2, compute pool, and VM teardown HTTP handlers.
package hub

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os/exec"
	"runtime"
	"strings"

	"github.com/realchendahuang/AgyMate/internal/colab"
	"github.com/realchendahuang/AgyMate/internal/config"
)

func (s *Server) handleColabGet(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"
	snap := s.colabCollector.Probe(r.Context(), s.Config().Colab, force)
	if force {
		s.broadcast("overview")
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleColabNuke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
	}
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	res, snap, err := s.colabCollector.NukeAccount(r.Context(), s.Config().Colab, req.Account)
	s.broadcast("overview")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":    false,
			"error": err.Error(),
			"nuke":  res,
			"colab": snap,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"nuke":  res,
		"colab": snap,
	})
}

func (s *Server) handleColabAccountLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LoginHint   string `json:"loginHint"`
		OpenBrowser *bool  `json:"openBrowser"`
	}
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	openBrowser := true
	if req.OpenBrowser != nil {
		openBrowser = *req.OpenBrowser
	}
	authURL, state, err := s.colabCollector.StartOAuthFlow(s.Config().ListenAddr, req.LoginHint)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	opened := false
	if openBrowser {
		opened = openURLInBrowser(authURL) == nil
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"authUrl": authURL,
		"state":   state,
		"opened":  opened,
	})
}

func (s *Server) handleColabOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if oauthErr := strings.TrimSpace(q.Get("error")); oauthErr != "" {
		msg := firstNonEmpty(q.Get("error_description"), oauthErr)
		writeOAuthResultHTML(w, http.StatusBadRequest, false, "授权未完成", msg)
		return
	}
	acc, err := s.colabCollector.CompleteOAuthFlow(r.Context(), q.Get("code"), q.Get("state"))
	if err != nil {
		writeOAuthResultHTML(w, http.StatusBadRequest, false, "授权失败", err.Error())
		return
	}

	s.state.SetColabHidden(acc.Email, false)
	_ = s.state.Save()

	s.cfgMu.Lock()
	if !s.cfg.Colab.Enabled {
		next := s.cfg
		next.Colab.Enabled = true
		if serr := config.Save(next); serr == nil {
			s.cfg = next
		}
	}
	s.cfgMu.Unlock()
	go s.PollOnceWithForce(context.Background(), true)

	writeOAuthResultHTML(w, http.StatusOK, true, "Colab 账号已绑定", fmt.Sprintf("%s 已接入 AgyMate 算力池，可关闭此标签页返回面板。", acc.Email))
}

func (s *Server) handleColabAccountRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		writeJSONError(w, http.StatusBadRequest, "email is required")
		return
	}
	removed, err := s.colabCollector.RemoveAccount(email)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.state.SetColabHidden(email, true)
	_ = s.state.Save()

	snap := s.colabCollector.Probe(r.Context(), s.Config().Colab, true)
	s.broadcast("overview")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"removed": removed,
		"colab":   snap,
	})
}

func writeOAuthResultHTML(w http.ResponseWriter, status int, ok bool, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	accent := "#16a34a"
	if !ok {
		accent = "#dc2626"
	}
	autoClose := ""
	if ok {
		autoClose = `<script>setTimeout(function(){try{window.close();}catch(e){}}, 1200);</script>`
	}
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>%s · AgyMate</title></head><body style="font-family:-apple-system,BlinkMacSystemFont,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#0f1115;color:#f3f4f6"><div style="max-width:420px;padding:24px 28px;border:1px solid #262b36;border-radius:12px;background:#161922"><h2 style="margin:0 0 10px;font-size:18px;color:%s">%s</h2><p style="margin:0;font-size:14px;line-height:1.5;color:#cbd5e1">%s</p></div>%s</body></html>`,
		html.EscapeString(title),
		accent,
		html.EscapeString(title),
		html.EscapeString(detail),
		autoClose,
	)
}

func resolveColabForAccount(colabSnap colab.Snapshot, account string) *colab.Snapshot {
	if !colabSnap.Enabled {
		return nil
	}
	target := strings.TrimSpace(account)
	if len(colabSnap.Accounts) > 0 {
		if target == "" {
			return nil
		}
		for i := range colabSnap.Accounts {
			ca := &colabSnap.Accounts[i]
			if ca.OK && strings.EqualFold(strings.TrimSpace(ca.Account), target) {
				return ca
			}
		}
		return nil
	}
	if !colabSnap.OK {
		return nil
	}
	cliAcct := strings.TrimSpace(colabSnap.Account)
	if cliAcct == "" || (target != "" && strings.EqualFold(cliAcct, target)) {
		return &colabSnap
	}
	return nil
}

func openURLInBrowser(targetURL string) error {
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return errors.New("empty url")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", targetURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL).Start()
	default:
		return exec.Command("xdg-open", targetURL).Start()
	}
}
