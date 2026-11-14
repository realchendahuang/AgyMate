package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestHubProtectedMainAndIDELifecycleRoutes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Box.ProfilesDir = t.TempDir()
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>AgyMate</html>")},
	}
	srv := NewServer(cfg, fsys)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Health check works on loopback
	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /api/health, got %d", resp.StatusCode)
	}

	// Protected routes MUST return 403 for main and ide
	protectedPaths := []string{
		"/api/instances/main/stop",
		"/api/instances/main/launch",
		"/api/instances/main/heal",
		"/api/instances/ide/stop",
		"/api/instances/ide/launch",
		"/api/instances/ide/heal",
	}
	for _, p := range protectedPaths {
		r, err := http.Post(ts.URL+p, "application/json", bytes.NewReader([]byte(`{"force":true}`)))
		if err != nil {
			t.Fatalf("POST %s failed: %v", p, err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("POST %s expected HTTP 403 Forbidden, got %d", p, r.StatusCode)
		}
	}

	// Hooks ingest works quickly
	hookReq := `{"hookEventName":"SessionStart","conversationId":"11111111-1111-1111-1111-111111111111","instanceId":"p1","cwd":"/tmp/demo"}`
	r, err := http.Post(ts.URL+"/api/hooks/ingest", "application/json", bytes.NewReader([]byte(hookReq)))
	if err != nil {
		t.Fatalf("POST /api/hooks/ingest failed: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on hooks ingest, got %d", r.StatusCode)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil || !out.OK {
		t.Fatalf("expected ok:true from hooks ingest, got %+v (%v)", out, err)
	}
}
