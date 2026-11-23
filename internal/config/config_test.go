package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultConfigDeterministic(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.CDP.ZhInject {
		t.Fatalf("expected DefaultConfig CDP.ZhInject false, got true")
	}
	if cfg.Colab.Enabled {
		t.Fatalf("expected DefaultConfig Colab.Enabled false, got true")
	}
	if cfg.Pricing.UltraMonthlyUSD != 0 {
		t.Fatalf("expected DefaultConfig UltraMonthlyUSD 0 (auto), got %v", cfg.Pricing.UltraMonthlyUSD)
	}
	if len(cfg.CDP.HUDInstances) != 0 {
		t.Fatalf("expected empty HUDInstances (all instances), got %v", cfg.CDP.HUDInstances)
	}
	if cfg.CDP.HUDPosition != "top-right" {
		t.Fatalf("expected DefaultConfig CDP.HUDPosition=top-right, got %q", cfg.CDP.HUDPosition)
	}
	if cfg.Paths.MainAppPath == "" {
		t.Fatalf("expected non-empty MainAppPath for %s", runtime.GOOS)
	}
	if cfg.Paths.ProfilesDir == "" {
		t.Fatalf("expected non-empty ProfilesDir")
	}
}

func TestHUDPositionValidationFallbackToTopRight(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, ".config", "agymate", "config.json")
	t.Setenv("AGYMATE_CONFIG", cfgPath)

	for _, invalid := range []string{"", "center", "middle-top", "unknown"} {
		if got := NormalizeHUDPosition(invalid); got != "top-right" {
			t.Fatalf("NormalizeHUDPosition(%q) = %q, want top-right", invalid, got)
		}
	}
	for _, valid := range []string{"top-right", "top-left", "bottom-right", "bottom-left"} {
		if got := NormalizeHUDPosition("  " + strings.ToUpper(valid) + " "); got != valid {
			t.Fatalf("NormalizeHUDPosition(%q) = %q, want %q", valid, got, valid)
		}
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"cdp":{"hudPosition":"invalid-corner"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load invalid hudPosition failed: %v", err)
	}
	if loaded.CDP.HUDPosition != "top-right" {
		t.Fatalf("expected invalid hudPosition to fall back to top-right, got %q", loaded.CDP.HUDPosition)
	}
}

func TestLoadAutoDetectsFirstRunOptionalFeatures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGYMATE_CONFIG", filepath.Join(home, ".config", "agymate", "config.json"))
	t.Setenv("PATH", t.TempDir())

	// Case 1: clean environment without optional integrations.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load clean failed: %v", err)
	}
	if cfg.CDP.ZhInject {
		t.Fatalf("expected ZhInject false when inject.js absent")
	}
	if cfg.Colab.Enabled {
		t.Fatalf("expected Colab.Enabled false when colab-safe absent")
	}

	// Case 2: optional inject.js and colab-safe installed, no config.json yet.
	zhPath := filepath.Join(home, ".antigravity-zh", "inject.js")
	if err := os.MkdirAll(filepath.Dir(zhPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zhPath, []byte("// zh"), 0o644); err != nil {
		t.Fatal(err)
	}
	colabPath := filepath.Join(home, ".local", "bin", "colab-safe")
	if err := os.MkdirAll(filepath.Dir(colabPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(colabPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load with optional integrations failed: %v", err)
	}
	if !cfg.CDP.ZhInject {
		t.Fatalf("expected ZhInject auto-enabled when %s exists", zhPath)
	}
	if !cfg.Colab.Enabled {
		t.Fatalf("expected Colab.Enabled auto-enabled when %s exists", colabPath)
	}

	// Case 3: explicit config.json overrides environment detection and preserves 0/empty values.
	cfg.CDP.ZhInject = false
	cfg.Colab.Enabled = false
	cfg.Pricing.UltraMonthlyUSD = 0
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load saved failed: %v", err)
	}
	if loaded.CDP.ZhInject || loaded.Colab.Enabled {
		t.Fatalf("expected explicit disabled flags preserved, got zh=%v colab=%v", loaded.CDP.ZhInject, loaded.Colab.Enabled)
	}
	if loaded.Pricing.UltraMonthlyUSD != 0 {
		t.Fatalf("expected explicit 0 subscription baseline preserved, got %v", loaded.Pricing.UltraMonthlyUSD)
	}
}

func TestLegacyBoxConfigMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, ".config", "agymate", "config.json")
	t.Setenv("AGYMATE_CONFIG", cfgPath)

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyJSON := `{
  "box": {
    "mainAppPath": "/custom/Antigravity.app",
    "ideAppPath": "/custom/Antigravity IDE.app",
    "boxAppPath": "/Applications/Antigravity-Box.app",
    "updateScriptPath": "~/legacy/update-box.sh",
    "profilesDir": "~/custom-profiles"
  }
}`
	if err := os.WriteFile(cfgPath, []byte(legacyJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load legacy failed: %v", err)
	}
	if loaded.Paths.MainAppPath != "/custom/Antigravity.app" ||
		loaded.Paths.IDEAppPath != "/custom/Antigravity IDE.app" ||
		loaded.Paths.ProfilesDir != "~/custom-profiles" {
		t.Fatalf("expected legacy box paths migrated to Paths, got %+v", loaded.Paths)
	}

	if err := Save(loaded); err != nil {
		t.Fatalf("Save migrated config failed: %v", err)
	}
	savedRaw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(savedRaw), `"box"`) || strings.Contains(string(savedRaw), "boxAppPath") {
		t.Fatalf("expected Save() to emit only paths without box keys, got:\n%s", string(savedRaw))
	}
}

