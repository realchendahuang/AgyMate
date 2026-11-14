package instances

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

var (
	// ErrReadOnlyLifecycle prevents lifecycle mutations on main or ide.
	ErrReadOnlyLifecycle = errors.New("main and ide lifecycles are protected and strictly read-only")
	validProfileNameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	secretKeyRe          = regexp.MustCompile(`(?i)key|token|secret|password|credential|auth|signature|private`)
)

// ProfileMeta mirrors ~/.antigravity-profiles/<name>/profile.json.
type ProfileMeta struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	SymlinkPolicy  string `json:"symlink_policy"`
	InheritedFrom  string `json:"inherited_from,omitempty"`
	CreatedAt      string `json:"created_at"`
	Version        int    `json:"version"`
	LastAppVersion string `json:"last_app_version,omitempty"`
}

// AccountInfo describes extracted Google OAuth metadata inside a ParaGravity profile.
type AccountInfo struct {
	Email           string `json:"email"`
	TokenExp        string `json:"tokenExp"`
	HasToken        bool   `json:"hasToken"`
	HasRefreshToken bool   `json:"hasRefreshToken"`
}

// HealReport summarizes repairs performed on a profile.
type HealReport struct {
	Profile           string   `json:"profile"`
	SkippedRunning    bool     `json:"skippedRunning"`
	LocksCleaned      []string `json:"locksCleaned,omitempty"`
	VersionChanged    bool     `json:"versionChanged"`
	OldVersion        string   `json:"oldVersion,omitempty"`
	NewVersion        string   `json:"newVersion,omitempty"`
	CachesPurged      []string `json:"cachesPurged,omitempty"`
	SymlinksRepaired  []string `json:"symlinksRepaired,omitempty"`
	SymlinksRemoved   []string `json:"symlinksRemoved,omitempty"`
	UpdaterSuppressed bool     `json:"updaterSuppressed"`
}

// CreateProfileOpts configures creation of a new ParaGravity sandbox profile.
type CreateProfileOpts struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	LinksPolicy   string `json:"linksPolicy"` // full | minimal | none
	InheritFrom   string `json:"inheritFrom"` // "" | "host" | <profile>
	InheritMCP    bool   `json:"inheritMcp"`
	Launch        bool   `json:"launch"`
	WorkspacePath string `json:"workspacePath"`
}

var (
	linkItemsFull = []string{
		".zshrc", ".zshenv", ".zprofile",
		".bashrc", ".bash_profile", ".profile",
		".gitconfig", ".gitignore_global",
		".ssh",
		".config",
		".npmrc", ".yarnrc",
		"Desktop", "Documents", "Downloads", "Projects", "Developer",
	}
	linkItemsMinimal = []string{
		".zshrc", ".zshenv", ".zprofile",
		".bashrc", ".bash_profile", ".profile",
		".gitconfig", ".gitignore_global",
		"Desktop", "Documents", "Downloads", "Projects", "Developer",
	}
	ephemeralCacheDirs = []string{
		"Code Cache",
		"GPUCache",
		"DawnGraphiteCache",
		"DawnWebGPUCache",
		"Cache",
	}
)

// ValidateProfileName checks name safety and rejects reserved slots main/ide.
func ValidateProfileName(name string) error {
	if name == "main" || name == "ide" {
		return ErrReadOnlyLifecycle
	}
	if !validProfileNameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: must match ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$", name)
	}
	return nil
}

