package doctor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEnsureAndCheckSidecarIn(t *testing.T) {
	home := t.TempDir()
	cli := "/Users/example/.local/bin/agymate"

	ok, _ := CheckSidecarIn(home, cli)
	if ok {
		t.Fatal("expected CheckSidecarIn to be false before registration")
	}

	writtenPath, err := EnsureSidecarIn(home, cli)
	if err != nil {
		t.Fatalf("EnsureSidecarIn error: %v", err)
	}
	if _, err := os.Stat(writtenPath); err != nil {
		t.Fatalf("expected %s to exist: %v", writtenPath, err)
	}
	if _, err := os.Stat(writtenPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no lingering .tmp file next to %s", writtenPath)
	}

	ok, detail := CheckSidecarIn(home, cli)
	if !ok {
		t.Fatalf("expected CheckSidecarIn=true after EnsureSidecarIn, got false (%s)", detail)
	}
}

func TestCheckPluginIn(t *testing.T) {
	home := t.TempDir()
	if ok, _ := CheckPluginIn(home); ok {
		t.Fatal("expected CheckPluginIn=false before plugin installation")
	}

	pluginDir := PluginPathIn(home)
	if err := os.MkdirAll(filepath.Join(pluginDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"agymate"}`), 0o644)
	_ = os.WriteFile(filepath.Join(pluginDir, "mcp_config.json"), []byte(`{"mcpServers":{"hub":{"command":"bash"}}}`), 0o644)
	_ = os.WriteFile(filepath.Join(pluginDir, "scripts", "run-mcp.sh"), []byte("#!/bin/sh\n"), 0o755)

	ok, detail := CheckPluginIn(home)
	if !ok {
		t.Fatalf("expected CheckPluginIn=true, got false (%s)", detail)
	}
}

func TestRenderSidecarOfflineHTML(t *testing.T) {
	htmlDoc := renderSidecarOfflineHTML("http://127.0.0.1:3778", errors.New("dial tcp 127.0.0.1:3778: connect: connection refused <tag>"))
	for _, want := range []string{
		"AgyMate Hub 尚未启动",
		"agymate service restart",
		"立即重试",
		"2500",
		"@media (prefers-color-scheme: light)",
		"/api/health",
		"&lt;tag&gt;",
	} {
		if !strings.Contains(htmlDoc, want) {
			t.Fatalf("expected offline sidecar HTML to contain %q", want)
		}
	}
}

func TestRunSidecarProxyForwardsAndInjectsHubToken(t *testing.T) {
	dir := t.TempDir()
	tokPath := filepath.Join(dir, "hub.token")
	t.Setenv("AGYMATE_HUB_TOKEN", tokPath)
	t.Setenv("AGYMATE_CONFIG", filepath.Join(dir, "config.json"))
	if err := os.WriteFile(tokPath, []byte("hub-secret-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	gotTokenCh := make(chan string, 4)
	mockHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokenCh <- r.Header.Get("X-AgyMate-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer mockHub.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()

	t.Setenv("ANTIGRAVITY_SIDECAR_WEB_PORT", port)
	t.Setenv("ANTIGRAVITY_SIDECAR_UI_TOKEN", "sidecar-secret")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunSidecarProxy(ctx, mockHub.URL)
	}()

	proxyURL := "http://127.0.0.1:" + port
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, gerr := http.Get(proxyURL + "/api/health")
		if gerr == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sidecar proxy did not start: %v", gerr)
		}
		time.Sleep(25 * time.Millisecond)
	}
	// Drain the GET /api/health token check (unauthenticated GET must NOT receive Hub token)
	if tok := <-gotTokenCh; tok != "" {
		t.Fatalf("expected unauthenticated GET /api/health not to receive X-AgyMate-Token, got %q", tok)
	}

	// 0. Authenticated GET with X-Sidecar-Token DOES inject Hub token
	getAuthReq, _ := http.NewRequest(http.MethodGet, proxyURL+"/api/config", nil)
	getAuthReq.Header.Set("X-Sidecar-Token", "sidecar-secret")
	getAuthResp, err := http.DefaultClient.Do(getAuthReq)
	if err != nil {
		t.Fatal(err)
	}
	getAuthResp.Body.Close()
	if tok := <-gotTokenCh; tok != "hub-secret-123" {
		t.Fatalf("expected authed GET /api/config to receive injected X-AgyMate-Token, got %q", tok)
	}

	// 1. Missing sidecar token must be rejected 401 before reaching Hub
	unauthReq, _ := http.NewRequest(http.MethodPost, proxyURL+"/api/cdp/sync", strings.NewReader(`{}`))
	unauthResp, err := http.DefaultClient.Do(unauthReq)
	if err != nil {
		t.Fatal(err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 when X-Sidecar-Token missing, got %d", unauthResp.StatusCode)
	}

	// 2. Valid X-Sidecar-Token causes sidecar proxy to inject X-AgyMate-Token if absent
	okReq, _ := http.NewRequest(http.MethodPost, proxyURL+"/api/cdp/sync", strings.NewReader(`{}`))
	okReq.Header.Set("X-Sidecar-Token", "sidecar-secret")
	okResp, err := http.DefaultClient.Do(okReq)
	if err != nil {
		t.Fatal(err)
	}
	okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with valid X-Sidecar-Token, got %d", okResp.StatusCode)
	}
	if tok := <-gotTokenCh; tok != "hub-secret-123" {
		t.Fatalf("expected injected X-AgyMate-Token=hub-secret-123, got %q", tok)
	}

	// 3. Explicit X-AgyMate-Token from frontend is preserved
	fwdReq, _ := http.NewRequest(http.MethodPost, proxyURL+"/api/cdp/sync", strings.NewReader(`{}`))
	fwdReq.Header.Set("X-Sidecar-Token", "sidecar-secret")
	fwdReq.Header.Set("X-AgyMate-Token", "frontend-token-456")
	fwdResp, err := http.DefaultClient.Do(fwdReq)
	if err != nil {
		t.Fatal(err)
	}
	fwdResp.Body.Close()
	if tok := <-gotTokenCh; tok != "frontend-token-456" {
		t.Fatalf("expected forwarded X-AgyMate-Token=frontend-token-456, got %q", tok)
	}

	cancel()
	if perr := <-errCh; perr != nil {
		t.Fatalf("RunSidecarProxy returned error on cancel: %v", perr)
	}
}
