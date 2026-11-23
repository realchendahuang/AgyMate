package colab

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func writeMockColabSafe(t *testing.T, dir string) (string, string) {
	t.Helper()
	stateFile := filepath.Join(dir, "mode.txt")
	countFile := filepath.Join(dir, "calls.txt")
	scriptPath := filepath.Join(dir, "colab-safe")
	script := `#!/bin/sh
STATE_FILE="` + stateFile + `"
COUNT_FILE="` + countFile + `"
echo x >> "$COUNT_FILE"
MODE=$(cat "$STATE_FILE" 2>/dev/null || echo "idle")
CMD="$1"
shift || true
if [ "$CMD" = "doctor" ]; then
  if [ "$MODE" = "orphan" ]; then
    echo '{"ok":true,"status":"authenticated","account":"dev@example.com","currentBalance":180.5,"consumptionRateHourly":5.37,"assignmentsCount":1,"eligibleGpus":["H100","G4","A100","L4","T4"],"eligibleTpus":["V6E1","V5E1"],"sessions":[{"name":"?","orphan":true,"endpoint":"m-s-abc123","accelerator":"A100","variant":"GPU","shape":"HIGHMEM"}],"checkedAt":1794638000}'
    exit 0
  fi
  echo '{"ok":true,"status":"authenticated","account":"dev@example.com","currentBalance":2512.08,"consumptionRateHourly":0.0,"assignmentsCount":0,"eligibleGpus":["H100","G4","A100","L4","T4"],"eligibleTpus":["V6E1","V5E1"],"sessions":[],"checkedAt":1794638000}'
  exit 0
fi
if [ "$CMD" = "nuke" ]; then
  echo "idle" > "$STATE_FILE"
  echo '{"ok":true,"terminatedCount":1,"terminated":[{"name":"?","orphan":true,"endpoint":"m-s-abc123","accelerator":"A100","variant":"GPU","shape":"HIGHMEM"}],"remainingCount":0,"failedCount":0,"currentBalance":2512.08,"consumptionRateHourly":0.0,"checkedAt":1794638010}'
  exit 0
fi
exit 2
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock script: %v", err)
	}
	return scriptPath, stateFile
}

func TestCollectorProbeCacheAndForce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGYMATE_COLAB_ACCOUNTS", filepath.Join(dir, "colab_accounts.json"))
	scriptPath, _ := writeMockColabSafe(t, dir)

	now := time.Unix(1794638000, 0)
	c := NewCollector()
	c.nowFn = func() time.Time { return now }

	cfg := config.DefaultConfig().Colab
	cfg.Enabled = true
	cfg.CLIPath = scriptPath
	cfg.PollIntervalSec = 60

	s1 := c.Probe(context.Background(), cfg, false)
	if !s1.OK || s1.Status != "authenticated" {
		t.Fatalf("expected authenticated OK snapshot, got %+v", s1)
	}
	if s1.Account != "dev@example.com" || s1.CurrentBalance != 2512.08 {
		t.Fatalf("unexpected account/balance: %+v", s1)
	}
	if len(s1.EligibleGPUs) != 5 || s1.EligibleGPUs[0] != "H100" {
		t.Fatalf("expected eligible GPUs starting with H100, got %+v", s1.EligibleGPUs)
	}
	if s1.EstimatedValueUSD < 251.0 || s1.EstimatedValueUSD > 251.5 {
		t.Fatalf("expected ~251.21 USD value, got %f", s1.EstimatedValueUSD)
	}
	if s1.HoursUntilEmpty != -1 {
		t.Fatalf("expected HoursUntilEmpty=-1 when idle, got %f", s1.HoursUntilEmpty)
	}

	// Within 60s without force: must reuse cache (only 1 invocation so far).
	now = now.Add(20 * time.Second)
	s2 := c.Probe(context.Background(), cfg, false)
	if !s2.OK {
		t.Fatalf("expected cached snapshot ok")
	}
	rawCalls, _ := os.ReadFile(filepath.Join(dir, "calls.txt"))
	if got := len(strings.Split(strings.TrimSpace(string(rawCalls)), "\n")); got != 1 {
		t.Fatalf("expected 1 subprocess call due to 60s cache, got %d", got)
	}

	// force=true bypasses interval cache.
	_ = c.Probe(context.Background(), cfg, true)
	rawCalls, _ = os.ReadFile(filepath.Join(dir, "calls.txt"))
	if got := len(strings.Split(strings.TrimSpace(string(rawCalls)), "\n")); got != 2 {
		t.Fatalf("expected 2 subprocess calls after force=true, got %d", got)
	}
}

func TestCollectorOrphanDurationAlertsAndNuke(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGYMATE_COLAB_ACCOUNTS", filepath.Join(dir, "colab_accounts.json"))
	scriptPath, stateFile := writeMockColabSafe(t, dir)
	if err := os.WriteFile(stateFile, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	now := time.Unix(1794638000, 0)
	c := NewCollector()
	c.nowFn = func() time.Time { return now }

	cfg := config.DefaultConfig().Colab
	cfg.Enabled = true
	cfg.CLIPath = scriptPath
	cfg.PollIntervalSec = 15
	cfg.LowBalanceCCU = 200.0
	cfg.WarnActiveMinutes = 30

	s1 := c.Probe(context.Background(), cfg, true)
	if s1.AssignmentsCount != 1 || s1.OrphanCount != 1 || s1.ActiveSince != now.Unix() {
		t.Fatalf("expected orphan active VM tracking, got %+v", s1)
	}
	if s1.HoursUntilEmpty <= 0 {
		t.Fatalf("expected positive HoursUntilEmpty when burning, got %f", s1.HoursUntilEmpty)
	}

	// At 0m active: low-balance alert fires once, burn alert waits until >=5m for orphan.
	burn, low, _ := c.EvaluateAlerts(cfg, now)
	if burn {
		t.Fatalf("expected no burn alert at 0m")
	}
	if !low {
		t.Fatalf("expected low-balance alert when 180.5 <= 200")
	}

	// Advance 6 minutes -> orphan burn alert triggers, low alert deduplicated.
	now = now.Add(6 * time.Minute)
	burn, low, snap := c.EvaluateAlerts(cfg, now)
	if !burn || snap.ActiveMinutes != 6 {
		t.Fatalf("expected orphan burn alert at 6m, got burn=%v mins=%d", burn, snap.ActiveMinutes)
	}
	if low {
		t.Fatalf("expected low-balance alert deduped")
	}

	// Nuke clears orphan VM and resets ActiveSince.
	nukeRes, afterSnap, err := c.Nuke(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected Nuke error: %v", err)
	}
	if !nukeRes.OK || nukeRes.TerminatedCount != 1 || nukeRes.RemainingCount != 0 {
		t.Fatalf("unexpected nuke result: %+v", nukeRes)
	}
	if afterSnap.AssignmentsCount != 0 || afterSnap.ActiveSince != 0 || afterSnap.CurrentBalance != 2512.08 {
		t.Fatalf("expected clean state after nuke, got %+v", afterSnap)
	}
}

func TestCollectorMissingCLIAndDisabled(t *testing.T) {
	t.Setenv("AGYMATE_COLAB_ACCOUNTS", filepath.Join(t.TempDir(), "colab_accounts.json"))
	c := NewCollector()
	cfg := config.DefaultConfig().Colab
	cfg.Enabled = false
	sDisabled := c.Probe(context.Background(), cfg, true)
	if sDisabled.Status != "disabled" || sDisabled.Enabled {
		t.Fatalf("expected disabled status, got %+v", sDisabled)
	}

	cfg.Enabled = true
	cfg.CLIPath = filepath.Join(t.TempDir(), "does-not-exist")
	sMissing := c.Probe(context.Background(), cfg, true)
	if sMissing.Status != "missing_cli" || sMissing.Installed || sMissing.OK {
		t.Fatalf("expected missing_cli status, got %+v", sMissing)
	}
}
