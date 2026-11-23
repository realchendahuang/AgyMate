package instances

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestValidateProfileNameReserved(t *testing.T) {
	for _, ro := range []string{"main", "ide"} {
		if err := ValidateProfileName(ro); !errors.Is(err, ErrReadOnlyLifecycle) {
			t.Fatalf("expected ErrReadOnlyLifecycle for %q, got %v", ro, err)
		}
	}
	for _, reserved := range []string{"all", "custom", "", "../escape", ".hidden"} {
		if err := ValidateProfileName(reserved); err == nil {
			t.Fatalf("expected error for reserved/invalid profile name %q", reserved)
		}
	}
	for _, okName := range []string{"work-1", "client.alpha", "p5"} {
		if err := ValidateProfileName(okName); err != nil {
			t.Fatalf("expected %q to be valid, got %v", okName, err)
		}
	}
}

func TestDiscoverAllSingleAndProfilesWithoutMeta(t *testing.T) {
	origLister := processLister
	processLister = func() []procEntry { return nil }
	t.Cleanup(func() { processLister = origLister })

	home := t.TempDir()
	t.Setenv("HOME", home)
	profilesDir := filepath.Join(home, "profiles")

	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = profilesDir
	cfg.Paths.IDEAppPath = filepath.Join(home, "nonexistent-ide")

	// Empty setup should discover only main cleanly with neutral "Antigravity" label.
	got := DiscoverAll(cfg)
	if len(got) != 1 || got[0].ID != KindMain || got[0].Label != "Antigravity" {
		t.Fatalf("unexpected empty discovery result: %+v", got)
	}

	// Ensure MainAppPath exists in headless environments when verifying multi-slot profile discovery.
	if _, err := os.Stat(cfg.Paths.MainAppPath); err != nil {
		fakeApp := filepath.Join(home, "Antigravity")
		if err := os.WriteFile(fakeApp, []byte("stub"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg.Paths.MainAppPath = fakeApp
	}

	// Profile directory containing only data/ (no profile.json) should still be discovered.
	if err := os.MkdirAll(filepath.Join(profilesDir, "team-b", "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	got = DiscoverAll(cfg)
	if len(got) != 2 || got[1].ID != "team-b" || got[1].Kind != KindProfile {
		t.Fatalf("expected team-b profile discovered, got %+v", got)
	}
}

func TestExtractUserDataDirAndAdhocDiscovery(t *testing.T) {
	cases := map[string]string{
		`/Applications/Antigravity.app/Contents/MacOS/Antigravity --user-data-dir=/tmp/box1`:       `/tmp/box1`,
		`/usr/bin/antigravity --user-data-dir="/tmp/my box/data" --no-sandbox`:                     `/tmp/my box/data`,
		`/usr/bin/antigravity --user-data-dir='/tmp/single box'`:                                   `/tmp/single box`,
		`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir=/tmp/chrome`: `/tmp/chrome`,
		`/Applications/Antigravity.app/Contents/MacOS/Antigravity`:                                 ``,
	}
	for cmd, want := range cases {
		if got := extractUserDataDir(cmd); got != want {
			t.Fatalf("extractUserDataDir(%q) = %q, want %q", cmd, got, want)
		}
	}

	knownData := t.TempDir()
	adhocData := filepath.Join(t.TempDir(), "Work Box")
	if err := os.MkdirAll(adhocData, 0o700); err != nil {
		t.Fatal(err)
	}
	procs := []procEntry{
		{PID: 101, Command: `/Applications/Antigravity.app/Contents/MacOS/Antigravity --user-data-dir=` + knownData},
		{PID: 102, Command: `/Applications/Antigravity.app/Contents/MacOS/Antigravity --type=renderer --user-data-dir=` + adhocData},
		{PID: 103, Command: `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir=` + adhocData},
		{PID: 104, Command: `/Applications/Antigravity.app/Contents/MacOS/Antigravity --user-data-dir="` + adhocData + `"`},
	}
	known := []Instance{{ID: "p1", Kind: KindProfile, UserDataDir: knownData}}
	extra := discoverAdhocInstances(procs, known, "/Applications/Antigravity.app")
	if len(extra) != 1 {
		t.Fatalf("expected 1 adhoc instance, got %+v", extra)
	}
	if extra[0].Kind != KindCustom || extra[0].ID != "custom-work-box" || extra[0].MainPID != 104 || !extra[0].ReadOnlyLifecycle {
		t.Fatalf("unexpected adhoc instance: %+v", extra[0])
	}
}

func TestAssignHubsByAncestryIDEAndAdhoc(t *testing.T) {
	procs := []procEntry{
		{PID: 71158, PPID: 1, Command: "/Applications/Antigravity.app/Contents/MacOS/Antigravity"},
		{PID: 71192, PPID: 71158, Command: "/Applications/Antigravity.app/Contents/Resources/bin/language_server --subclient_type hub"},
		{PID: 85134, PPID: 1, Command: "/Applications/Antigravity IDE.app/Contents/MacOS/Electron"},
		{PID: 85198, PPID: 85134, Command: "/Applications/Antigravity IDE.app/Contents/Frameworks/Antigravity IDE Helper (Plugin).app/Contents/MacOS/Antigravity IDE Helper (Plugin)"},
		{PID: 85255, PPID: 85198, Command: "/Users/example/.gemini/bin/agy --hub --hub-port=49656 --app_data_dir=antigravity"},
		{PID: 9000, PPID: 1, Command: "/Applications/Antigravity.app/Contents/MacOS/Antigravity --user-data-dir=/tmp/work"},
		{PID: 9005, PPID: 9000, Command: "/Applications/Antigravity.app/Contents/Resources/bin/language_server --subclient_type hub"},
	}

	sharedHubs := []HubInfo{
		{PID: 85255, HTTPPort: 49656},
		{PID: 9005, HTTPPort: 50001},
		{PID: 71192, HTTPPort: 58606},
	}

	adhoc := []Instance{
		{ID: "custom-work", Kind: KindCustom, MainPID: 9000},
	}
	adhocPtrs := []*Instance{&adhoc[0]}

	mainHub, ideHub := assignHubsByAncestry(procs, sharedHubs, 71158, 85134, adhocPtrs)
	if ideHub == nil || ideHub.PID != 85255 || ideHub.HTTPPort != 49656 {
		t.Fatalf("expected IDE hub PID 85255, got %+v", ideHub)
	}
	if adhoc[0].Hub == nil || adhoc[0].Hub.PID != 9005 || adhoc[0].Hub.HTTPPort != 50001 {
		t.Fatalf("expected adhoc custom-work hub PID 9005, got %+v", adhoc[0].Hub)
	}
	if mainHub == nil || mainHub.PID != 71192 || mainHub.HTTPPort != 58606 {
		t.Fatalf("expected main hub PID 71192, got %+v", mainHub)
	}
}

func TestExtractEmailFromUserDataDirAppStorageAndVSCDB(t *testing.T) {
	// 1. app_storage.json priority
	dirApp := t.TempDir()
	appStorage := filepath.Join(dirApp, "app_storage.json")
	if err := os.WriteFile(appStorage, []byte(`{"agy.onboarding.lastLoginUsername":"alice@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := extractEmailFromUserDataDir(dirApp); got != "alice@example.com" {
		t.Fatalf("expected alice@example.com from app_storage.json, got %q", got)
	}

	// 2. state.vscdb fallback (ItemTable -> google.google-antigravity)
	dirDB := t.TempDir()
	dbDir := filepath.Join(dirDB, "User", "globalStorage")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "state.vscdb")
	con, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := con.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		con.Close()
		t.Fatal(err)
	}
	payload := `{"antigravity-storage":{"agy.onboarding.lastLoginUsername":"ide-user@example.com"}}`
	if _, err := con.Exec(`INSERT INTO ItemTable(key, value) VALUES ('google.google-antigravity', ?)`, payload); err != nil {
		con.Close()
		t.Fatal(err)
	}
	con.Close()

	if got := extractEmailFromUserDataDir(dirDB); got != "ide-user@example.com" {
		t.Fatalf("expected ide-user@example.com from state.vscdb, got %q", got)
	}
}
