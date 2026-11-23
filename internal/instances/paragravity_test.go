package instances

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

func TestExtractAccountInfoV229JSON(t *testing.T) {
	dir := t.TempDir()
	gemDir := filepath.Join(dir, "home", ".gemini")
	if err := os.MkdirAll(gemDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tok := map[string]any{
		"auth_method":    "oauth",
		"id_token_email": "box1@example.com",
		"token": map[string]any{
			"access_token":  "ya29.short",
			"refresh_token": "1//persistent",
			"expiry":        time.Now().Add(-48 * time.Hour).Format(time.RFC3339), // short-lived access token already expired
		},
	}
	b, _ := json.Marshal(tok)
	if err := os.WriteFile(filepath.Join(gemDir, "jetski-standalone-oauth-token"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	info := ExtractAccountInfo(dir)
	if info.Email != "box1@example.com" {
		t.Fatalf("expected email box1@example.com, got %q", info.Email)
	}
	if !info.HasToken || !info.HasRefreshToken {
		t.Fatalf("expected HasToken=true and HasRefreshToken=true, got %+v", info)
	}
}

func TestExtractAccountInfoLegacyJWT(t *testing.T) {
	dir := t.TempDir()
	gemDir := filepath.Join(dir, "home", ".gemini")
	_ = os.MkdirAll(gemDir, 0o700)

	claims, _ := json.Marshal(map[string]any{
		"email": "legacy@example.com",
		"exp":   time.Now().Add(2 * time.Hour).Unix(),
	})
	jwt := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	_ = os.WriteFile(filepath.Join(gemDir, "jetski-standalone-oauth-token"), []byte(jwt), 0o600)

	info := ExtractAccountInfo(dir)
	if info.Email != "legacy@example.com" || !info.HasToken || info.HasRefreshToken {
		t.Fatalf("unexpected legacy info: %+v", info)
	}
}

func TestValidateProfileNameBlocksMainAndIDE(t *testing.T) {
	for _, bad := range []string{"main", "ide", "../escape", "a b", ""} {
		if err := ValidateProfileName(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
	for _, good := range []string{"p1", "p2", "work-01", "dev.box"} {
		if err := ValidateProfileName(good); err != nil {
			t.Fatalf("expected valid for %q: %v", good, err)
		}
	}
}

func TestHealHomeSymlinksRepairsBrokenTarget(t *testing.T) {
	realHome := t.TempDir()
	profHome := t.TempDir()

	// Create real target file and broken symlink pointing elsewhere
	realZsh := filepath.Join(realHome, ".zshrc")
	_ = os.WriteFile(realZsh, []byte("# zsh\n"), 0o600)
	brokenDst := filepath.Join(profHome, ".zshrc")
	_ = os.Symlink("/Users/deleted-user/.zshrc", brokenDst)

	danglingDst := filepath.Join(profHome, ".nonexistent")
	_ = os.Symlink("/Users/deleted-user/.nonexistent", danglingDst)

	repaired, removed := healHomeSymlinks(realHome, profHome, "none")
	if len(repaired) != 1 || repaired[0] != ".zshrc" {
		t.Fatalf("expected [.zshrc] repaired, got %v", repaired)
	}
	if len(removed) != 1 || removed[0] != ".nonexistent" {
		t.Fatalf("expected [.nonexistent] removed, got %v", removed)
	}
	target, err := os.Readlink(brokenDst)
	if err != nil || target != realZsh {
		t.Fatalf("expected repaired symlink -> %s, got %s (%v)", realZsh, target, err)
	}
}

func TestCreateAndDeleteProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.DefaultConfig()
	cfg.Paths.ProfilesDir = filepath.Join(home, "profiles")

	// Protected & unsafe names must fail DeleteProfile
	for _, blocked := range []string{"main", "ide", "../escape", "nonexistent"} {
		if err := DeleteProfile(cfg, blocked, false); err == nil {
			t.Fatalf("expected DeleteProfile(%q) error, got nil", blocked)
		}
	}

	meta, err := CreateProfile(cfg, CreateProfileOpts{
		Name:        "work",
		Description: "Work Account",
		LinksPolicy: "none",
	})
	if err != nil {
		t.Fatalf("CreateProfile error: %v", err)
	}
	if meta.Name != "work" {
		t.Fatalf("expected meta.Name=work, got %q", meta.Name)
	}

	profileDir := filepath.Join(cfg.Paths.ProfilesDir, "work")
	if !dirExists(profileDir) {
		t.Fatalf("expected %s to exist after CreateProfile", profileDir)
	}

	got := DiscoverAll(cfg)
	found := false
	for _, inst := range got {
		if inst.ID == "work" {
			found = true
			if inst.Label != "Work Account" {
				t.Fatalf("expected label 'Work Account' from meta.Description, got %q", inst.Label)
			}
			if inst.PrimaryGeminiDir != filepath.Join(profileDir, "home", ".gemini", "antigravity") {
				t.Fatalf("unexpected PrimaryGeminiDir: %s", inst.PrimaryGeminiDir)
			}
		}
	}
	if !found {
		t.Fatalf("expected created profile work to be discovered: %+v", got)
	}

	if err := DeleteProfile(cfg, "work", false); err != nil {
		t.Fatalf("DeleteProfile(work) failed: %v", err)
	}
	if dirExists(profileDir) {
		t.Fatalf("expected %s removed after DeleteProfile", profileDir)
	}
}

func TestBuildProfileLaunchArgs(t *testing.T) {
	ws := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "data")

	cfg := config.DefaultConfig()
	cfg.CDP.Enabled = true
	cfg.CDP.WindowHUD = true
	cfg.CDP.ZhInject = true

	args, err := buildProfileLaunchArgs(cfg, dataDir, ws)
	if err != nil {
		t.Fatalf("buildProfileLaunchArgs failed: %v", err)
	}
	want := []string{"--user-data-dir=" + dataDir, "--remote-debugging-port=0", ws}
	if len(args) != len(want) {
		t.Fatalf("expected %d args %v, got %v", len(want), want, args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg[%d]: want %q, got %q", i, want[i], args[i])
		}
	}

	// Only ZhInject enabled should still append CDP ephemeral port
	cfg.CDP.WindowHUD = false
	cfg.CDP.ZhInject = true
	args, err = buildProfileLaunchArgs(cfg, dataDir, "")
	if err != nil {
		t.Fatalf("buildProfileLaunchArgs(zh-only) failed: %v", err)
	}
	if len(args) != 2 || args[1] != "--remote-debugging-port=0" {
		t.Fatalf("expected --remote-debugging-port=0 when ZhInject=true, got %v", args)
	}

	// CDP disabled should omit --remote-debugging-port=0
	cfg.CDP.Enabled = false
	args, err = buildProfileLaunchArgs(cfg, dataDir, "")
	if err != nil {
		t.Fatalf("buildProfileLaunchArgs(cdp-off) failed: %v", err)
	}
	if len(args) != 1 || args[0] != "--user-data-dir="+dataDir {
		t.Fatalf("expected only --user-data-dir when CDP disabled, got %v", args)
	}

	// Non-existent workspace must return error
	if _, err := buildProfileLaunchArgs(cfg, dataDir, filepath.Join(ws, "does-not-exist")); err == nil {
		t.Fatalf("expected error for missing workspace directory")
	}
}