// ExtractAccountInfo reads google_accounts.json and jetski-standalone-oauth-token,
// supporting both v2.29+ JSON format (with persistent refresh_token) and legacy JWT.
func ExtractAccountInfo(profileDir string) AccountInfo {
	var info AccountInfo
	geminiDir := filepath.Join(profileDir, "home", ".gemini")
	if !dirExists(geminiDir) {
		return info
	}

	if raw, err := os.ReadFile(filepath.Join(geminiDir, "google_accounts.json")); err == nil {
		var acc struct {
			Active string `json:"active"`
		}
		if json.Unmarshal(raw, &acc) == nil && acc.Active != "" {
			info.Email = acc.Active
		}
	}

	tokPath := filepath.Join(geminiDir, "jetski-standalone-oauth-token")
	rawBytes, err := os.ReadFile(tokPath)
	if err != nil {
		return info
	}
	tokRaw := strings.TrimSpace(string(rawBytes))
	if tokRaw == "" {
		return info
	}
	info.HasToken = true

	var tokJSON struct {
		IDTokenEmail string `json:"id_token_email"`
		Token        struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Expiry       string `json:"expiry"`
			ExpiresAt    string `json:"expires_at"`
		} `json:"token"`
	}
	if json.Unmarshal([]byte(tokRaw), &tokJSON) == nil && (tokJSON.IDTokenEmail != "" || tokJSON.Token.RefreshToken != "" || tokJSON.Token.AccessToken != "") {
		if tokJSON.IDTokenEmail != "" {
			info.Email = tokJSON.IDTokenEmail
		}
		if tokJSON.Token.RefreshToken != "" {
			info.HasRefreshToken = true
		}
		if tokJSON.Token.Expiry != "" {
			info.TokenExp = tokJSON.Token.Expiry
		} else if tokJSON.Token.ExpiresAt != "" {
			info.TokenExp = tokJSON.Token.ExpiresAt
		}
		return info
	}

	// Legacy pure JWT fallback
	if claims := decodeJWTPayload(tokRaw); len(claims) > 0 {
		if info.Email == "" {
			if em, ok := claims["email"].(string); ok {
				info.Email = em
			}
		}
		if exp, ok := claims["exp"].(float64); ok && exp > 0 {
			info.TokenExp = time.Unix(int64(exp), 0).Format(time.RFC3339)
		}
	}
	return info
}

func decodeJWTPayload(jwtStr string) map[string]any {
	parts := strings.Split(strings.TrimSpace(jwtStr), ".")
	if len(parts) < 2 {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil
		}
	}
	var out map[string]any
	if json.Unmarshal(decoded, &out) != nil {
		return nil
	}
	return out
}

