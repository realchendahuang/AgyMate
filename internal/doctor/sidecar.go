package doctor

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
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

// PluginPath returns ~/.gemini/config/plugins/agymate.
func PluginPath() string {
	home, _ := os.UserHomeDir()
	return PluginPathIn(home)
}

// PluginPathIn resolves the Antigravity native plugin path inside a specified home directory.
func PluginPathIn(homeDir string) string {
	return filepath.Join(homeDir, ".gemini", "config", "plugins", "agymate")
}

// CheckPlugin verifies whether the AgyMate Antigravity plugin and its MCP App config are installed.
func CheckPlugin() (bool, string) {
	home, _ := os.UserHomeDir()
	return CheckPluginIn(home)
}

// CheckPluginIn checks plugin + MCP App registration under a specific home directory.
func CheckPluginIn(homeDir string) (bool, string) {
	dir := PluginPathIn(homeDir)
	manifestPath := filepath.Join(dir, "plugin.json")
	if _, err := os.Stat(manifestPath); err != nil {
		return false, "未安装插件: " + dir
	}
	mcpPath := filepath.Join(dir, "mcp_config.json")
	raw, err := os.ReadFile(mcpPath)
	if err != nil {
		return false, "缺少 mcp_config.json: " + mcpPath
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &doc) != nil || len(doc.MCPServers) == 0 {
		return false, "mcp_config.json 无效或缺少 mcpServers"
	}
	for _, srv := range doc.MCPServers {
		if strings.TrimSpace(srv.Command) == "" {
			return false, "mcp_config.json 缺少 command"
		}
		// Only require run-mcp.sh when mcp_config.json references it; direct binary invocation works cross-platform.
		for _, a := range srv.Args {
			if strings.Contains(a, "run-mcp.sh") {
				runScript := filepath.Join(dir, "scripts", "run-mcp.sh")
				if st, err := os.Stat(runScript); err != nil || st.IsDir() {
					return false, "缺少 MCP 启动脚本: " + runScript
				}
			}
		}
	}
	return true, dir
}

