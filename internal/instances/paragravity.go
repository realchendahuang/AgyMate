package instances

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/realchendahuang/AgyMate/internal/config"
)

var (
	// ErrReadOnlyLifecycle prevents lifecycle mutations on main or ide.
	ErrReadOnlyLifecycle = errors.New("默认客户端 (main) 与 IDE 客户端受保护，禁止执行启停、修复或删除操作")
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

// AccountInfo describes extracted Google OAuth metadata inside an isolated profile.
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

// CreateProfileOpts configures creation of a new isolated Antigravity profile.
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
		"GraphiteDawnCache",
		"Cache",
	}
)

// ValidateProfileName checks name safety and rejects reserved slots (main, ide, all, custom).
func ValidateProfileName(name string) error {
	if name == "main" || name == "ide" {
		return ErrReadOnlyLifecycle
	}
	if name == "all" || name == "custom" {
		return fmt.Errorf("保留实例名称 %q 不可用于隔离沙盒", name)
	}
	if !validProfileNameRe.MatchString(name) {
		return fmt.Errorf("实例名称 %q 格式无效（需以字母或数字开头，仅允许字母、数字、点、下划线或连字符，长度不超过 64）", name)
	}
	return nil
}

// ExtractAccountInfo reads google_accounts.json and jetski-standalone-oauth-token,
// supporting both v2.29+ JSON format (with persistent refresh_token) and legacy JWT.
func ExtractAccountInfo(profileDir string) AccountInfo {
	return extractAccountFromGeminiRoot(filepath.Join(profileDir, "home", ".gemini"))
}