func TestPersistentStateAccountCacheAndRollover(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := LoadState(statePath)

	futureReset := time.Unix(2000000000, 0)
	pastReset := time.Unix(1700000000, 0)
	st.RecordAccountSnapshot("main", CachedAccountState{
		Email:     "User@Example.com",
		UserName:  "User",
		PlanName:  "Google AI Ultra",
		TierID:    "g1-ultra-tier",
		UpdatedAt: 1750000000,
		Buckets: []CachedBucket{
			{
				GroupName:         "Gemini Models",
				BucketID:          "gemini-5h",
				DisplayName:       "Five Hour Limit Remaining",
				Window:            "5h",
				RemainingFraction: 0.42,
				RemainingPct:      42.0,
				ResetTime:         pastReset.UTC().Format(time.RFC3339),
				ResetUnix:         pastReset.Unix(),
			},
			{
				GroupName:         "Gemini Models",
				BucketID:          "gemini-weekly",
				DisplayName:       "Weekly Limit Remaining",
				Window:            "weekly",
				RemainingFraction: 0.75,
				RemainingPct:      75.0,
				ResetTime:         futureReset.UTC().Format(time.RFC3339),
				ResetUnix:         futureReset.Unix(),
			},
		},
	})
	st.SetColabHidden("Old@Example.com", true)
	if err := st.Save(); err != nil {
		t.Fatalf("Save state failed: %v", err)
	}

	reloaded := LoadState(statePath)
	if got := reloaded.LastInstanceAccount("main"); got != "User@Example.com" {
		t.Fatalf("expected LastInstanceAccount=User@Example.com, got %q", got)
	}
	if !reloaded.IsColabHidden("old@example.com") {
		t.Fatalf("expected IsColabHidden case-insensitive match")
	}

	now := time.Unix(1790000000, 0)
	snap, ok := reloaded.GetAccountSnapshot("user@example.com", now)
	if !ok {
		t.Fatalf("expected cached snapshot for user@example.com")
	}
	if snap.PlanName != "Google AI Ultra" || len(snap.Buckets) != 2 {
		t.Fatalf("unexpected cached snapshot: %+v", snap)
	}
	if snap.Buckets[0].RemainingPct != 100.0 || snap.Buckets[0].RemainingFraction != 1.0 || snap.Buckets[0].ResetUnix != 0 {
		t.Fatalf("expected expired 5h bucket rolled over to 100%%, got %+v", snap.Buckets[0])
	}
	if snap.Buckets[1].RemainingPct != 75.0 || snap.Buckets[1].ResetUnix != futureReset.Unix() {
		t.Fatalf("expected future weekly bucket untouched, got %+v", snap.Buckets[1])
	}
}