func defaultCLIBin(homeDir, cliBin string) string {
	if strings.TrimSpace(cliBin) != "" {
		return strings.TrimSpace(cliBin)
	}
	if exe, err := os.Executable(); err == nil {
		base := strings.ToLower(filepath.Base(exe))
		if base == "agymate" || base == "agymate.exe" {
			if realPath, rerr := filepath.EvalSymlinks(exe); rerr == nil {
				return realPath
			}
			return exe
		}
	}
	if found, err := exec.LookPath("agymate"); err == nil && found != "" {
		return found
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
		Command  string   `json:"command"`
		Args     []string `json:"args"`
		HasWebUI bool     `json:"has_web_ui"`
		UIConfig struct {
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

// EnsureSidecarForProfiles registers the AgyMate Sidecar in the user's primary home directory
// and across all discovered isolated profile sandboxes.
func EnsureSidecarForProfiles(cfg config.Config, cliBin string) ([]string, error) {
	var paths []string
	mainPath, err := EnsureSidecar(cliBin)
	if err != nil {
		return nil, err
	}
	paths = append(paths, mainPath)

	for _, inst := range instances.DiscoverAll(cfg) {
		if inst.Kind != instances.KindProfile || strings.TrimSpace(inst.ProfileDir) == "" {
			continue
		}
		profHome := filepath.Join(inst.ProfileDir, "home")
		if info, statErr := os.Stat(profHome); statErr != nil || !info.IsDir() {
			continue
		}
		if p, pErr := EnsureSidecarIn(profHome, cliBin); pErr == nil {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// RemoveSidecarForProfiles removes AgyMate's Sidecar registration from the primary home directory
// and all isolated profile sandboxes.
func RemoveSidecarForProfiles(cfg config.Config) ([]string, error) {
	var removed []string
	removeIn := func(homeDir string) error {
		scDir := filepath.Dir(SidecarPathIn(homeDir))
		if _, err := os.Stat(scDir); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err := os.RemoveAll(scDir); err != nil {
			return err
		}
		removed = append(removed, scDir)
		return nil
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if err := removeIn(home); err != nil {
			return removed, err
		}
	}
	for _, inst := range instances.DiscoverAll(cfg) {
		if inst.Kind != instances.KindProfile || strings.TrimSpace(inst.ProfileDir) == "" {
			continue
		}
		profHome := filepath.Join(inst.ProfileDir, "home")
		if err := removeIn(profHome); err != nil {
			return removed, err
		}
	}
	return removed, nil
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
		"description":    "AgyMate — Antigravity 实例配额、Token 账单、上下文水位与多开管理。",
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

type sidecarCtxKey int

const sidecarAuthedKey sidecarCtxKey = 1

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
		if authed, _ := req.Context().Value(sidecarAuthedKey).(bool); authed && req.Header.Get("X-AgyMate-Token") == "" {
			if tok := config.LoadHubToken(); tok != "" {
				req.Header.Set("X-AgyMate-Token", tok)
			}
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, perr error) {
		w.Header().Set("Cache-Control", "no-store")
		if req != nil && strings.HasPrefix(req.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ok":false,"error":"AgyMate Hub 未启动"}`))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(renderSidecarOfflineHTML(hubListenAddr, perr)))
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

		provided := r.Header.Get("x-sidecar-token")
		if provided == "" {
			provided = r.URL.Query().Get("token")
		}
		sidecarAuthed := uiToken != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(uiToken)) == 1
		if r.Method != http.MethodGet && r.Method != http.MethodHead && uiToken != "" && !sidecarAuthed {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		if sidecarAuthed {
			r = r.WithContext(context.WithValue(r.Context(), sidecarAuthedKey, true))
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

func renderSidecarOfflineHTML(hubListenAddr string, perr error) string {
	errText := "连接被拒绝"
	if perr != nil && strings.TrimSpace(perr.Error()) != "" {
		errText = strings.TrimSpace(perr.Error())
	}
	safeAddr := html.EscapeString(hubListenAddr)
	safeErr := html.EscapeString(errText)

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AgyMate Hub 未启动</title>
<style>
  :root {
    color-scheme: dark light;
    --bg: #0d1017;
    --card: #151923;
    --card-border: rgba(148, 163, 184, 0.16);
    --code-bg: rgba(15, 23, 42, 0.72);
    --text: #f1f5f9;
    --muted: #94a3b8;
    --subtle: #64748b;
    --accent: #6366f1;
    --accent-hover: #4f46e5;
    --accent-fg: #ffffff;
    --warn: #f59e0b;
    --warn-bg: rgba(245, 158, 11, 0.14);
    --ok: #10b981;
    --btn-ghost: rgba(148, 163, 184, 0.12);
    --btn-ghost-hover: rgba(148, 163, 184, 0.2);
    --shadow: 0 18px 42px rgba(0, 0, 0, 0.42);
  }
  @media (prefers-color-scheme: light) {
    :root {
      --bg: #f6f8fb;
      --card: #ffffff;
      --card-border: rgba(15, 23, 42, 0.10);
      --code-bg: #f1f5f9;
      --text: #0f172a;
      --muted: #475569;
      --subtle: #64748b;
      --accent: #4f46e5;
      --accent-hover: #4338ca;
      --accent-fg: #ffffff;
      --warn: #d97706;
      --warn-bg: rgba(217, 119, 6, 0.10);
      --ok: #059669;
      --btn-ghost: rgba(15, 23, 42, 0.06);
      --btn-ghost-hover: rgba(15, 23, 42, 0.11);
      --shadow: 0 12px 32px rgba(15, 23, 42, 0.08);
    }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
    font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", system-ui, sans-serif;
    background: var(--bg);
    color: var(--text);
    line-height: 1.5;
  }
  .card {
    width: 100%%;
    max-width: 392px;
    background: var(--card);
    border: 1px solid var(--card-border);
    border-radius: 14px;
    padding: 20px 18px 18px;
    box-shadow: var(--shadow);
  }
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 3px 10px;
    border-radius: 999px;
    font-size: 11.5px;
    font-weight: 600;
    color: var(--warn);
    background: var(--warn-bg);
    margin-bottom: 12px;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%%;
    background: currentColor;
    animation: pulse 1.8s ease-in-out infinite;
  }
  .badge.ok { color: var(--ok); background: rgba(16, 185, 129, 0.14); }
  @keyframes pulse {
    0%%, 100%% { opacity: 1; transform: scale(1); }
    50%% { opacity: 0.45; transform: scale(0.86); }
  }
  h1 {
    margin: 0 0 6px;
    font-size: 16px;
    font-weight: 650;
    letter-spacing: -0.01em;
  }
  .desc {
    margin: 0 0 14px;
    font-size: 12.5px;
    color: var(--muted);
  }
  .desc code {
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 11.5px;
    padding: 1px 5px;
    border-radius: 5px;
    background: var(--code-bg);
    color: var(--text);
  }
  .cmd-box {
    border: 1px solid var(--card-border);
    background: var(--code-bg);
    border-radius: 10px;
    padding: 10px 11px;
    margin-bottom: 14px;
  }
  .cmd-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 11px;
    color: var(--subtle);
    margin-bottom: 5px;
  }
  .copy-btn {
    border: 0;
    background: var(--btn-ghost);
    color: var(--text);
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 5px;
    cursor: pointer;
    transition: background 0.15s;
  }
  .copy-btn:hover { background: var(--btn-ghost-hover); }
  .cmd-line {
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 12.5px;
    font-weight: 500;
    word-break: break-all;
    user-select: all;
  }
  .cmd-tip {
    margin-top: 6px;
    font-size: 11px;
    color: var(--subtle);
  }
  .actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .retry-btn {
    border: 0;
    border-radius: 8px;
    background: var(--accent);
    color: var(--accent-fg);
    font-size: 12.5px;
    font-weight: 600;
    padding: 7px 14px;
    cursor: pointer;
    white-space: nowrap;
    transition: background 0.15s, opacity 0.15s;
  }
  .retry-btn:hover { background: var(--accent-hover); }
  .retry-btn:disabled { opacity: 0.65; cursor: default; }
  .status-tip {
    font-size: 11.5px;
    color: var(--subtle);
    text-align: right;
  }
  details {
    margin-top: 12px;
    padding-top: 10px;
    border-top: 1px solid var(--card-border);
    font-size: 11px;
    color: var(--subtle);
  }
  summary { cursor: pointer; user-select: none; }
  .err-raw {
    margin-top: 6px;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 11px;
    word-break: break-word;
    opacity: 0.88;
  }
</style>
</head>
<body>
  <main class="card" role="status" aria-live="polite">
    <div class="badge" id="badge"><span class="dot"></span><span id="badge-text">守护服务未连接</span></div>
    <h1>AgyMate Hub 尚未启动</h1>
    <p class="desc">当前无法连接本地伴侣服务 <code>%s</code>。启动后台守护进程后，此面板将自动恢复并载入实时看板。</p>
    <div class="cmd-box">
      <div class="cmd-head">
        <span>终端恢复命令</span>
        <button type="button" class="copy-btn" id="copy-btn">复制命令</button>
      </div>
      <div class="cmd-line" id="cmd-text">agymate service restart</div>
      <div class="cmd-tip">首次部署请运行 <code>agymate service install</code></div>
    </div>
    <div class="actions">
      <button type="button" class="retry-btn" id="retry-btn">立即重试</button>
      <span class="status-tip" id="probe-tip">每 2.5 秒自动探测恢复…</span>
    </div>
    <details>
      <summary>查看连接诊断信息</summary>
      <div class="err-raw">%s</div>
    </details>
  </main>
  <script>
    (function() {
      var probing = false;
      var retryBtn = document.getElementById('retry-btn');
      var probeTip = document.getElementById('probe-tip');
      var badge = document.getElementById('badge');
      var badgeText = document.getElementById('badge-text');
      var copyBtn = document.getElementById('copy-btn');
      var cmd = 'agymate service restart';

      copyBtn.addEventListener('click', function() {
        var done = function() {
          copyBtn.textContent = '已复制 ✓';
          setTimeout(function() { copyBtn.textContent = '复制命令'; }, 1800);
        };
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(cmd).then(done).catch(fallbackCopy);
        } else {
          fallbackCopy();
        }
        function fallbackCopy() {
          try {
            var ta = document.createElement('textarea');
            ta.value = cmd;
            ta.style.position = 'fixed';
            ta.style.opacity = '0';
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
            done();
          } catch (e) {}
        }
      });

      function probe(manual) {
        if (probing) return;
        probing = true;
        if (manual) {
          retryBtn.disabled = true;
          retryBtn.textContent = '正在重连…';
          probeTip.textContent = '正在探测本地 Hub…';
        }
        var ctrl = typeof AbortController !== 'undefined' ? new AbortController() : null;
        var timer = ctrl ? setTimeout(function() { ctrl.abort(); }, 1800) : null;
        fetch('/api/health', { cache: 'no-store', signal: ctrl ? ctrl.signal : undefined })
          .then(function(res) {
            if (res && res.ok) {
              badge.className = 'badge ok';
              badgeText.textContent = '已恢复连接';
              probeTip.textContent = '正在载入 AgyMate 面板…';
              window.location.reload();
              return;
            }
            throw new Error('offline');
          })
          .catch(function() {
            if (manual) {
              probeTip.textContent = '仍未就绪，每 2.5 秒自动探测…';
            }
          })
          .finally(function() {
            if (timer) clearTimeout(timer);
            probing = false;
            retryBtn.disabled = false;
            retryBtn.textContent = '立即重试';
          });
      }

      retryBtn.addEventListener('click', function() { probe(true); });
      setInterval(function() { probe(false); }, 2500);
    })();
  </script>
</body>
</html>`, safeAddr, safeErr)
}