func readProfileMeta(profileDir string) ProfileMeta {
	var meta ProfileMeta
	raw, err := os.ReadFile(filepath.Join(profileDir, "profile.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &meta)
	}
	if meta.SymlinkPolicy == "" {
		meta.SymlinkPolicy = "full"
	}
	return meta
}

func writeProfileMeta(profileDir string, meta ProfileMeta) error {
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	p := filepath.Join(profileDir, "profile.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ReadAppBundleVersion reads CFBundleShortVersionString from an app's Info.plist via plutil/PlistBuddy.
func ReadAppBundleVersion(appPath string) string {
	if appPath == "" {
		return ""
	}
	plist := filepath.Join(appPath, "Contents", "Info.plist")
	if !fileExists(plist) {
		return ""
	}
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", plist).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// HealProfile repairs stale locks, broken home symlinks, obsolete caches, and auto-update flags.
func HealProfile(cfg config.Config, name string, force bool) (HealReport, error) {
	if err := ValidateProfileName(name); err != nil {
		return HealReport{}, err
	}
	profileDir := filepath.Join(config.ExpandPath(cfg.Box.ProfilesDir), name)
	if !dirExists(profileDir) {
		return HealReport{}, fmt.Errorf("profile %q not found at %s", name, profileDir)
	}

	report := HealReport{Profile: name}
	homeDir := filepath.Join(profileDir, "home")
	dataDir := filepath.Join(profileDir, "data")
	procs := listProcesses()
	running := findProfilePID(profileDir, dataDir, procs) > 0

	// Always safe to repair broken symlinks inside home/ when target moved (e.g. /Users/kim -> /Users/chendahuang).
	realHome, _ := os.UserHomeDir()
	meta := readProfileMeta(profileDir)
	repaired, removed := healHomeSymlinks(realHome, homeDir, meta.SymlinkPolicy)
	report.SymlinksRepaired = repaired
	report.SymlinksRemoved = removed

	if running {
		report.SkippedRunning = true
		return report, nil
	}

	// Clean stale locks
	for _, lockName := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "DevToolsActivePort"} {
		lp := filepath.Join(dataDir, lockName)
		if _, err := os.Lstat(lp); err == nil {
			if os.Remove(lp) == nil {
				report.LocksCleaned = append(report.LocksCleaned, lockName)
			}
		}
	}
	runPID := filepath.Join(profileDir, "run.pid")
	if _, err := os.Lstat(runPID); err == nil {
		if os.Remove(runPID) == nil {
			report.LocksCleaned = append(report.LocksCleaned, "run.pid")
		}
	}

	boxPath := cfg.Box.BoxAppPath
	if !fileExists(filepath.Join(boxPath, "Contents", "MacOS", "Antigravity")) {
		boxPath = cfg.Box.MainAppPath
	}
	curVer := ReadAppBundleVersion(boxPath)
	report.OldVersion = meta.LastAppVersion
	report.NewVersion = curVer
	report.VersionChanged = curVer != "" && meta.LastAppVersion != curVer

	if (report.VersionChanged || force) && dirExists(dataDir) {
		for _, cdir := range ephemeralCacheDirs {
			full := filepath.Join(dataDir, cdir)
			if dirExists(full) {
				if os.RemoveAll(full) == nil {
					report.CachesPurged = append(report.CachesPurged, cdir)
				}
			}
		}
		if curVer != "" {
			meta.LastAppVersion = curVer
			_ = writeProfileMeta(profileDir, meta)
		}
	}

	report.UpdaterSuppressed = suppressSandboxAutoupdate(dataDir)
	hardenProfilePerms(profileDir)
	return report, nil
}

func healHomeSymlinks(realHome, profileHome, policy string) (repaired []string, removed []string) {
	if realHome == "" || !dirExists(profileHome) {
		return nil, nil
	}
	entries, err := os.ReadDir(profileHome)
	if err == nil {
		for _, ent := range entries {
			dst := filepath.Join(profileHome, ent.Name())
			linfo, err := os.Lstat(dst)
			if err != nil || linfo.Mode()&os.ModeSymlink == 0 {
				continue
			}
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			// Broken symlink! Remove it, and recreate if realHome has the item.
			_ = os.Remove(dst)
			src := filepath.Join(realHome, ent.Name())
			if _, err := os.Stat(src); err == nil {
				if os.Symlink(src, dst) == nil {
					repaired = append(repaired, ent.Name())
					continue
				}
			}
			removed = append(removed, ent.Name())
		}
	}

	// Ensure configured policy items exist if present in realHome
	items := linkItemsFull
	switch policy {
	case "minimal":
		items = linkItemsMinimal
	case "none":
		items = nil
	}
	for _, item := range items {
		src := filepath.Join(realHome, item)
		dst := filepath.Join(profileHome, item)
		if _, err := os.Stat(src); err == nil {
			if _, err := os.Lstat(dst); os.IsNotExist(err) {
				if os.Symlink(src, dst) == nil {
					repaired = append(repaired, item)
				}
			}
		}
	}

	// Keychain dir symlink for macOS compatibility (same as ParaGravity)
	realKC := filepath.Join(realHome, "Library", "Keychains")
	profLib := filepath.Join(profileHome, "Library")
	profKC := filepath.Join(profLib, "Keychains")
	if dirExists(realKC) {
		_ = os.MkdirAll(profLib, 0o700)
		if linfo, err := os.Lstat(profKC); err == nil {
			if linfo.Mode()&os.ModeSymlink != 0 {
				if _, err := os.Stat(profKC); err != nil {
					_ = os.Remove(profKC)
					_ = os.Symlink(realKC, profKC)
				}
			}
		} else if os.IsNotExist(err) {
			_ = os.Symlink(realKC, profKC)
		}
	}
	return repaired, removed
}

func suppressSandboxAutoupdate(dataDir string) bool {
	modified := false
	appStoragePath := filepath.Join(dataDir, "app_storage.json")
	appData := make(map[string]any)
	if raw, err := os.ReadFile(appStoragePath); err == nil {
		_ = json.Unmarshal(raw, &appData)
	}
	if appData["autoCheckForUpdates"] != "false" {
		appData["autoCheckForUpdates"] = "false"
		_ = os.MkdirAll(dataDir, 0o700)
		if b, err := json.MarshalIndent(appData, "", "  "); err == nil {
			b = append(b, '\n')
			if os.WriteFile(appStoragePath, b, 0o600) == nil {
				modified = true
			}
		}
	}

	userSettingsPath := filepath.Join(dataDir, "User", "settings.json")
	sData := make(map[string]any)
	if raw, err := os.ReadFile(userSettingsPath); err == nil {
		_ = json.Unmarshal(raw, &sData)
	}
	if sData["update.mode"] != "none" {
		sData["update.mode"] = "none"
		_ = os.MkdirAll(filepath.Dir(userSettingsPath), 0o700)
		if b, err := json.MarshalIndent(sData, "", "  "); err == nil {
			b = append(b, '\n')
			if os.WriteFile(userSettingsPath, b, 0o600) == nil {
				modified = true
			}
		}
	}
	return modified
}

func hardenProfilePerms(profileDir string) {
	dirs := []string{
		profileDir,
		filepath.Join(profileDir, "data"),
		filepath.Join(profileDir, "home"),
		filepath.Join(profileDir, "home", ".gemini"),
		filepath.Join(profileDir, "home", ".gemini", "config"),
		filepath.Join(profileDir, "data", "User"),
	}
	for _, d := range dirs {
		if dirExists(d) {
			_ = os.Chmod(d, 0o700)
		}
	}
}

// LaunchProfile starts an isolated ParaGravity profile with Antigravity-Box.app.
func LaunchProfile(cfg config.Config, name, workspace string, forceHeal bool) (int, error) {
	if err := ValidateProfileName(name); err != nil {
		return 0, err
	}
	profileDir := filepath.Join(config.ExpandPath(cfg.Box.ProfilesDir), name)
	if !dirExists(profileDir) {
		return 0, fmt.Errorf("profile %q does not exist", name)
	}
	dataDir := filepath.Join(profileDir, "data")
	homeDir := filepath.Join(profileDir, "home")

	procs := listProcesses()
	if existing := findProfilePID(profileDir, dataDir, procs); existing > 0 {
		return existing, nil
	}

	if _, err := HealProfile(cfg, name, forceHeal); err != nil {
		return 0, err
	}

	appBin := filepath.Join(cfg.Box.BoxAppPath, "Contents", "MacOS", "Antigravity")
	if !fileExists(appBin) {
		appBin = filepath.Join(cfg.Box.MainAppPath, "Contents", "MacOS", "Antigravity")
	}
	if !fileExists(appBin) {
		return 0, fmt.Errorf("Antigravity binary not found at %s", appBin)
	}

	resolvedData, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		resolvedData = filepath.Clean(dataDir)
	}
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		resolvedHome = filepath.Clean(homeDir)
	}

	args := []string{"--user-data-dir=" + resolvedData}
	if workspace != "" {
		absWS, err := filepath.Abs(config.ExpandPath(workspace))
		if err != nil || !dirExists(absWS) {
			return 0, fmt.Errorf("workspace does not exist: %s", workspace)
		}
		args = append(args, absWS)
	}

	logDir := filepath.Join(profileDir, "logs")
	_ = os.MkdirAll(logDir, 0o700)
	logPath := filepath.Join(logDir, fmt.Sprintf("launch-%s-%d.log", time.Now().Format("20060102-150405"), os.Getpid()))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	cmd := exec.Command(appBin, args...)
	cmd.Env = buildProfileEnv(resolvedHome)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = os.WriteFile(filepath.Join(profileDir, "run.pid"), []byte(strconv.Itoa(pid)), 0o600)
	_ = cmd.Process.Release()
	pruneLaunchLogs(logDir, 10)
	return pid, nil
}

func buildProfileEnv(resolvedHome string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+4)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "HOME", "SSH_CONNECTION", "SSH_CLIENT", "ELECTRON_RUN_AS_NODE":
			continue
		default:
			out = append(out, kv)
		}
	}
	out = append(out,
		"HOME="+resolvedHome,
		"SSH_CONNECTION=127.0.0.1 50000 127.0.0.1 22",
		"SSH_CLIENT=127.0.0.1 50000 22",
	)
	return out
}

