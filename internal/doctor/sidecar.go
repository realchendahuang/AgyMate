package doctor

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SidecarPath returns ~/.gemini/config/sidecars/agymate/sidecar.json.
func SidecarPath() string {
	home, _ := os.UserHomeDir()
	return SidecarPathIn(home)
}

// SidecarPathIn resolves the sidecar registration path inside a specified home directory.
func SidecarPathIn(homeDir string) string {
	return filepath.Join(homeDir, ".gemini", "config", "sidecars", "agymate", "sidecar.json")
}

func defaultCLIBin(homeDir, cliBin string) string {
	if strings.TrimSpace(cliBin) != "" {
		return strings.TrimSpace(cliBin)
	}
	return filepath.Join(homeDir, ".local", "bin", "agymate")
}

// CheckSidecar verifies whether AgyMate is registered as an Antigravity native Sidecar.
func CheckSidecar(cliBin string) (bool, string) {
	home, _ := os.UserHomeDir()
	return CheckSidecarIn(home, cliBin)
}

// CheckSidecarIn checks sidecar registration under a specific home directory (test helper).
func CheckSidecarIn(homeDir, cliBin string) (bool, string) {
	p := SidecarPathIn(homeDir)
	raw, err := os.ReadFile(p)
	if err != nil {
		return false, "未注册: " + p
	}
	var doc struct {
		Command   string   `json:"command"`
		Args      []string `json:"args"`
		HasWebUI  bool     `json:"has_web_ui"`
		UIConfig  struct {
			Views []struct {
				Path       string `json:"path"`
				Entrypoint string `json:"entrypoint"`
			} `json:"views"`
		} `json:"ui_config"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, "sidecar.json 解析失败"
	}
	if !doc.HasWebUI || doc.Command == "" {
		return false, "sidecar.json 缺少 command 或 has_web_ui"
	}
	if cliBin != "" && doc.Command != cliBin {
		return false, fmt.Sprintf("command=%s (期望 %s)", doc.Command, cliBin)
	}
	if len(doc.UIConfig.Views) == 0 || doc.UIConfig.Views[0].Entrypoint != "SIDECAR_UI_ENTRYPOINT_AUX_PANE" {
		return false, "ui_config.views 缺少 AUX_PANE 入口"
	}
	return true, p
}

// EnsureSidecar writes ~/.gemini/config/sidecars/agymate/sidecar.json atomically.
func EnsureSidecar(cliBin string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return EnsureSidecarIn(home, cliBin)
}

// EnsureSidecarIn writes the AgyMate sidecar manifest inside a specified home directory.
func EnsureSidecarIn(homeDir, cliBin string) (string, error) {
	bin := defaultCLIBin(homeDir, cliBin)
	p := SidecarPathIn(homeDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}

	manifest := map[string]any{
		"command":        bin,
		"args":           []string{"sidecar-proxy"},
		"restart_policy": "always",
		"display_name":   "AgyMate",
		"description":    "Antigravity 五开伴侣：实时配额、SQLite Token 账单、上下文水位与沙盒护航。",
		"has_web_ui":     true,
		"agent_permissions": map[string]any{
			"allows_user_interactions": false,
		},
		"ui_config": map[string]any{
			"display_name": "AgyMate",
			"views": []map[string]any{
				{
					"path":       "/?mode=sidecar",
					"entrypoint": "SIDECAR_UI_ENTRYPOINT_AUX_PANE",
					"title":      "AgyMate",
				},
			},
		},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return p, nil
}

// RunSidecarProxy implements Antigravity's native sidecar_sdk HTTP contract on
// 127.0.0.1:$ANTIGRAVITY_SIDECAR_WEB_PORT and reverse-proxies UI & API traffic to the local AgyMate Hub.
func RunSidecarProxy(ctx context.Context, hubListenAddr string) error {
	port := strings.TrimSpace(os.Getenv("ANTIGRAVITY_SIDECAR_WEB_PORT"))
	if port == "" {
		return fmt.Errorf("missing ANTIGRAVITY_SIDECAR_WEB_PORT (run inside Antigravity sidecar runtime)")
	}
	uiToken := os.Getenv("ANTIGRAVITY_SIDECAR_UI_TOKEN")

	if hubListenAddr == "" {
		hubListenAddr = "127.0.0.1:3778"
	}
	if !strings.HasPrefix(hubListenAddr, "http://") && !strings.HasPrefix(hubListenAddr, "https://") {
		hubListenAddr = "http://" + hubListenAddr
	}
	target, err := url.Parse(hubListenAddr)
	if err != nil {
		return fmt.Errorf("invalid hub address %q: %w", hubListenAddr, err)
	}

	home, _ := os.UserHomeDir()
	preloadPath := filepath.Join(home, ".gemini", "antigravity", "lib", "node_modules", "sidecar_sdk", "preload.js")
	if _, err := os.Stat(preloadPath); err != nil {
		preloadPath = filepath.Join(home, ".gemini", "config", "node_modules", "sidecar_sdk", "preload.js")
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		// Strip sidecar token parameter before forwarding to the local Hub.
		if req.URL != nil {
			q := req.URL.Query()
			q.Del("token")
			if req.URL.Path == "/" && q.Get("mode") == "" {
				q.Set("mode", "sidecar")
			}
			req.URL.RawQuery = q.Encode()
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, perr error) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><body style="font-family:system-ui;padding:24px;background:#11161d;color:#e5edf5"><h3>AgyMate Hub 未连接</h3><p style="color:#94a3b8">无法连接 <code>%s</code> (%v)。请确认 AgyMate 已启动。</p></body>`, hubListenAddr, perr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/preload.js" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			if b, err := os.ReadFile(preloadPath); err == nil {
				_, _ = w.Write(bytes.TrimSpace(b))
				return
			}
			_, _ = w.Write([]byte("// agymate sidecar preload\n"))
			return
		}

		if r.Method != http.MethodGet && uiToken != "" {
			provided := r.Header.Get("x-sidecar-token")
			if provided == "" {
				provided = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(provided), []byte(uiToken)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
		}

		proxy.ServeHTTP(w, r)
	})

	addr := net.JoinHostPort("127.0.0.1", port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("[sidecar] listening on %s -> %s\n", addr, hubListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}