func TestLoadOrCreateHubToken(t *testing.T) {
	dir := t.TempDir()
	tokPath := filepath.Join(dir, "hub.token")
	t.Setenv("AGYMATE_HUB_TOKEN", tokPath)

	if got := LoadHubToken(); got != "" {
		t.Fatalf("expected empty token before creation, got %q", got)
	}
	tok1, err := LoadOrCreateHubToken()
	if err != nil {
		t.Fatalf("LoadOrCreateHubToken failed: %v", err)
	}
	if len(tok1) < 32 {
		t.Fatalf("expected strong hex token, got len=%d", len(tok1))
	}
	st, err := os.Stat(tokPath)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 mode on hub.token, got %o", st.Mode().Perm())
	}
	tok2, err := LoadOrCreateHubToken()
	if err != nil || tok2 != tok1 {
		t.Fatalf("expected idempotent token %q, got %q (%v)", tok1, tok2, err)
	}
	if got := LoadHubToken(); got != tok1 {
		t.Fatalf("LoadHubToken mismatch: got %q want %q", got, tok1)
	}
}

func TestPersistentStateConcurrentSaveAndMutate(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	st := LoadState(statePath)

	var wg sync.WaitGroup
	base := time.Now()

	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				now := base.Add(time.Duration(i*15) * time.Second)
				key := fmt.Sprintf("inst%d:gemini-5h", worker)
				st.RecordQuota(key, 1.0-float64(i%10)*0.05, now.Add(4*time.Hour).Unix(), now)
				_ = st.EstimateRunway(key, 0.85, now.Add(4*time.Hour).Unix(), now)
				st.RecordAccountSnapshot(fmt.Sprintf("inst%d", worker), CachedAccountState{
					Email:     fmt.Sprintf("user%d@example.com", worker),
					PlanName:  "Google AI Ultra",
					UpdatedAt: now.Unix(),
					Buckets: []CachedBucket{
						{BucketID: "gemini-5h", RemainingFraction: 0.85, RemainingPct: 85.0},
					},
				})
				st.SetColabHidden(fmt.Sprintf("hidden%d@example.com", worker), i%2 == 0)
				_ = st.ShouldNotifyLowQuota(key, int64(i/10+1), now)
			}
		}(g)
	}

	for s := 0; s < 4; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				if err := st.Save(); err != nil {
					t.Errorf("concurrent Save failed: %v", err)
				}
				_ = st.SaveIfDirty()
			}
		}()
	}

	wg.Wait()
	if err := st.Save(); err != nil {
		t.Fatalf("final Save failed: %v", err)
	}

	reloaded := LoadState(statePath)
	if len(reloaded.QuotaHistory) != 6 || len(reloaded.AccountCache) != 6 {
		t.Fatalf("unexpected reloaded counts: quota=%d accounts=%d", len(reloaded.QuotaHistory), len(reloaded.AccountCache))
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range ents {
		if strings.HasSuffix(ent.Name(), ".tmp") {
			t.Fatalf("found leftover temp file %s", ent.Name())
		}
	}
}