func extractAccountFromGeminiRoot(geminiDir string) AccountInfo {
	var info AccountInfo
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

// extractEmailFromUserDataDir inspects an Antigravity user-data-dir for cached login email
// via app_storage.json or read-only User/globalStorage/state.vscdb.
func extractEmailFromUserDataDir(userDataDir string) string {
	if strings.TrimSpace(userDataDir) == "" {
		return ""
	}
	appStoragePath := filepath.Join(userDataDir, "app_storage.json")
	if raw, err := os.ReadFile(appStoragePath); err == nil {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			if email := pickLoginEmailFromMap(m); email != "" {
				return email
			}
		}
	}

	vscdbPath := filepath.Join(userDataDir, "User", "globalStorage", "state.vscdb")
	if !fileExists(vscdbPath) {
		return ""
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(500)", vscdbPath)
	con, err := sql.Open("sqlite", dsn)
	if err != nil {
		return ""
	}
	defer con.Close()

	rows, err := con.Query(`SELECT value FROM ItemTable WHERE key = 'google.google-antigravity' LIMIT 1`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	if rows.Next() {
		var val string
		if rows.Scan(&val) == nil && val != "" {
			var outer map[string]any
			if json.Unmarshal([]byte(val), &outer) == nil {
				if sub, ok := outer["antigravity-storage"].(map[string]any); ok {
					if email := pickLoginEmailFromMap(sub); email != "" {
						return email
					}
				} else if subStr, ok := outer["antigravity-storage"].(string); ok && subStr != "" {
					var subMap map[string]any
					if json.Unmarshal([]byte(subStr), &subMap) == nil {
						if email := pickLoginEmailFromMap(subMap); email != "" {
							return email
						}
					}
				}
				if email := pickLoginEmailFromMap(outer); email != "" {
					return email
				}
			}
		}
	}
	return ""
}

func pickLoginEmailFromMap(m map[string]any) string {
	if m == nil {
		return ""
	}
	for _, key := range []string{"agy.onboarding.lastLoginUsername", "jetski.onboarding.lastLoginUsername"} {
		if v, ok := m[key].(string); ok {
			trimmed := strings.TrimSpace(v)
			if strings.Contains(trimmed, "@") {
				return trimmed
			}
		}
	}
	return ""
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

// ReadAppBundleVersion reads CFBundleShortVersionString from an app's Info.plist via PlistBuddy on macOS.
func ReadAppBundleVersion(appPath string) string {
	if runtime.GOOS != "darwin" || appPath == "" {
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
	profileDir := filepath.Join(config.ExpandPath(cfg.Paths.ProfilesDir), name)
	if !dirExists(profileDir) {
		return HealReport{}, fmt.Errorf("未找到隔离实例 %q (%s)", name, profileDir)
	}

	report := HealReport{Profile: name}
	homeDir := filepath.Join(profileDir, "home")
	dataDir := filepath.Join(profileDir, "data")
	procs := listProcesses()
	running := findProfilePID(profileDir, dataDir, procs) > 0

	// Always safe to repair broken symlinks inside home/ when target moved (e.g. across user home paths).
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

	curVer := ReadAppBundleVersion(config.ExpandPath(cfg.Paths.MainAppPath))
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
				if createLink(src, dst) {
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
				if createLink(src, dst) {
					repaired = append(repaired, item)
				}
			}
		}
	}

	// Keychain dir symlink for macOS compatibility
	if runtime.GOOS == "darwin" {
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
	}
	return repaired, removed
}

func createLink(src, dst string) bool {
	if err := os.Symlink(src, dst); err == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		if info, err := os.Stat(src); err == nil && info.IsDir() {
			return exec.Command("cmd", "/c", "mklink", "/J", dst, src).Run() == nil
		}
	}
	return false
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

// LaunchProfile starts an isolated profile using the official Antigravity client.
func LaunchProfile(cfg config.Config, name, workspace string, forceHeal bool) (int, error) {
	if err := ValidateProfileName(name); err != nil {
		return 0, err
	}
	profileDir := filepath.Join(config.ExpandPath(cfg.Paths.ProfilesDir), name)
	if !dirExists(profileDir) {
		return 0, fmt.Errorf("隔离实例 %q 不存在", name)
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

	appBin := resolveAppBinary(cfg)
	if !fileExists(appBin) {
		return 0, fmt.Errorf("未找到 Antigravity 可执行文件: %s", appBin)
	}

	resolvedData, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		resolvedData = filepath.Clean(dataDir)
	}
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		resolvedHome = filepath.Clean(homeDir)
	}

	args, err := buildProfileLaunchArgs(cfg, resolvedData, workspace)
	if err != nil {
		return 0, err
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
	configureDetachedProcess(cmd)

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
	out := make([]string, 0, len(base)+8)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "SSH_CONNECTION", "SSH_CLIENT", "ELECTRON_RUN_AS_NODE":
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
	if runtime.GOOS == "windows" {
		roaming := filepath.Join(resolvedHome, "AppData", "Roaming")
		local := filepath.Join(resolvedHome, "AppData", "Local")
		_ = os.MkdirAll(roaming, 0o700)
		_ = os.MkdirAll(local, 0o700)
		out = append(out,
			"USERPROFILE="+resolvedHome,
			"APPDATA="+roaming,
			"LOCALAPPDATA="+local,
		)
	}
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

// StopProfile gracefully stops a running isolated profile (never main/ide).
func StopProfile(cfg config.Config, name string, force bool) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	profileDir := filepath.Join(config.ExpandPath(cfg.Paths.ProfilesDir), name)
	if !dirExists(profileDir) {
		return fmt.Errorf("隔离实例 %q 不存在", name)
	}
	dataDir := filepath.Join(profileDir, "data")
	procs := listProcesses()
	pid := findProfilePID(profileDir, dataDir, procs)
	if pid == 0 {
		_ = os.Remove(filepath.Join(profileDir, "run.pid"))
		return nil
	}

	if err := stopProcessTree(pid, force); err != nil {
		return fmt.Errorf("实例 %q (PID %d) 仍在运行，可尝试强制停止: %w", name, pid, err)
	}
	_ = os.Remove(filepath.Join(profileDir, "run.pid"))
	return nil
}

// DeleteProfile deletes an isolated sandbox profile directory and its macOS launcher bundle.
func DeleteProfile(cfg config.Config, name string, forceStop bool) error {
	name = strings.TrimSpace(name)
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	profilesDir := filepath.Clean(config.ExpandPath(cfg.Paths.ProfilesDir))
	profileDir := filepath.Join(profilesDir, name)
	if filepath.Dir(profileDir) != profilesDir {
		return fmt.Errorf("不安全的实例目录路径 %q", name)
	}
	if !dirExists(profileDir) {
		return fmt.Errorf("隔离实例 %q 不存在", name)
	}

	procs := listProcesses()
	if pid := findProfilePID(profileDir, filepath.Join(profileDir, "data"), procs); pid > 0 {
		if !forceStop {
			return fmt.Errorf("实例 %q 正在运行 (PID %d)，请先停止后再删除", name, pid)
		}
		if err := StopProfile(cfg, name, true); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(profileDir); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		realHome, _ := os.UserHomeDir()
		appsDir := filepath.Join(realHome, "Applications")
		stubPath := filepath.Join(appsDir, fmt.Sprintf("Antigravity (%s).app", name))
		if filepath.Dir(stubPath) == appsDir {
			_ = os.RemoveAll(stubPath)
		}
	}
	return nil
}

// CreateProfile provisions a new isolated profile with sanitized host config inheritance
// and a macOS ~/Applications/Antigravity (<name>).app launcher bundle.
func CreateProfile(cfg config.Config, opts CreateProfileOpts) (ProfileMeta, error) {
	name := strings.TrimSpace(opts.Name)
	if err := ValidateProfileName(name); err != nil {
		return ProfileMeta{}, err
	}
	profilesDir := config.ExpandPath(cfg.Paths.ProfilesDir)
	profileDir := filepath.Join(profilesDir, name)
	if dirExists(profileDir) {
		return ProfileMeta{}, fmt.Errorf("隔离实例 %q 已存在", name)
	}
	policy := opts.LinksPolicy
	if policy == "" {
		policy = "minimal"
	}
	if policy != "full" && policy != "minimal" && policy != "none" {
		return ProfileMeta{}, fmt.Errorf("无效的符号链接策略 %q（可选: full / minimal / none）", policy)
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
	mainVer := ReadAppBundleVersion(config.ExpandPath(cfg.Paths.MainAppPath))
	meta := ProfileMeta{
		Name:           name,
		Description:    strings.TrimSpace(opts.Description),
		SymlinkPolicy:  policy,
		InheritedFrom:  opts.InheritFrom,
		CreatedAt:      time.Now().Format(time.RFC3339),
		Version:        1,
		LastAppVersion: mainVer,
	}
	if err := writeProfileMeta(profileDir, meta); err != nil {
		return ProfileMeta{}, err
	}
	hardenProfilePerms(profileDir)
	_ = createMacOSLauncherStub(name)
	return meta, nil
}

func inheritSanitizedHostConfig(realHome, tgtHome, tgtData string, inheritMCP bool) {
	mainDataDir, _ := defaultUserDataDirs(realHome)
	srcAppSupport := filepath.Join(mainDataDir, "User")
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

// EnsureMacOSLauncherStub creates or repairs ~/Applications/Antigravity (<name>).app as a native shell bundle.
func EnsureMacOSLauncherStub(name string) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	return createMacOSLauncherStub(name)
}

func resolveCLIBin(realHome string) string {
	if exe, err := os.Executable(); err == nil && filepath.Base(exe) == "agymate" && fileExists(exe) {
		return exe
	}
	localBin := filepath.Join(realHome, ".local", "bin", "agymate")
	if fileExists(localBin) {
		return localBin
	}
	if p, err := exec.LookPath("agymate"); err == nil && p != "" {
		return p
	}
	return localBin
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func createMacOSLauncherStub(name string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	realHome, _ := os.UserHomeDir()
	appsDir := filepath.Join(realHome, "Applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return err
	}
	appPath := filepath.Join(appsDir, fmt.Sprintf("Antigravity (%s).app", name))
	if filepath.Dir(appPath) != appsDir {
		return fmt.Errorf("unsafe launcher bundle path for %q", name)
	}
	if err := os.RemoveAll(appPath); err != nil {
		return err
	}

	contentsDir := filepath.Join(appPath, "Contents")
	macosDir := filepath.Join(contentsDir, "MacOS")
	resDir := filepath.Join(contentsDir, "Resources")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		return err
	}

	iconBlock := ""
	srcIcon := "/Applications/Antigravity.app/Contents/Resources/icon.icns"
	if rawIcon, rerr := os.ReadFile(srcIcon); rerr == nil && len(rawIcon) > 0 {
		if os.MkdirAll(resDir, 0o755) == nil && os.WriteFile(filepath.Join(resDir, "icon.icns"), rawIcon, 0o644) == nil {
			iconBlock = "\t<key>CFBundleIconFile</key>\n\t<string>icon</string>\n"
		}
	}

	cliBin := resolveCLIBin(realHome)
	slug := strings.Trim(nonIDCharsRe.ReplaceAllString(strings.ToLower(name), "-"), "-.")
	if slug == "" {
		slug = "profile"
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>launcher</string>
%s	<key>CFBundleIdentifier</key>
	<string>io.agymate.profile.%s</string>
	<key>CFBundleName</key>
	<string>Antigravity (%s)</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>LSUIElement</key>
	<true/>
</dict>
</plist>
`, iconBlock, slug, name)
	if err := os.WriteFile(filepath.Join(contentsDir, "Info.plist"), []byte(plist), 0o644); err != nil {
		return err
	}

	script := fmt.Sprintf("#!/bin/sh\nexec %s profile launch %s >/dev/null 2>&1 &\n", shellSingleQuote(cliBin), shellSingleQuote(name))
	if err := os.WriteFile(filepath.Join(macosDir, "launcher"), []byte(script), 0o755); err != nil {
		return err
	}
	return nil
}

func resolveAppBinary(cfg config.Config) string {
	mainPath := config.ExpandPath(cfg.Paths.MainAppPath)
	if runtime.GOOS == "darwin" {
		if mainPath != "" {
			bin := filepath.Join(mainPath, "Contents", "MacOS", "Antigravity")
			if fileExists(bin) {
				return bin
			}
		}
		realHome, _ := os.UserHomeDir()
		userApp := filepath.Join(realHome, "Applications", "Antigravity.app", "Contents", "MacOS", "Antigravity")
		if fileExists(userApp) {
			return userApp
		}
		return filepath.Join(mainPath, "Contents", "MacOS", "Antigravity")
	}
	if mainPath != "" && fileExists(mainPath) {
		return mainPath
	}
	for _, cand := range []string{"antigravity", "Antigravity.exe", "Antigravity"} {
		if p, err := exec.LookPath(cand); err == nil && p != "" {
			return p
		}
	}
	return mainPath
}

func buildProfileLaunchArgs(cfg config.Config, resolvedData, workspace string) ([]string, error) {
	args := []string{"--user-data-dir=" + resolvedData}
	if cfg.CDP.Enabled && (cfg.CDP.WindowHUD || cfg.CDP.ZhInject) {
		// Antigravity 2.33+ disables default DevToolsActivePort; port=0 binds an ephemeral localhost CDP port.
		args = append(args, "--remote-debugging-port=0")
	}
	if workspace != "" {
		absWS, err := filepath.Abs(config.ExpandPath(workspace))
		if err != nil || !dirExists(absWS) {
			return nil, fmt.Errorf("工作区目录不存在: %s", workspace)
		}
		args = append(args, absWS)
	}
	return args, nil
}

func resolveIDEBinary(cfg config.Config) string {
	idePath := config.ExpandPath(cfg.Paths.IDEAppPath)
	if runtime.GOOS == "darwin" {
		if idePath != "" {
			for _, name := range []string{"Antigravity IDE", "Antigravity"} {
				bin := filepath.Join(idePath, "Contents", "MacOS", name)
				if fileExists(bin) {
					return bin
				}
			}
		}
	}
	return idePath
}

// LaunchMainApp launches the official Antigravity (or Antigravity IDE) client with an optional
// ephemeral localhost CDP port (--remote-debugging-port=0) to enable the floating HUD and
// built-in Chinese UI localization without modifying the official app bundle.
func LaunchMainApp(cfg config.Config, useIDE bool, workspace string, enableCDP bool) (pid int, alreadyCDP bool, err error) {
	targetKind := KindMain
	targetLabel := "Antigravity"
	appBin := resolveAppBinary(cfg)
	if useIDE {
		targetKind = KindIDE
		targetLabel = "Antigravity IDE"
		appBin = resolveIDEBinary(cfg)
	}
	if appBin == "" || !fileExists(appBin) {
		return 0, false, fmt.Errorf("未找到 %s 可执行文件: %s", targetLabel, appBin)
	}

	var args []string
	if enableCDP {
		args = append(args, "--remote-debugging-port=0")
	}
	if strings.TrimSpace(workspace) != "" {
		absWS, aerr := filepath.Abs(config.ExpandPath(workspace))
		if aerr != nil || !dirExists(absWS) {
			return 0, false, fmt.Errorf("工作区目录不存在: %s", workspace)
		}
		args = append(args, absWS)
	}

	// Respect read-only lifecycle guard: never kill an existing official main/ide window.
	for _, inst := range DiscoverAll(cfg) {
		if inst.ID != targetKind || !inst.Running {
			continue
		}
		if enableCDP && inst.CDPPort == 0 {
			return inst.MainPID, false, fmt.Errorf(
				"%s 当前已在无 CDP 调试口模式下运行 (PID %d)。请先完全退出该客户端（macOS 按 ⌘Q），再运行 `agymate launch` 以开启右上角悬浮窗与中文汉化",
				targetLabel, inst.MainPID,
			)
		}
		if strings.TrimSpace(workspace) == "" {
			_ = FocusWindow(inst)
			return inst.MainPID, inst.CDPPort > 0, nil
		}
		// Existing CDP-enabled instance can open an additional workspace window directly.
		cmd := exec.Command(appBin, args...)
		configureDetachedProcess(cmd)
		if serr := cmd.Start(); serr != nil {
			return inst.MainPID, inst.CDPPort > 0, serr
		}
		_ = cmd.Process.Release()
		return inst.MainPID, inst.CDPPort > 0, nil
	}

	cmd := exec.Command(appBin, args...)
	configureDetachedProcess(cmd)
	if serr := cmd.Start(); serr != nil {
		return 0, false, serr
	}
	launchedPID := cmd.Process.Pid
	_ = cmd.Process.Release()
	return launchedPID, enableCDP, nil
}

// EnsureMainAppLauncherStub creates ~/Applications/Antigravity (AgyMate).app on macOS
// so users can open the official Antigravity client with HUD + Chinese localization via Spotlight/Dock
// without modifying /Applications/Antigravity.app.
func EnsureMainAppLauncherStub(cfg config.Config) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("快捷启动器 .app 仅支持 macOS（其他平台请直接运行 `agymate launch`）")
	}
	realHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	appsDir := filepath.Join(realHome, "Applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return "", err
	}
	appPath := filepath.Join(appsDir, "Antigravity (AgyMate).app")
	if err := os.RemoveAll(appPath); err != nil {
		return "", err
	}
	contentsDir := filepath.Join(appPath, "Contents")
	macosDir := filepath.Join(contentsDir, "MacOS")
	resDir := filepath.Join(contentsDir, "Resources")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		return "", err
	}

	iconBlock := ""
	mainAppDir := config.ExpandPath(cfg.Paths.MainAppPath)
	if mainAppDir == "" {
		mainAppDir = "/Applications/Antigravity.app"
	}
	srcIcon := filepath.Join(mainAppDir, "Contents", "Resources", "icon.icns")
	if rawIcon, rerr := os.ReadFile(srcIcon); rerr == nil && len(rawIcon) > 0 {
		if os.MkdirAll(resDir, 0o755) == nil && os.WriteFile(filepath.Join(resDir, "icon.icns"), rawIcon, 0o644) == nil {
			iconBlock = "\t<key>CFBundleIconFile</key>\n\t<string>icon</string>\n"
		}
	}

	cliBin := resolveCLIBin(realHome)
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>launcher</string>
%s	<key>CFBundleIdentifier</key>
	<string>io.agymate.launcher.main</string>
	<key>CFBundleName</key>
	<string>Antigravity (AgyMate)</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>LSUIElement</key>
	<true/>
</dict>
</plist>
`, iconBlock)
	if err := os.WriteFile(filepath.Join(contentsDir, "Info.plist"), []byte(plist), 0o644); err != nil {
		return "", err
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %s launch >/dev/null 2>&1 &\n", shellSingleQuote(cliBin))
	if err := os.WriteFile(filepath.Join(macosDir, "launcher"), []byte(script), 0o755); err != nil {
		return "", err
	}
	return appPath, nil
}