func pruneLaunchLogs(logDir string, keep int) {
	matches, err := filepath.Glob(filepath.Join(logDir, "launch-*.log"))
	if err != nil || len(matches) <= keep {
		return
	}
	type item struct {
		path string
		mt   time.Time
	}
	items := make([]item, 0, len(matches))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		items = append(items, item{path: m, mt: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].mt.After(items[j].mt)
	})
	for i := keep; i < len(items); i++ {
		_ = os.Remove(items[i].path)
	}
}

// StopProfile gracefully stops a running ParaGravity profile (never main/ide).
func StopProfile(cfg config.Config, name string, force bool) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	profileDir := filepath.Join(config.ExpandPath(cfg.Box.ProfilesDir), name)
	if !dirExists(profileDir) {
		return fmt.Errorf("profile %q does not exist", name)
	}
	dataDir := filepath.Join(profileDir, "data")
	procs := listProcesses()
	pid := findProfilePID(profileDir, dataDir, procs)
	if pid == 0 {
		_ = os.Remove(filepath.Join(profileDir, "run.pid"))
		return nil
	}

	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !IsPIDAlive(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if IsPIDAlive(pid) && force {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if !IsPIDAlive(pid) {
		_ = os.Remove(filepath.Join(profileDir, "run.pid"))
		return nil
	}
	return fmt.Errorf("profile %q (pid %d) is still running; retry with force", name, pid)
}

// CreateProfile provisions a new isolated ParaGravity profile with sanitized host config inheritance
// and a macOS ~/Applications/Antigravity (<name>).app launcher stub.
func CreateProfile(cfg config.Config, opts CreateProfileOpts) (ProfileMeta, error) {
	name := strings.TrimSpace(opts.Name)
	if err := ValidateProfileName(name); err != nil {
		return ProfileMeta{}, err
	}
	profilesDir := config.ExpandPath(cfg.Box.ProfilesDir)
	profileDir := filepath.Join(profilesDir, name)
	if dirExists(profileDir) {
		return ProfileMeta{}, fmt.Errorf("profile %q already exists", name)
	}
	policy := opts.LinksPolicy
	if policy == "" {
		policy = "minimal"
	}
	if policy != "full" && policy != "minimal" && policy != "none" {
		return ProfileMeta{}, fmt.Errorf("invalid symlink policy %q", policy)
	}

	homeDir := filepath.Join(profileDir, "home")
	dataDir := filepath.Join(profileDir, "data")
	if err := os.MkdirAll(filepath.Join(homeDir, ".gemini", "config"), 0o700); err != nil {
		return ProfileMeta{}, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "User"), 0o700); err != nil {
		return ProfileMeta{}, err
	}

	realHome, _ := os.UserHomeDir()
	healHomeSymlinks(realHome, homeDir, policy)

	if opts.InheritFrom == "host" {
		inheritSanitizedHostConfig(realHome, homeDir, dataDir, opts.InheritMCP)
	}

	_ = suppressSandboxAutoupdate(dataDir)
	boxVer := ReadAppBundleVersion(cfg.Box.BoxAppPath)
	meta := ProfileMeta{
		Name:           name,
		Description:    strings.TrimSpace(opts.Description),
		SymlinkPolicy:  policy,
		InheritedFrom:  opts.InheritFrom,
		CreatedAt:      time.Now().Format(time.RFC3339),
		Version:        1,
		LastAppVersion: boxVer,
	}
	if err := writeProfileMeta(profileDir, meta); err != nil {
		return ProfileMeta{}, err
	}
	hardenProfilePerms(profileDir)
	_ = createMacOSLauncherStub(name)
	return meta, nil
}

