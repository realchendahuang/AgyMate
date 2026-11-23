package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/realchendahuang/AgyMate/internal/colab"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
)

func isolateHubTestEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AGYMATE_STATE", filepath.Join(dir, "state.json"))
	t.Setenv("AGYMATE_COLAB_ACCOUNTS", filepath.Join(dir, "colab_accounts.json"))
	t.Setenv("AGYMATE_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("AGYMATE_HUB_TOKEN", filepath.Join(dir, "hub.token"))
}

func authedRequest(method, url string, body []byte, token string) (*http.Response, error) {
	var br io.Reader
	if body != nil {
		br = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, br)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-AgyMate-Token", token)
	}
	return http.DefaultClient.Do(req)
}

func TestHubProtectedMainAndIDELifecycleRoutes(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.Colab.Enabled = false
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
		r, err := authedRequest(http.MethodPost, ts.URL+p, []byte(`{"force":true}`), srv.AuthToken())
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
	r, err := authedRequest(http.MethodPost, ts.URL+"/api/hooks/ingest", []byte(hookReq), srv.AuthToken())
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

func TestHubColabRoutes(t *testing.T) {
	isolateHubTestEnv(t)
	tmp := t.TempDir()
	stateFile := filepath.Join(tmp, "colab-state")
	if err := os.WriteFile(stateFile, []byte("burning"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	mockCLI := filepath.Join(tmp, "colab-safe")
	script := `#!/bin/sh
STATE_FILE="` + stateFile + `"
MODE=$(cat "$STATE_FILE" 2>/dev/null || echo "idle")
CMD="$1"
if [ "$CMD" = "doctor" ]; then
  if [ "$MODE" = "burning" ]; then
    echo '{"ok":true,"status":"authenticated","account":"dev@example.com","currentBalance":2512.08,"consumptionRateHourly":1.71,"assignmentsCount":1,"eligibleGpus":["H100","G4","A100","L4","T4"],"eligibleTpus":["V6E1"],"sessions":[{"name":"?","orphan":true,"endpoint":"m-s-xyz","accelerator":"T4","variant":"GPU","shape":"STANDARD"}],"checkedAt":1794638000}'
    exit 0
  fi
  echo '{"ok":true,"status":"authenticated","account":"dev@example.com","currentBalance":2511.90,"consumptionRateHourly":0.0,"assignmentsCount":0,"eligibleGpus":["H100","G4","A100","L4","T4"],"eligibleTpus":["V6E1"],"sessions":[],"checkedAt":1794638020}'
  exit 0
fi
if [ "$CMD" = "nuke" ]; then
  echo "idle" > "$STATE_FILE"
  echo '{"ok":true,"terminatedCount":1,"terminated":[{"name":"?","orphan":true,"endpoint":"m-s-xyz","accelerator":"T4","variant":"GPU","shape":"STANDARD"}],"remainingCount":0,"failedCount":0,"currentBalance":2511.90,"consumptionRateHourly":0.0,"checkedAt":1794638020}'
  exit 0
fi
exit 2
`
	if err := os.WriteFile(mockCLI, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock cli: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.Colab.Enabled = true
	cfg.Colab.CLIPath = mockCLI
	cfg.Colab.PollIntervalSec = 60

	srv := NewServer(cfg, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>AgyMate</html>")}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// GET /api/colab?refresh=1 returns live mock Colab status
	resp, err := http.Get(ts.URL + "/api/colab?refresh=1")
	if err != nil {
		t.Fatalf("GET /api/colab failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on GET /api/colab, got %d", resp.StatusCode)
	}
	var snap struct {
		OK               bool    `json:"ok"`
		Account          string  `json:"account"`
		AssignmentsCount int     `json:"assignmentsCount"`
		OrphanCount      int     `json:"orphanCount"`
		CurrentBalance   float64 `json:"currentBalance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode colab snap: %v", err)
	}
	if !snap.OK || snap.Account != "dev@example.com" || snap.AssignmentsCount != 1 || snap.OrphanCount != 1 {
		t.Fatalf("unexpected colab snapshot: %+v", snap)
	}

	// GET /api/overview reflects default 0 (auto) subscription baseline and cached Colab status
	ovResp, err := http.Get(ts.URL + "/api/overview")
	if err != nil {
		t.Fatalf("GET /api/overview failed: %v", err)
	}
	defer ovResp.Body.Close()
	var ov OverviewPayload
	if err := json.NewDecoder(ovResp.Body).Decode(&ov); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if ov.Pricing.UltraMonthlyUSD != 0 {
		t.Fatalf("expected UltraMonthlyUSD=0 (auto), got %.1f", ov.Pricing.UltraMonthlyUSD)
	}
	if !ov.Colab.OK || ov.Colab.AssignmentsCount != 1 {
		t.Fatalf("expected overview colab assignments=1, got %+v", ov.Colab)
	}

	// POST /api/colab/nuke clears active session and updates overview snapshot
	nukeResp, err := authedRequest(http.MethodPost, ts.URL+"/api/colab/nuke", []byte("{}"), srv.AuthToken())
	if err != nil {
		t.Fatalf("POST /api/colab/nuke failed: %v", err)
	}
	defer nukeResp.Body.Close()
	if nukeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on POST /api/colab/nuke, got %d", nukeResp.StatusCode)
	}
	var nukeOut struct {
		OK   bool `json:"ok"`
		Nuke struct {
			TerminatedCount int `json:"terminatedCount"`
			RemainingCount  int `json:"remainingCount"`
		} `json:"nuke"`
		Colab struct {
			AssignmentsCount int     `json:"assignmentsCount"`
			CurrentBalance   float64 `json:"currentBalance"`
		} `json:"colab"`
	}
	if err := json.NewDecoder(nukeResp.Body).Decode(&nukeOut); err != nil {
		t.Fatalf("decode nuke out: %v", err)
	}
	if !nukeOut.OK || nukeOut.Nuke.TerminatedCount != 1 || nukeOut.Nuke.RemainingCount != 0 || nukeOut.Colab.AssignmentsCount != 0 {
		t.Fatalf("unexpected nuke response: %+v", nukeOut)
	}
}

func TestHubFrontendRootNoRedirect(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.Colab.Enabled = false
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html>AgyMate</html>")},
		"app.js":     &fstest.MapFile{Data: []byte("console.log(1);")},
	}
	srv := NewServer(cfg, fsys)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := noRedirect.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / expected 200 OK without redirect, got %d (Location=%q)", resp.StatusCode, resp.Header.Get("Location"))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET / expected text/html Content-Type, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("GET / expected Cache-Control=no-store, got %q", cc)
	}
	if !strings.Contains(string(body), "AgyMate") {
		t.Fatalf("GET / expected body to contain AgyMate, got %q", string(body))
	}

	sidecarResp, err := noRedirect.Get(ts.URL + "/?mode=sidecar")
	if err != nil {
		t.Fatalf("GET /?mode=sidecar failed: %v", err)
	}
	sidecarResp.Body.Close()
	if sidecarResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /?mode=sidecar expected 200 OK without redirect, got %d (Location=%q)", sidecarResp.StatusCode, sidecarResp.Header.Get("Location"))
	}

	jsResp, err := noRedirect.Get(ts.URL + "/app.js")
	if err != nil {
		t.Fatalf("GET /app.js failed: %v", err)
	}
	jsResp.Body.Close()
	if jsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /app.js expected 200 OK, got %d", jsResp.StatusCode)
	}

	apiResp, err := noRedirect.Get(ts.URL + "/api/does-not-exist")
	if err != nil {
		t.Fatalf("GET /api/does-not-exist failed: %v", err)
	}
	apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/does-not-exist expected 404 Not Found, got %d", apiResp.StatusCode)
	}
}

func TestHubSandboxedIframeCORSAndPreflight(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.Colab.Enabled = false
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html>AgyMate</html>")},
	}
	srv := NewServer(cfg, fsys)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{}

	// 1. Preflight OPTIONS with Origin: null (Magpie sandboxed iframe)
	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/config", nil)
	if err != nil {
		t.Fatalf("NewRequest OPTIONS failed: %v", err)
	}
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /api/config failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS expected 204 No Content, got %d", resp.StatusCode)
	}
	if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "null" {
		t.Fatalf("expected Access-Control-Allow-Origin=null, got %q", acao)
	}
	if acam := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(acam, "PUT") {
		t.Fatalf("expected Access-Control-Allow-Methods to include PUT, got %q", acam)
	}

	// 2. Actual GET with Origin: null
	getReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/overview", nil)
	if err != nil {
		t.Fatalf("NewRequest GET failed: %v", err)
	}
	getReq.Header.Set("Origin", "null")
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET /api/overview failed: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET expected 200 OK, got %d", getResp.StatusCode)
	}
	if acao := getResp.Header.Get("Access-Control-Allow-Origin"); acao != "null" {
		t.Fatalf("GET expected Access-Control-Allow-Origin=null, got %q", acao)
	}

	// 3. Evil external origin must remain blocked
	evilReq, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/config", nil)
	if err != nil {
		t.Fatalf("NewRequest evil failed: %v", err)
	}
	evilReq.Header.Set("Origin", "https://evil.example")
	evilResp, err := client.Do(evilReq)
	if err != nil {
		t.Fatalf("OPTIONS evil failed: %v", err)
	}
	evilResp.Body.Close()
	if evilResp.StatusCode != http.StatusForbidden {
		t.Fatalf("evil origin expected 403 Forbidden, got %d", evilResp.StatusCode)
	}
}

func TestHubProfileCreateAndDeleteRoutes(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.Colab.Enabled = false
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>AgyMate</html>")},
	}
	srv := NewServer(cfg, fsys)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, id := range []string{"main", "ide"} {
		r, err := authedRequest(http.MethodPost, ts.URL+"/api/instances/"+id+"/delete", []byte(`{"force":true}`), srv.AuthToken())
		if err != nil {
			t.Fatalf("POST /api/instances/%s/delete failed: %v", id, err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("delete %s expected 403 Forbidden, got %d", id, r.StatusCode)
		}
	}

	createBody := `{"name":"p-test","linksPolicy":"none","inheritFrom":"","inheritMcp":false}`
	cr, err := authedRequest(http.MethodPost, ts.URL+"/api/profiles/create", []byte(createBody), srv.AuthToken())
	if err != nil {
		t.Fatalf("POST /api/profiles/create failed: %v", err)
	}
	cr.Body.Close()
	if cr.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on profile create, got %d", cr.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(cfg.Paths.ProfilesDir, "p-test")); err != nil {
		t.Fatalf("expected created profile dir: %v", err)
	}

	dr, err := authedRequest(http.MethodPost, ts.URL+"/api/instances/p-test/delete", []byte(`{}`), srv.AuthToken())
	if err != nil {
		t.Fatalf("POST /api/instances/p-test/delete failed: %v", err)
	}
	dr.Body.Close()
	if dr.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on profile delete, got %d", dr.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(cfg.Paths.ProfilesDir, "p-test")); !os.IsNotExist(err) {
		t.Fatalf("expected profile dir to be removed after delete, got err=%v", err)
	}
}

func TestEffectiveMonthlySubUSD(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Pricing.UltraMonthlyUSD = 0

	sameEmailSnaps := map[string]rpc.InstanceSnapshot{
		"main": {Email: "Alice@Example.com", PlanName: "Google AI Ultra", TierID: "ultra"},
		"ide":  {Email: "alice@example.com", PlanName: "Google AI Ultra", TierID: "ultra"},
	}
	if got := effectiveMonthlySubUSD(cfg, sameEmailSnaps); got != 200 {
		t.Fatalf("expected deduplicated same-email ultra baseline 200, got %v", got)
	}

	twoProSnaps := map[string]rpc.InstanceSnapshot{
		"main": {Email: "a@example.com", PlanName: "Google AI Pro"},
		"work": {Email: "b@example.com", TierID: "pro"},
	}
	if got := effectiveMonthlySubUSD(cfg, twoProSnaps); got != 40 {
		t.Fatalf("expected two distinct pro accounts sum 40, got %v", got)
	}

	cfg.Pricing.UltraMonthlyUSD = 123
	if got := effectiveMonthlySubUSD(cfg, twoProSnaps); got != 123 {
		t.Fatalf("expected explicit Pricing.UltraMonthlyUSD override 123, got %v", got)
	}
}

func TestHubAccountGroupingOfflineQuotaAndColabOAuthRoutes(t *testing.T) {
	isolateHubTestEnv(t)

	var oauthMock *httptest.Server
	oauthMock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "hub-at-1",
				"refresh_token": "hub-rt-1",
				"expires_in":    3600,
			})
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"email": "colab2@gmail.com"})
		case "/tun/m/ccu-info":
			_, _ = w.Write([]byte(")]}'\n" + `{"currentBalance":750.0,"consumptionRateHourly":0,"assignmentsCount":0,"eligibleGpus":["L4"],"eligibleTpus":[]}`))
		case "/tun/m/assignments":
			_, _ = w.Write([]byte(")]}'\n" + `{"assignments":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer oauthMock.Close()

	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.Colab.Enabled = false
	cfg.Colab.CLIPath = filepath.Join(t.TempDir(), "nonexistent-cli")
	srv := NewServer(cfg, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>AgyMate</html>")}})
	srv.colabCollector.SetEndpoints(
		oauthMock.URL+"/auth",
		oauthMock.URL+"/token",
		oauthMock.URL+"/userinfo",
		oauthMock.URL,
		oauthMock.Client(),
	)

	// Seed PersistentState with cached offline quota for dev@gmail.com shared across main & ide.
	srv.state.RecordAccountSnapshot("main", config.CachedAccountState{
		Email:    "dev@gmail.com",
		PlanName: "Google AI Ultra",
		TierID:   "ultra",
		Buckets: []config.CachedBucket{
			{GroupName: "Claude", BucketID: "claude_5h", DisplayName: "Claude", Window: "5h", RemainingFraction: 0.8, RemainingPct: 80.0},
		},
		UpdatedAt: 1700000000,
	})
	srv.mu.Lock()
	srv.instList = []instances.Instance{
		{ID: "main", Label: "Antigravity", Account: "dev@gmail.com"},
		{ID: "ide", Label: "Antigravity IDE", Account: "DEV@gmail.com"},
		{ID: "p1", Label: "P1", Account: "alt@gmail.com"},
	}
	srv.instToday = map[string]telemetry.InstAggregate{
		"main": {InstanceID: "main", Calls: 2, Input: 100, Cache: 300, Total: 450, TotalUSD: 1.25},
		"ide":  {InstanceID: "ide", Calls: 1, Input: 100, Cache: 100, Total: 250, TotalUSD: 0.75},
	}
	srv.mu.Unlock()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Verify /api/overview groups main + ide into 1 dev@gmail.com AccountView with cached quota.
	ovResp, err := http.Get(ts.URL + "/api/overview")
	if err != nil {
		t.Fatalf("GET /api/overview: %v", err)
	}
	var ov OverviewPayload
	if err := json.NewDecoder(ovResp.Body).Decode(&ov); err != nil {
		ovResp.Body.Close()
		t.Fatalf("decode overview: %v", err)
	}
	ovResp.Body.Close()
	if len(ov.Accounts) != 2 {
		t.Fatalf("expected 2 grouped accounts, got %d: %+v", len(ov.Accounts), ov.Accounts)
	}
	if ov.Accounts[0].Key != "acct:dev@gmail.com" || ov.Accounts[0].TotalSlots != 2 || !ov.Accounts[0].CachedQuota {
		t.Fatalf("expected first account to be merged cached dev@gmail.com (2 slots), got %+v", ov.Accounts[0])
	}
	if len(ov.Accounts[0].Buckets) != 1 || ov.Accounts[0].Buckets[0].RemainingPct != 80 {
		t.Fatalf("expected hydrated 80%% cached quota bucket, got %+v", ov.Accounts[0].Buckets)
	}
	if ov.Accounts[0].TodayUSD != 2.0 || int(ov.Accounts[0].TodayCacheHitRate+0.5) != 67 {
		t.Fatalf("expected merged TodayUSD=2.0 and ~67%% cache hit rate, got usd=%v rate=%v", ov.Accounts[0].TodayUSD, ov.Accounts[0].TodayCacheHitRate)
	}

	// Offline cached plan must also contribute to effectiveMonthlySubUSDWithState.
	if sub := srv.effectiveMonthlySubUSD(cfg); sub != 200 {
		t.Fatalf("expected offline cached Ultra baseline 200, got %v", sub)
	}

	// Verify Colab OAuth login -> callback (including cross-origin browser redirect) -> probe -> remove.
	loginResp, err := authedRequest(http.MethodPost, ts.URL+"/api/colab/accounts/login", []byte(`{"loginHint":"colab2@gmail.com","openBrowser":false}`), srv.AuthToken())
	if err != nil {
		t.Fatalf("POST /api/colab/accounts/login: %v", err)
	}
	var loginOut struct {
		OK      bool   `json:"ok"`
		AuthURL string `json:"authUrl"`
		State   string `json:"state"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginOut); err != nil {
		loginResp.Body.Close()
		t.Fatalf("decode loginOut: %v", err)
	}
	loginResp.Body.Close()
	if !loginOut.OK || !strings.Contains(loginOut.AuthURL, "code_challenge_method=S256") || loginOut.State == "" {
		t.Fatalf("unexpected login response: %+v", loginOut)
	}

	cbReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/colab/oauth/callback?code=ok-code&state="+loginOut.State, nil)
	cbReq.Header.Set("Origin", "https://accounts.google.com")
	cbResp, err := http.DefaultClient.Do(cbReq)
	if err != nil {
		t.Fatalf("GET oauth callback: %v", err)
	}
	cbBody, _ := io.ReadAll(cbResp.Body)
	cbResp.Body.Close()
	if cbResp.StatusCode != http.StatusOK || !strings.Contains(string(cbBody), "colab2@gmail.com") {
		t.Fatalf("expected 200 HTML callback confirmation for colab2@gmail.com, got %d: %s", cbResp.StatusCode, string(cbBody))
	}

	colabResp, err := http.Get(ts.URL + "/api/colab?refresh=1")
	if err != nil {
		t.Fatalf("GET /api/colab?refresh=1: %v", err)
	}
	var colabOut struct {
		OK             bool    `json:"ok"`
		CurrentBalance float64 `json:"currentBalance"`
		Accounts       []struct {
			Account string  `json:"account"`
			Balance float64 `json:"currentBalance"`
		} `json:"accounts"`
	}
	_ = json.NewDecoder(colabResp.Body).Decode(&colabOut)
	colabResp.Body.Close()
	if !colabOut.OK || len(colabOut.Accounts) != 1 || colabOut.Accounts[0].Account != "colab2@gmail.com" || colabOut.Accounts[0].Balance != 750.0 {
		t.Fatalf("expected colab2@gmail.com (750.0 CCU) in colab snapshot, got %+v", colabOut)
	}

	rmResp, err := authedRequest(http.MethodPost, ts.URL+"/api/colab/accounts/remove", []byte(`{"email":"colab2@gmail.com"}`), srv.AuthToken())
	if err != nil {
		t.Fatalf("POST /api/colab/accounts/remove: %v", err)
	}
	var rmOut struct {
		OK      bool `json:"ok"`
		Removed bool `json:"removed"`
	}
	_ = json.NewDecoder(rmResp.Body).Decode(&rmOut)
	rmResp.Body.Close()
	if !rmOut.OK || !rmOut.Removed {
		t.Fatalf("expected removed=true from accounts/remove, got %+v", rmOut)
	}
	if !srv.state.IsColabHidden("colab2@gmail.com") {
		t.Fatalf("expected colab2@gmail.com marked hidden in state after remove")
	}
}

func TestBuildHUDDataChineseLocalizationAndModelSelection(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.Pricing.Currency = "CNY"
	cfg.Pricing.USDToCNY = 7.20
	srv := NewServer(cfg, nil)

	now := time.Now()
	resetSoon := now.Add(2*time.Hour + 15*time.Minute).Unix()
	resetWeek := now.Add(5*24*time.Hour + 6*time.Hour).Unix()

	inst := instances.Instance{
		ID:      "main",
		Label:   "Antigravity",
		Kind:    instances.KindMain,
		Running: true,
		Account: "dev@example.com",
	}
	snap := rpc.InstanceSnapshot{
		Email:    "dev@example.com",
		PlanName: "Google AI Ultra",
		TierID:   "g1-ultra-tier",
		Buckets: []rpc.QuotaBucket{
			{
				GroupName:           "Gemini Models",
				BucketID:            "gemini-weekly",
				DisplayName:         "Weekly Limit Remaining",
				Window:              "weekly",
				RemainingPct:        91.5,
				ResetUnix:           resetWeek,
				BurnPctPerHour:      5.0,
				HoursUntilEmpty:     18.3,
				ExhaustsBeforeReset: true,
			},
			{
				GroupName:           "Gemini Models",
				BucketID:            "gemini-5h",
				DisplayName:         "Five Hour Limit Remaining",
				Window:              "5h",
				RemainingPct:        74.0,
				ResetUnix:           resetSoon,
				BurnPctPerHour:      35.0,
				HoursUntilEmpty:     2.1,
				ExhaustsBeforeReset: true,
			},
			{
				GroupName:    "Claude and GPT models",
				BucketID:     "3p-5h",
				DisplayName:  "Five Hour Limit Remaining",
				Window:       "5h",
				RemainingPct: 12.0,
				ResetUnix:    resetSoon,
			},
		},
		Context: &rpc.ContextWindow{
			CascadeID:           "11111111-2222-3333-4444-555555555555",
			Title:               "打磨右上角中文浮窗",
			Model:               "Gemini 4 Carbon (High)",
			EstimatedTokensUsed: 84000,
			MaxContextTokens:    200000,
			UsagePct:            42.0,
			TokenBreakdown: []rpc.ContextGroup{
				{Name: "System Prompt", NumTokens: 12000},
				{Name: "Tools", NumTokens: 8000},
				{Name: "Mcp Tools", NumTokens: 2000},
				{Name: "Chat Messages", NumTokens: 50000},
				{Name: "Tool Results", NumTokens: 7000},
				{Name: "Thinking", NumTokens: 5000},
			},
		},
	}
	today := telemetry.InstAggregate{
		InstanceID: "main",
		Calls:      12,
		Total:      320000,
		CacheRate:  86.5,
		TotalUSD:   10.0,
	}
	live := []hooks.LiveSession{
		{
			ConversationID: "11111111-2222-3333-4444-555555555555",
			InstanceID:     "main",
			Title:          "打磨右上角中文浮窗",
			Model:          "Gemini 4 Carbon (High)",
			State:          "waiting_approval",
		},
	}
	colabSnap := colab.Snapshot{
		Enabled:               true,
		OK:                    true,
		Account:               "dev@example.com",
		AssignmentsCount:      1,
		ConsumptionRateHourly: 1.71,
		CurrentBalance:        1850,
	}

	hd := srv.buildHUDData(inst, snap, today, map[string]telemetry.InstAggregate{"main": today}, live, colabSnap, cfg, now)
	if hd.Position != "top-right" {
		t.Fatalf("expected Position=top-right, got %q", hd.Position)
	}
	if hd.Badge != "主窗口" || hd.PlanName != "Ultra 套餐" {
		t.Fatalf("expected localized Badge/PlanName, got badge=%q plan=%q", hd.Badge, hd.PlanName)
	}
	if hd.State != "waiting_approval" || hd.StateLabel != "待确认" {
		t.Fatalf("expected waiting_approval/待确认, got state=%q label=%q", hd.State, hd.StateLabel)
	}
	if hd.QuotaLabel != "5小时" || hd.QuotaPct != 74.0 {
		t.Fatalf("expected active Gemini model 5h bucket (5小时 74%%), got %q %.1f", hd.QuotaLabel, hd.QuotaPct)
	}
	if hd.TodayCostLabel != "¥72.00" {
		t.Fatalf("expected CNY cost label ¥72.00, got %q", hd.TodayCostLabel)
	}
	if hd.BurnHint == "" || !strings.Contains(hd.BurnHint, "耗尽") {
		t.Fatalf("expected Chinese BurnHint, got %q", hd.BurnHint)
	}
	wantGroups := []string{"系统提示词", "工具定义", "MCP 工具", "对话消息", "工具结果", "思考过程"}
	if len(hd.Breakdown) != len(wantGroups) {
		t.Fatalf("expected %d breakdown groups, got %+v", len(wantGroups), hd.Breakdown)
	}
	for i, want := range wantGroups {
		if hd.Breakdown[i].Name != want {
			t.Fatalf("breakdown[%d] = %q, want %q", i, hd.Breakdown[i].Name, want)
		}
	}

	// Empty buckets must yield QuotaPct = -1 instead of false 0%
	emptyHD := srv.buildHUDData(instances.Instance{ID: "p9", Running: true}, rpc.InstanceSnapshot{}, telemetry.InstAggregate{}, nil, nil, colab.Snapshot{}, cfg, now)
	if emptyHD.QuotaPct != -1 {
		t.Fatalf("expected QuotaPct=-1 when no buckets exist, got %v", emptyHD.QuotaPct)
	}
	if hd.InstanceID != "main" || hd.ActiveCascadeID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("expected active cascade metadata populated, got inst=%q cid=%q", hd.InstanceID, hd.ActiveCascadeID)
	}
	if hd.CurrencySymbol != "¥" {
		t.Fatalf("expected CurrencySymbol=¥, got %q", hd.CurrencySymbol)
	}
	if len(hd.CascadeContexts) == 0 || hd.CascadeContexts["11111111-2222-3333-4444-555555555555"].Used != 84000 {
		t.Fatalf("expected per-cascade context map populated, got %+v", hd.CascadeContexts)
	}
}

func TestConversationHeatmapAndQuotaHistoryRoutes(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.CDP.Enabled = false
	cfg.Colab.Enabled = false
	srv := NewServer(cfg, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>AgyMate</html>")}})

	now := time.Now()
	srv.state.RecordQuota("main:gemini-5h", 0.90, now.Add(3*time.Hour).Unix(), now.Add(-10*time.Minute))
	srv.state.RecordQuota("main:gemini-5h", 0.72, now.Add(3*time.Hour).Unix(), now)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. /api/quota/history
	qResp, err := http.Get(ts.URL + "/api/quota/history?prefix=main:")
	if err != nil {
		t.Fatalf("GET /api/quota/history failed: %v", err)
	}
	var qBody struct {
		OK     bool                           `json:"ok"`
		Series map[string][]config.QuotaPoint `json:"series"`
	}
	_ = json.NewDecoder(qResp.Body).Decode(&qBody)
	qResp.Body.Close()
	if !qBody.OK || len(qBody.Series["main:gemini-5h"]) != 2 {
		t.Fatalf("expected 2 quota trend points, got %+v", qBody)
	}

	// 2. /api/telemetry/heatmap
	hResp, err := http.Get(ts.URL + "/api/telemetry/heatmap?weeks=12")
	if err != nil {
		t.Fatalf("GET /api/telemetry/heatmap failed: %v", err)
	}
	var hBody struct {
		OK    bool                   `json:"ok"`
		Weeks int                    `json:"weeks"`
		Days  []telemetry.HeatmapDay `json:"days"`
	}
	_ = json.NewDecoder(hResp.Body).Decode(&hBody)
	hResp.Body.Close()
	if !hBody.OK || hBody.Weeks != 12 || len(hBody.Days) != 84 {
		t.Fatalf("unexpected heatmap response: ok=%v weeks=%d len=%d", hBody.OK, hBody.Weeks, len(hBody.Days))
	}

	// 3. /api/conversation
	cResp, err := http.Get(ts.URL + "/api/conversation?cascadeId=11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("GET /api/conversation failed: %v", err)
	}
	var cBody struct {
		OK        bool                         `json:"ok"`
		CascadeID string                       `json:"cascadeId"`
		Detail    telemetry.ConversationDetail `json:"detail"`
	}
	_ = json.NewDecoder(cResp.Body).Decode(&cBody)
	cResp.Body.Close()
	if !cBody.OK || cBody.CascadeID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected conversation response: %+v", cBody)
	}
}

func TestHubConfigSaveImmediateSyncAndCDPSyncRoute(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.CDP.Enabled = false
	cfg.Colab.Enabled = false
	srv := NewServer(cfg, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>AgyMate</html>")}})

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Subscribe to SSE channel internally to verify immediate broadcast on PUT /api/config
	subCh := make(chan string, 8)
	srv.sseMu.Lock()
	srv.subs[subCh] = struct{}{}
	srv.sseMu.Unlock()
	defer func() {
		srv.sseMu.Lock()
		delete(srv.subs, subCh)
		srv.sseMu.Unlock()
	}()

	putBody := `{"cdp":{"enabled":false,"windowHud":true,"hudPosition":"bogus-corner"},"pricing":{"mode":"promo","currency":"cny","usdToCny":7.25}}`
	putReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/config", bytes.NewReader([]byte(putBody)))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("Origin", "magpie://plugin")
	putReq.Header.Set("X-AgyMate-Token", srv.AuthToken())
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT /api/config failed: %v", err)
	}
	var savedCfg config.Config
	if err := json.NewDecoder(putResp.Body).Decode(&savedCfg); err != nil {
		putResp.Body.Close()
		t.Fatalf("decode savedCfg: %v", err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on PUT /api/config, got %d", putResp.StatusCode)
	}
	if acao := putResp.Header.Get("Access-Control-Allow-Origin"); acao != "magpie://plugin" {
		t.Fatalf("expected magpie://plugin CORS header, got %q", acao)
	}
	if savedCfg.CDP.HUDPosition != "top-right" || savedCfg.Pricing.Currency != "CNY" || savedCfg.Pricing.Mode != "promo" {
		t.Fatalf("expected normalized top-right/CNY/promo, got %+v / %+v", savedCfg.CDP, savedCfg.Pricing)
	}

	select {
	case ev := <-subCh:
		if ev != "overview" {
			t.Fatalf("expected immediate SSE overview broadcast, got %q", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE broadcast on PUT /api/config")
	}

	// Both /api/cdp/sync and /api/cdp/inject-now must respond 200 OK
	for _, ep := range []string{"/api/cdp/sync", "/api/cdp/inject-now"} {
		sr, err := authedRequest(http.MethodPost, ts.URL+ep, []byte("{}"), srv.AuthToken())
		if err != nil {
			t.Fatalf("POST %s failed: %v", ep, err)
		}
		sr.Body.Close()
		if sr.StatusCode != http.StatusOK {
			t.Fatalf("POST %s expected 200 OK, got %d", ep, sr.StatusCode)
		}
	}
}

func TestHubCSRFAndAuthTokenProtection(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = t.TempDir()
	cfg.Colab.Enabled = false
	cfg.Telegram.BotToken = "123456:SECRET_BOT_TOKEN_XYZ"
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><head><title>AgyMate</title></head><body>ok</body></html>")},
	}
	srv := NewServer(cfg, fsys)
	if srv.AuthToken() == "" {
		t.Fatal("expected non-empty AuthToken")
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. HTML index injects <meta name="agymate-token"> and NEVER sends ACAO: null for Origin: null
	htmlReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	htmlReq.Header.Set("Origin", "null")
	htmlResp, err := http.DefaultClient.Do(htmlReq)
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	htmlBytes, _ := io.ReadAll(htmlResp.Body)
	htmlResp.Body.Close()
	if htmlResp.StatusCode != http.StatusOK {
		t.Fatalf("GET / expected 200, got %d", htmlResp.StatusCode)
	}
	if acao := htmlResp.Header.Get("Access-Control-Allow-Origin"); acao != "" {
		t.Fatalf("GET / must not expose Access-Control-Allow-Origin for Origin: null, got %q", acao)
	}
	wantMeta := `<meta name="agymate-token" content="` + srv.AuthToken() + `">`
	if !strings.Contains(string(htmlBytes), wantMeta) {
		t.Fatalf("expected index.html to inject %q, got:\n%s", wantMeta, string(htmlBytes))
	}

	// 2. GET /api/health reports version + fullVersion + commit metadata
	hResp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health failed: %v", err)
	}
	var health map[string]any
	_ = json.NewDecoder(hResp.Body).Decode(&health)
	hResp.Body.Close()
	if health["ok"] != true || health["fullVersion"] == "" {
		t.Fatalf("unexpected /api/health payload: %+v", health)
	}

	// 3. Unauthenticated GET /api/config masks BotToken and refuses ACAO: null
	cfgReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/config", nil)
	cfgReq.Header.Set("Origin", "null")
	cfgResp, err := http.DefaultClient.Do(cfgReq)
	if err != nil {
		t.Fatalf("GET /api/config failed: %v", err)
	}
	var gotCfg config.Config
	_ = json.NewDecoder(cfgResp.Body).Decode(&gotCfg)
	cfgResp.Body.Close()
	if cfgResp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unauthenticated GET /api/config must not expose ACAO: null, got %q", cfgResp.Header.Get("Access-Control-Allow-Origin"))
	}
	if gotCfg.Telegram.BotToken == cfg.Telegram.BotToken || !strings.Contains(gotCfg.Telegram.BotToken, "...") {
		t.Fatalf("expected unauthenticated GET /api/config to mask BotToken, got %q", gotCfg.Telegram.BotToken)
	}

	// 4. All mutating endpoints MUST reject requests missing or mismatching X-AgyMate-Token (even with Origin: null or loopback Origin)
	mutating := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/instances/p1/delete", `{}`},
		{http.MethodPost, "/api/instances/p1/stop", `{}`},
		{http.MethodPost, "/api/instances/p1/launch", `{}`},
		{http.MethodPost, "/api/instances/p1/heal", `{}`},
		{http.MethodPost, "/api/instances/p1/focus", `{}`},
		{http.MethodPost, "/api/profiles/create", `{"name":"p-unauth"}`},
		{http.MethodPost, "/api/cache/clean", `{}`},
		{http.MethodPut, "/api/config", `{}`},
		{http.MethodPost, "/api/doctor/fix", `{}`},
		{http.MethodPost, "/api/colab/nuke", `{}`},
		{http.MethodPost, "/api/colab/accounts/login", `{}`},
		{http.MethodPost, "/api/colab/accounts/remove", `{"email":"a@b.com"}`},
		{http.MethodPost, "/api/notify/test", `{}`},
		{http.MethodPost, "/api/telemetry/save-csv", `{}`},
		{http.MethodPost, "/api/cdp/sync", `{}`},
		{http.MethodPost, "/api/cdp/inject-now", `{}`},
		{http.MethodPost, "/api/hooks/ingest", `{"hookEventName":"Stop"}`},
	}

	for _, tc := range mutating {
		for _, origin := range []string{"", "null", "http://127.0.0.1:9999", "magpie://plugin"} {
			req, _ := http.NewRequest(tc.method, ts.URL+tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s (origin=%q) failed: %v", tc.method, tc.path, origin, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s %s without token (origin=%q) expected 401, got %d", tc.method, tc.path, origin, resp.StatusCode)
			}
		}

		badReq, _ := http.NewRequest(tc.method, ts.URL+tc.path, bytes.NewReader([]byte(tc.body)))
		badReq.Header.Set("Content-Type", "application/json")
		badReq.Header.Set("X-AgyMate-Token", "wrong-token")
		badResp, err := http.DefaultClient.Do(badReq)
		if err != nil {
			t.Fatalf("%s %s with wrong token failed: %v", tc.method, tc.path, err)
		}
		badResp.Body.Close()
		if badResp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s with wrong token expected 401, got %d", tc.method, tc.path, badResp.StatusCode)
		}
	}

	// 5. Authorization: Bearer <token> also works on mutating routes
	bearerReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/cdp/sync", bytes.NewReader([]byte(`{}`)))
	bearerReq.Header.Set("Content-Type", "application/json")
	bearerReq.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	bearerResp, err := http.DefaultClient.Do(bearerReq)
	if err != nil {
		t.Fatalf("POST /api/cdp/sync with Bearer failed: %v", err)
	}
	bearerResp.Body.Close()
	if bearerResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/cdp/sync with Bearer expected 200, got %d", bearerResp.StatusCode)
	}

	// 6. Unauthenticated Origin: null must NOT receive ACAO: null on sensitive GET routes,
	//    while authenticated Origin: null (with X-AgyMate-Token) is permitted.
	for _, sensitivePath := range []string{
		"/api/config",
		"/api/sessions",
		"/api/telemetry",
		"/api/telemetry/export.csv",
		"/api/doctor",
		"/api/cache",
		"/api/colab",
	} {
		unauthGet, _ := http.NewRequest(http.MethodGet, ts.URL+sensitivePath, nil)
		unauthGet.Header.Set("Origin", "null")
		unauthResp, err := http.DefaultClient.Do(unauthGet)
		if err != nil {
			t.Fatalf("GET %s failed: %v", sensitivePath, err)
		}
		unauthResp.Body.Close()
		if acao := unauthResp.Header.Get("Access-Control-Allow-Origin"); acao != "" {
			t.Fatalf("unauthenticated GET %s with Origin: null must not expose ACAO, got %q", sensitivePath, acao)
		}

		authGet, _ := http.NewRequest(http.MethodGet, ts.URL+sensitivePath, nil)
		authGet.Header.Set("Origin", "null")
		authGet.Header.Set("X-AgyMate-Token", srv.AuthToken())
		authResp, err := http.DefaultClient.Do(authGet)
		if err != nil {
			t.Fatalf("authed GET %s failed: %v", sensitivePath, err)
		}
		authResp.Body.Close()
		if acao := authResp.Header.Get("Access-Control-Allow-Origin"); acao != "null" {
			t.Fatalf("authed GET %s with Origin: null expected ACAO=null, got %q", sensitivePath, acao)
		}
	}

	// 7. Short BotToken (<=8 bytes) must also be masked when unauthenticated
	srv.cfgMu.Lock()
	srv.cfg.Telegram.BotToken = "shorttok"
	srv.cfgMu.Unlock()
	shortCfgResp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config failed: %v", err)
	}
	var shortCfg config.Config
	_ = json.NewDecoder(shortCfgResp.Body).Decode(&shortCfg)
	shortCfgResp.Body.Close()
	if shortCfg.Telegram.BotToken != "..." {
		t.Fatalf("expected short BotToken masked to \"...\", got %q", shortCfg.Telegram.BotToken)
	}

	// 8. Fail-closed check: empty s.authToken must never authorize requests
	emptySrv := &Server{}
	testReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/cdp/sync", nil)
	if emptySrv.hasValidAuthToken(testReq) {
		t.Fatal("expected fail-closed hasValidAuthToken=false when server authToken is empty")
	}
}

func TestHubPartialConfigUpdate(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Original PollIntervalSec is 15.
	// Update ONLY PollIntervalSec to 10. ListenAddr should remain 127.0.0.1:3778.
	body := `{"pollIntervalSec": 10}`
	r, err := authedRequest(http.MethodPut, ts.URL+"/api/config", []byte(body), srv.AuthToken())
	if err != nil {
		t.Fatalf("PUT /api/config failed: %v", err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}

	// Check config
	updatedCfg := srv.Config()
	if updatedCfg.PollIntervalSec != 10 {
		t.Fatalf("expected PollIntervalSec=10, got %d", updatedCfg.PollIntervalSec)
	}
	if updatedCfg.ListenAddr != "127.0.0.1:3778" {
		t.Fatalf("expected ListenAddr=127.0.0.1:3778, got %q", updatedCfg.ListenAddr)
	}
}

func TestHubConfigPutMasksSecretsAndPreservesMaskedValues(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.CDP.Enabled = false
	cfg.Colab.Enabled = false
	cfg.Telegram.BotToken = "999999:SUPER_SECRET_BOT_TOKEN"
	cfg.Webhook.Secret = "whsec_super_secret_123456"
	srv := NewServer(cfg, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. PUT with masked placeholder values must preserve original secrets and return masked response
	body := `{"pollIntervalSec":12,"telegram":{"enabled":true,"botToken":"9999...OKEN","chatId":"42"},"webhook":{"enabled":true,"url":"https://example.com/hook","format":"auto","secret":"whse...3456"}}`
	resp, err := authedRequest(http.MethodPut, ts.URL+"/api/config", []byte(body), srv.AuthToken())
	if err != nil {
		t.Fatalf("PUT /api/config failed: %v", err)
	}
	var returned config.Config
	if err := json.NewDecoder(resp.Body).Decode(&returned); err != nil {
		resp.Body.Close()
		t.Fatalf("decode response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if returned.Telegram.BotToken == cfg.Telegram.BotToken || !strings.Contains(returned.Telegram.BotToken, "...") {
		t.Fatalf("PUT /api/config response must mask BotToken, got %q", returned.Telegram.BotToken)
	}
	if returned.Webhook.Secret == cfg.Webhook.Secret || !strings.Contains(returned.Webhook.Secret, "...") {
		t.Fatalf("PUT /api/config response must mask Webhook.Secret, got %q", returned.Webhook.Secret)
	}

	stored := srv.Config()
	if stored.Telegram.BotToken != cfg.Telegram.BotToken {
		t.Fatalf("expected original BotToken preserved, got %q", stored.Telegram.BotToken)
	}
	if stored.Webhook.Secret != cfg.Webhook.Secret {
		t.Fatalf("expected original Webhook.Secret preserved, got %q", stored.Webhook.Secret)
	}
}

func TestTelemetryCSVContentDispositionSanitized(t *testing.T) {
	isolateHubTestEnv(t)
	cfg := config.DefaultConfig()
	cfg.CDP.Enabled = false
	cfg.Colab.Enabled = false
	cfg.Paths.ProfilesDir = t.TempDir()
	srv := NewServer(cfg, nil)
	srv.instList = []instances.Instance{{ID: "main", Kind: instances.KindMain}}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + `/api/telemetry/export.csv?preset=today%22%3Bevil%3D1`)
	if err != nil {
		t.Fatalf("GET export.csv failed: %v", err)
	}
	defer resp.Body.Close()
	cd := resp.Header.Get("Content-Disposition")
	if strings.Contains(cd, ";evil=") || !strings.HasPrefix(cd, `attachment; filename="agymate-telemetry-today`) {
		t.Fatalf("unexpected Content-Disposition header: %q", cd)
	}
}

func TestFrontendOpaqueOriginSandboxSafety(t *testing.T) {
	appPath := filepath.Join("..", "..", "frontend", "app.js")
	appBytes, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read frontend/app.js: %v", err)
	}
	appSrc := string(appBytes)
	// Ensure frontend/app.js never evaluates bare localStorage/sessionStorage outside getLocalStorage().
	if strings.Contains(appSrc, "safeStorageGet(localStorage") ||
		strings.Contains(appSrc, "safeStorageSet(localStorage") ||
		strings.Contains(appSrc, "safeStorageRemove(localStorage") ||
		strings.Contains(appSrc, "sessionStorage") {
		t.Fatal("frontend/app.js contains unsafe direct storage reference that throws SecurityError in sandbox='allow-scripts' iframes")
	}

	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping opaque-origin sandbox JS test")
	}
	script := `
const fs = require('fs');
const vm = require('vm');
const assert = require('assert');

// 1. Verify frontend/i18n.js has zero duplicate keys in EXACT and boots cleanly when localStorage throws SecurityError.
const i18nSrc = fs.readFileSync('../../frontend/i18n.js', 'utf8');
const exactBlock = i18nSrc.slice(i18nSrc.indexOf('const EXACT = {'), i18nSrc.indexOf('const PATTERNS = ['));
const keyRe = /^\s*'([^']+)'\s*:/gm;
const seen = new Set();
let m;
while ((m = keyRe.exec(exactBlock)) !== null) {
  assert.ok(!seen.has(m[1]), 'duplicate EXACT i18n key: ' + m[1]);
  seen.add(m[1]);
}

const secErr = () => {
  const e = new Error("Failed to read the 'localStorage' property from 'Window': The document is sandboxed and lacks the 'allow-same-origin' flag.");
  e.name = 'SecurityError';
  throw e;
};
const sandbox = {
  location: { search: '' },
  navigator: { language: 'en-US' },
  URLSearchParams,
  CustomEvent: class CustomEvent { constructor(t, o) { this.type = t; this.detail = o?.detail; } },
  NodeFilter: { SHOW_ELEMENT: 1, SHOW_TEXT: 4 },
  document: {
    readyState: 'complete',
    documentElement: { lang: 'zh-CN' },
    title: '',
    body: null,
    getElementById: () => null,
    addEventListener: () => {},
  },
  MutationObserver: class { observe() {} },
  queueMicrotask,
  window: null,
};
sandbox.window = sandbox;
sandbox.window.dispatchEvent = () => {};
Object.defineProperty(sandbox, 'localStorage', { get: secErr, enumerable: true });
Object.defineProperty(sandbox, 'sessionStorage', { get: secErr, enumerable: true });

vm.runInNewContext(i18nSrc, sandbox, { filename: 'frontend/i18n.js' });
assert.ok(sandbox.window.AgyI18n, 'AgyI18n should initialize under opaque origin');
assert.strictEqual(sandbox.window.AgyI18n.toggle(), 'zh');
assert.strictEqual(sandbox.window.AgyI18n.toggle(), 'en');
assert.strictEqual(sandbox.window.AgyI18n.t('未开 CDP'), 'No CDP');
`
	cmd := exec.Command(nodeBin, "-e", script)
	if out, cerr := cmd.CombinedOutput(); cerr != nil {
		t.Fatalf("opaque-origin sandbox test failed: %v\n%s", cerr, string(out))
	}
}
