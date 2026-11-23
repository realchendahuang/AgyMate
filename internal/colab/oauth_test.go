package colab

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestMultiAccountNativeOAuthProbeAndNuke(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "colab_accounts.json")
	t.Setenv("AGYMATE_COLAB_ACCOUNTS", accountsPath)

	var mu sync.Mutex
	refreshCalls := 0
	alphaSessions := []map[string]any{
		{
			"accelerator":  "A100",
			"endpoint":     "m-alpha-1",
			"variant":      "GPU",
			"machineShape": "HIGHMEM",
		},
	}
	betaSessions := []map[string]any{
		{
			"accelerator":  "T4",
			"endpoint":     "m-beta-1",
			"variant":      "GPU",
			"machineShape": "STANDARD",
		},
	}
	unassigned := make([]string, 0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			grant := r.Form.Get("grant_type")
			w.Header().Set("Content-Type", "application/json")
			switch grant {
			case "authorization_code":
				code := r.Form.Get("code")
				if strings.TrimSpace(r.Form.Get("code_verifier")) == "" {
					http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
					return
				}
				switch code {
				case "code-alpha":
					// Short expires_in (10s <= 60s) forces refresh_token on first probe.
					_ = json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "at-alpha-init",
						"refresh_token": "rt-alpha",
						"expires_in":    10,
					})
				case "code-beta":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "at-beta",
						"refresh_token": "rt-beta",
						"expires_in":    3600,
					})
				default:
					http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				}
			case "refresh_token":
				if r.Form.Get("refresh_token") != "rt-alpha" {
					http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
					return
				}
				mu.Lock()
				refreshCalls++
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "at-alpha-fresh",
					"expires_in":   3600,
				})
			default:
				http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			}

		case r.URL.Path == "/userinfo":
			auth := r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			switch auth {
			case "Bearer at-alpha-init", "Bearer at-alpha-fresh":
				_ = json.NewEncoder(w).Encode(map[string]string{"email": "alpha@gmail.com"})
			case "Bearer at-beta":
				_ = json.NewEncoder(w).Encode(map[string]string{"email": "beta@gmail.com"})
			default:
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			}

		case r.URL.Path == "/tun/m/ccu-info":
			auth := r.Header.Get("Authorization")
			mu.Lock()
			defer mu.Unlock()
			var payload map[string]any
			switch auth {
			case "Bearer at-alpha-fresh":
				burn := 0.0
				if len(alphaSessions) > 0 {
					burn = 12.5
				}
				payload = map[string]any{
					"currentBalance":        1200.0,
					"consumptionRateHourly": burn,
					"assignmentsCount":      len(alphaSessions),
					"eligibleGpus":          []string{"A100", "L4"},
					"eligibleTpus":          []string{"V2_8"},
				}
			case "Bearer at-beta":
				payload = map[string]any{
					"currentBalance":        400.0,
					"consumptionRateHourly": 2.5,
					"assignmentsCount":      len(betaSessions),
					"eligibleGpus":          []string{"T4"},
					"eligibleTpus":          []string{},
				}
			default:
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			b, _ := json.Marshal(payload)
			_, _ = w.Write([]byte(")]}'\n" + string(b)))

		case r.URL.Path == "/tun/m/assignments":
			auth := r.Header.Get("Authorization")
			mu.Lock()
			defer mu.Unlock()
			var list []map[string]any
			switch auth {
			case "Bearer at-alpha-fresh":
				list = alphaSessions
			case "Bearer at-beta":
				list = betaSessions
			default:
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			b, _ := json.Marshal(map[string]any{"assignments": list})
			_, _ = w.Write([]byte(")]}'\n" + string(b)))

		case strings.HasPrefix(r.URL.Path, "/tun/m/unassign/"):
			ep := strings.TrimPrefix(r.URL.Path, "/tun/m/unassign/")
			auth := r.Header.Get("Authorization")
			if auth != "Bearer at-alpha-fresh" {
				http.Error(w, "unexpected account nuke", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodGet {
				b, _ := json.Marshal(map[string]string{"token": "xsrf-" + ep})
				_, _ = w.Write([]byte(")]}'\n" + string(b)))
				return
			}
			if r.Method == http.MethodPost {
				if r.Header.Get("X-Goog-Colab-Token") != "xsrf-"+ep {
					http.Error(w, "bad xsrf", http.StatusForbidden)
					return
				}
				mu.Lock()
				unassigned = append(unassigned, ep)
				kept := make([]map[string]any, 0, len(alphaSessions))
				for _, s := range alphaSessions {
					if s["endpoint"] != ep {
						kept = append(kept, s)
					}
				}
				alphaSessions = kept
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(")]}'\n{}"))
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewCollector()
	c.authURL = srv.URL + "/auth"
	c.tokenURL = srv.URL + "/token"
	c.userInfoURL = srv.URL + "/userinfo"
	c.colabBaseURL = srv.URL
	c.httpClient = srv.Client()
	c.storePathOverride = accountsPath
	fixedNow := time.Unix(1700000000, 0)
	c.nowFn = func() time.Time { return fixedNow }

	// Bind two distinct Google accounts via PKCE loopback flow.
	authA, stateA, err := c.StartOAuthFlow("127.0.0.1:3778", "alpha@gmail.com")
	if err != nil {
		t.Fatalf("StartOAuthFlow alpha: %v", err)
	}
	parsedA, err := url.Parse(authA)
	if err != nil {
		t.Fatalf("parse authA: %v", err)
	}
	if parsedA.Query().Get("code_challenge_method") != "S256" || parsedA.Query().Get("login_hint") != "alpha@gmail.com" {
		t.Fatalf("unexpected PKCE params: %s", authA)
	}
	accA, err := c.CompleteOAuthFlow(context.Background(), "code-alpha", stateA)
	if err != nil {
		t.Fatalf("CompleteOAuthFlow alpha: %v", err)
	}
	if accA.Email != "alpha@gmail.com" {
		t.Fatalf("expected alpha@gmail.com, got %q", accA.Email)
	}

	_, stateB, err := c.StartOAuthFlow("127.0.0.1:3778", "")
	if err != nil {
		t.Fatalf("StartOAuthFlow beta: %v", err)
	}
	if _, err := c.CompleteOAuthFlow(context.Background(), "code-beta", stateB); err != nil {
		t.Fatalf("CompleteOAuthFlow beta: %v", err)
	}

	cfg := config.DefaultConfig().Colab
	cfg.Enabled = true
	cfg.CLIPath = filepath.Join(dir, "nonexistent-colab-safe")

	snap := c.Probe(context.Background(), cfg, true)
	if !snap.OK || snap.Status != "authenticated" {
		t.Fatalf("expected OK/authenticated snapshot, got ok=%v status=%s err=%s", snap.OK, snap.Status, snap.LastError)
	}
	if math.Abs(snap.CurrentBalance-1600.0) > 1e-6 {
		t.Fatalf("expected aggregated balance 1600.0, got %v", snap.CurrentBalance)
	}
	if math.Abs(snap.ConsumptionRateHourly-15.0) > 1e-6 {
		t.Fatalf("expected aggregated burn rate 15.0, got %v", snap.ConsumptionRateHourly)
	}
	if snap.AssignmentsCount != 2 || len(snap.Sessions) != 2 {
		t.Fatalf("expected 2 total sessions, got count=%d len=%d", snap.AssignmentsCount, len(snap.Sessions))
	}
	if len(snap.Accounts) != 2 {
		t.Fatalf("expected 2 per-account cards, got %d", len(snap.Accounts))
	}
	mu.Lock()
	rc := refreshCalls
	mu.Unlock()
	if rc != 1 {
		t.Fatalf("expected 1 refresh_token exchange for expiring alpha token, got %d", rc)
	}

	// Targeted single-account nuke: terminate only alpha's VM, keep beta's VM intact.
	res, updatedSnap, err := c.NukeAccount(context.Background(), cfg, "alpha@gmail.com")
	if err != nil {
		t.Fatalf("NukeAccount alpha: %v", err)
	}
	if !res.OK || res.TerminatedCount != 1 {
		t.Fatalf("expected 1 terminated VM for alpha, got %+v", res)
	}
	if updatedSnap.AssignmentsCount != 1 || len(updatedSnap.Sessions) != 1 || updatedSnap.Sessions[0].Account != "beta@gmail.com" {
		t.Fatalf("expected beta session to remain untouched, got %+v", updatedSnap.Sessions)
	}
	mu.Lock()
	gotUnassigned := append([]string(nil), unassigned...)
	mu.Unlock()
	if len(gotUnassigned) != 1 || gotUnassigned[0] != "m-alpha-1" {
		t.Fatalf("expected m-alpha-1 unassigned, got %v", gotUnassigned)
	}

	// Removing alpha leaves only beta in Accounts.
	removed, err := c.RemoveAccount("alpha@gmail.com")
	if err != nil || !removed {
		t.Fatalf("RemoveAccount alpha failed: removed=%v err=%v", removed, err)
	}
	snapAfterRemove := c.Probe(context.Background(), cfg, true)
	if len(snapAfterRemove.Accounts) != 1 || snapAfterRemove.Accounts[0].Account != "beta@gmail.com" {
		t.Fatalf("expected only beta remaining, got %+v", snapAfterRemove.Accounts)
	}
}