func inheritSanitizedHostConfig(realHome, tgtHome, tgtData string, inheritMCP bool) {
	srcAppSupport := filepath.Join(realHome, "Library", "Application Support", "Antigravity", "User")
	tgtUser := filepath.Join(tgtData, "User")
	for _, fn := range []string{"settings.json", "keybindings.json"} {
		src := filepath.Join(srcAppSupport, fn)
		if b, err := os.ReadFile(src); err == nil {
			_ = os.WriteFile(filepath.Join(tgtUser, fn), b, 0o600)
		}
	}

	// Sanitize ~/.gemini/settings.json & ~/.gemini/config/config.json & mcp_config.json (never copy tokens/secrets)
	srcGemini := filepath.Join(realHome, ".gemini")
	tgtGemini := filepath.Join(tgtHome, ".gemini")
	tgtCfgDir := filepath.Join(tgtGemini, "config")
	if b, err := os.ReadFile(filepath.Join(srcGemini, "config", "config.json")); err == nil {
		var raw map[string]any
		if json.Unmarshal(b, &raw) == nil {
			if us, ok := raw["userSettings"].(map[string]any); ok {
				delete(us, "remoteControlHostname")
				if out, err := json.MarshalIndent(map[string]any{"userSettings": us}, "", "  "); err == nil {
					_ = os.WriteFile(filepath.Join(tgtCfgDir, "config.json"), out, 0o600)
				}
			}
		}
	}
	if inheritMCP {
		if b, err := os.ReadFile(filepath.Join(srcGemini, "config", "mcp_config.json")); err == nil {
			var raw map[string]any
			if json.Unmarshal(b, &raw) == nil {
				if srvs, ok := raw["mcpServers"].(map[string]any); ok {
					for _, sv := range srvs {
						if m, ok := sv.(map[string]any); ok {
							for _, field := range []string{"env", "headers"} {
								if sub, ok := m[field].(map[string]any); ok {
									for k := range sub {
										if secretKeyRe.MatchString(k) {
											sub[k] = ""
										}
									}
								}
							}
						}
					}
					if out, err := json.MarshalIndent(raw, "", "  "); err == nil {
						_ = os.WriteFile(filepath.Join(tgtCfgDir, "mcp_config.json"), out, 0o600)
					}
				}
			}
		}
	}
	// Symlink shared skills & GEMINI.md if present
	for _, item := range []string{"skills", "GEMINI.md", "AGENTS.md"} {
		src := filepath.Join(srcGemini, "config", item)
		dst := filepath.Join(tgtCfgDir, item)
		if _, err := os.Stat(src); err == nil {
			if _, err := os.Lstat(dst); os.IsNotExist(err) {
				_ = os.Symlink(src, dst)
			}
		}
	}
}

// EnsureMacOSLauncherStub creates or repairs ~/Applications/Antigravity (<name>).app via osacompile.
func EnsureMacOSLauncherStub(name string) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	return createMacOSLauncherStub(name)
}

func createMacOSLauncherStub(name string) error {
	realHome, _ := os.UserHomeDir()
	appsDir := filepath.Join(realHome, "Applications")
	_ = os.MkdirAll(appsDir, 0o755)
	appPath := filepath.Join(appsDir, fmt.Sprintf("Antigravity (%s).app", name))
	cliBin := filepath.Join(realHome, ".local", "bin", "agymate")
	script := fmt.Sprintf(`do shell script "'%s' profile launch '%s' > /dev/null 2>&1 &"`, cliBin, name)
	return exec.Command("osacompile", "-o", appPath, "-e", script).Run()
}