func TestRecordQuotaDownsamplingCompactionAndRunway(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	st := LoadState(statePath)

	now := time.Unix(1790000000, 0)
	resetTS := now.Add(5 * time.Hour).Unix()

	// Seed legacy bloated 14-day idle history (2500 points) + an expired >14d point.
	bloated := make([]QuotaPoint, 0, 2505)
	bloated = append(bloated, QuotaPoint{TS: now.Unix() - 15*86400, Remaining: 0.5, ResetTS: resetTS - 100000})
	for i := 0; i < 2500; i++ {
		ts := now.Unix() - 13*86400 + int64(i)*300
		if ts >= now.Unix()-3*3600 {
			break
		}
		bloated = append(bloated, QuotaPoint{TS: ts, Remaining: 1.0, ResetTS: resetTS + int64(i%3)})
	}
	st.QuotaHistory["main:gemini-5h"] = bloated

	// Trigger compaction even on an unchanged observation when history is bloated.
	st.RecordQuota("main:gemini-5h", 1.0, resetTS, now.Add(-2*time.Hour+60*time.Second))
	if got := len(st.QuotaHistory["main:gemini-5h"]); got > 20 {
		t.Fatalf("expected bloated flat 14d history to compact below 20 pts, got %d", got)
	}
	if st.QuotaHistory["main:gemini-5h"][0].TS < now.Unix()-14*86400 {
		t.Fatalf("expected >14d point to be pruned")
	}

	// Simulate active burn over past 90 minutes and verify EstimateRunway remains accurate.
	st.RecordQuota("main:gemini-5h", 1.0, resetTS, now.Add(-90*time.Minute))
	st.RecordQuota("main:gemini-5h", 0.90, resetTS, now.Add(-60*time.Minute))
	st.RecordQuota("main:gemini-5h", 0.80, resetTS, now.Add(-30*time.Minute))
	st.RecordQuota("main:gemini-5h", 0.70, resetTS, now)

	runway := st.EstimateRunway("main:gemini-5h", 0.70, resetTS, now)
	if runway.HoursUntilEmpty <= 0 || runway.BurnPctPerHour < 10.0 || !runway.ExhaustsBeforeReset {
		t.Fatalf("unexpected runway estimate: %+v", runway)
	}

	// Save compacts JSON and clears dirty flag; SaveIfDirty becomes a no-op.
	if err := st.SaveIfDirty(); err != nil {
		t.Fatalf("SaveIfDirty failed: %v", err)
	}
	if st.IsDirty() {
		t.Fatal("expected IsDirty=false after SaveIfDirty")
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\n  ") {
		t.Fatalf("expected compact JSON in state.json, got indented output")
	}
}

func TestCompactQuotaHistoryPrunesOfflineAndExpiredKeys(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := LoadState(statePath)

	now := time.Unix(1790000000, 0)
	resetTS := now.Add(4 * time.Hour).Unix()

	// Offline instance with legacy 14d bloat (including sliding resetTS while idle at 100%)
	offlinePts := make([]QuotaPoint, 0, 500)
	for i := 0; i < 500; i++ {
		ts := now.Unix() - 10*86400 + int64(i)*600
		offlinePts = append(offlinePts, QuotaPoint{
			TS:        ts,
			Remaining: 1.0,
			ResetTS:   ts + 5*3600,
		})
	}
	st.QuotaHistory["offline:gemini-5h"] = offlinePts

	// Deleted instance with only expired (>14d) points
	st.QuotaHistory["deleted:gemini-5h"] = []QuotaPoint{
		{TS: now.Unix() - 15*86400, Remaining: 0.5, ResetTS: resetTS},
		{TS: now.Unix() - 14*86400 - 10, Remaining: 0.4, ResetTS: resetTS},
	}

	if !st.CompactQuotaHistory(now) {
		t.Fatal("expected CompactQuotaHistory to report changes")
	}
	if _, exists := st.QuotaHistory["deleted:gemini-5h"]; exists {
		t.Fatal("expected expired >14d key deleted:gemini-5h to be removed")
	}
	if got := len(st.QuotaHistory["offline:gemini-5h"]); got > 10 {
		t.Fatalf("expected offline flat plateau to compact <= 10 pts, got %d", got)
	}
	if !st.IsDirty() {
		t.Fatal("expected IsDirty=true after CompactQuotaHistory")
	}
	if st.CompactQuotaHistory(now) {
		t.Fatal("expected second CompactQuotaHistory pass to be a no-op")
	}
}
