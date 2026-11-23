package colab

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

const (
	// Public desktop OAuth2 client from Apache-2.0 googlecolab/google-colab-cli.
	defaultOAuthClientID     = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	defaultOAuthClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"
	defaultAuthURL           = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultTokenURL          = "https://oauth2.googleapis.com/token"
	defaultUserInfoURL       = "https://www.googleapis.com/oauth2/v2/userinfo"
	defaultColabBaseURL      = "https://colab.research.google.com"

	oauthFlowTTL   = 10 * time.Minute
	maxHTTPRBodySz = 4 << 20
)

var colabScopes = []string{
	"openid",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/colaboratory",
	"https://www.googleapis.com/auth/drive.file",
}

var errOAuthRevoked = errors.New("oauth credential revoked or invalid_grant")

// StoredAccount persists OAuth2 credentials for one Google Colab account in ~/.config/agymate/colab_accounts.json.
type StoredAccount struct {
	Email        string `json:"email"`
	RefreshToken string `json:"refreshToken"`
	AccessToken  string `json:"accessToken,omitempty"`
	TokenExpiry  int64  `json:"tokenExpiry,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	AddedAt      int64  `json:"addedAt,omitempty"`
}

type pendingOAuthFlow struct {
	CodeVerifier string
	RedirectURI  string
	CreatedAt    time.Time
}

func (c *Collector) getHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: 12 * time.Second}
}

func (c *Collector) getAuthURL() string {
	if c.authURL != "" {
		return c.authURL
	}
	return defaultAuthURL
}

func (c *Collector) getTokenURL() string {
	if c.tokenURL != "" {
		return c.tokenURL
	}
	return defaultTokenURL
}

func (c *Collector) getUserInfoURL() string {
	if c.userInfoURL != "" {
		return c.userInfoURL
	}
	return defaultUserInfoURL
}

func (c *Collector) getColabBaseURL() string {
	if c.colabBaseURL != "" {
		return c.colabBaseURL
	}
	return defaultColabBaseURL
}

// SetEndpoints overrides OAuth/Colab API endpoints and HTTP client (used for testing).
func (c *Collector) SetEndpoints(authURL, tokenURL, userInfoURL, colabBaseURL string, client *http.Client) {
	if authURL != "" {
		c.authURL = authURL
	}
	if tokenURL != "" {
		c.tokenURL = tokenURL
	}
	if userInfoURL != "" {
		c.userInfoURL = userInfoURL
	}
	if colabBaseURL != "" {
		c.colabBaseURL = colabBaseURL
	}
	if client != nil {
		c.httpClient = client
	}
}

func (c *Collector) accountsStorePath() string {
	if c.storePathOverride != "" {
		return c.storePathOverride
	}
	return config.ColabAccountsPath()
}

func resolveOAuthClientCredentials() (clientID, clientSecret string) {
	if id := strings.TrimSpace(os.Getenv("AGYMATE_COLAB_CLIENT_ID")); id != "" {
		return id, strings.TrimSpace(os.Getenv("AGYMATE_COLAB_CLIENT_SECRET"))
	}
	return defaultOAuthClientID, defaultOAuthClientSecret
}

// StartOAuthFlow prepares a PKCE loopback OAuth2 authorization URL pointing at /api/colab/oauth/callback.
func (c *Collector) StartOAuthFlow(listenAddr, loginHint string) (authURL, state string, err error) {
	port := extractPort(listenAddr)
	redirectURI := fmt.Sprintf("http://127.0.0.1:%s/api/colab/oauth/callback", port)
	clientID, _ := resolveOAuthClientCredentials()

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", "", fmt.Errorf("generate oauth state: %w", err)
	}
	state = hex.EncodeToString(stateBytes)

	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return "", "", fmt.Errorf("generate pkce verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	now := c.now()
	c.oauthMu.Lock()
	if c.pendingFlows == nil {
		c.pendingFlows = make(map[string]pendingOAuthFlow)
	}
	for k, v := range c.pendingFlows {
		if now.Sub(v.CreatedAt) > oauthFlowTTL {
			delete(c.pendingFlows, k)
		}
	}
	c.pendingFlows[state] = pendingOAuthFlow{
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		CreatedAt:    now,
	}
	c.oauthMu.Unlock()

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(colabScopes, " "))
	q.Set("access_type", "offline")
	q.Set("prompt", "select_account consent")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if hint := strings.TrimSpace(loginHint); hint != "" {
		q.Set("login_hint", hint)
	}

	return c.getAuthURL() + "?" + q.Encode(), state, nil
}

// CompleteOAuthFlow validates state, exchanges the authorization code for OAuth2 tokens,
// resolves the Google email address, and upserts the credential into colab_accounts.json.
func (c *Collector) CompleteOAuthFlow(ctx context.Context, code, state string) (StoredAccount, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" || state == "" {
		return StoredAccount{}, errors.New("缺少授权 code 或 state 参数")
	}

	c.oauthMu.Lock()
	flow, ok := c.pendingFlows[state]
	if ok {
		delete(c.pendingFlows, state)
	}
	c.oauthMu.Unlock()
	if !ok {
		return StoredAccount{}, errors.New("授权会话已失效或已被使用，请重新发起绑定")
	}
	if c.now().Sub(flow.CreatedAt) > oauthFlowTTL {
		return StoredAccount{}, errors.New("授权已超时，请重新发起绑定")
	}

	clientID, clientSecret := resolveOAuthClientCredentials()
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	form.Set("redirect_uri", flow.RedirectURI)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", flow.CodeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.getTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return StoredAccount{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return StoredAccount{}, fmt.Errorf("交换 OAuth Token 失败: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPRBodySz))

	var tokResp struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		IDToken          string `json:"id_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(bodyBytes, &tokResp); err != nil {
		return StoredAccount{}, fmt.Errorf("解析 OAuth Token 响应失败 (%d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 400 || tokResp.Error != "" {
		msg := tokResp.ErrorDescription
		if msg == "" {
			msg = tokResp.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return StoredAccount{}, fmt.Errorf("Google OAuth 交换失败: %s", msg)
	}
	if tokResp.AccessToken == "" {
		return StoredAccount{}, errors.New("Google OAuth 未返回 access_token")
	}

	email, err := c.resolveUserEmail(ctx, tokResp.AccessToken, tokResp.IDToken)
	if err != nil || !strings.Contains(email, "@") {
		return StoredAccount{}, fmt.Errorf("无法获取登录账号邮箱: %v", err)
	}

	nowUnix := c.now().Unix()
	exp := tokResp.ExpiresIn
	if exp <= 0 {
		exp = 3600
	}

	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()

	accounts, err := c.loadAccountsLocked()
	if err != nil {
		return StoredAccount{}, err
	}
	stored := StoredAccount{
		Email:        email,
		RefreshToken: tokResp.RefreshToken,
		AccessToken:  tokResp.AccessToken,
		TokenExpiry:  nowUnix + exp,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AddedAt:      nowUnix,
	}

	updated := false
	for i := range accounts {
		if strings.EqualFold(accounts[i].Email, email) {
			if stored.RefreshToken == "" {
				stored.RefreshToken = accounts[i].RefreshToken
			}
			if accounts[i].AddedAt > 0 {
				stored.AddedAt = accounts[i].AddedAt
			}
			accounts[i] = stored
			updated = true
			break
		}
	}
	if !updated {
		accounts = append(accounts, stored)
	}
	if err := c.saveAccountsLocked(accounts); err != nil {
		return StoredAccount{}, err
	}

	c.mu.Lock()
	c.last.CheckedAt = 0
	c.mu.Unlock()
	return stored, nil
}

// RemoveAccount removes an OAuth-bound Colab account from colab_accounts.json.
func (c *Collector) RemoveAccount(email string) (bool, error) {
	target := strings.ToLower(strings.TrimSpace(email))
	if target == "" {
		return false, errors.New("账号邮箱不能为空")
	}

	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()

	accounts, err := c.loadAccountsLocked()
	if err != nil {
		return false, err
	}
	kept := make([]StoredAccount, 0, len(accounts))
	removed := false
	for _, acc := range accounts {
		if strings.EqualFold(strings.TrimSpace(acc.Email), target) {
			removed = true
			continue
		}
		kept = append(kept, acc)
	}
	if !removed {
		return false, nil
	}
	if err := c.saveAccountsLocked(kept); err != nil {
		return false, err
	}

	c.mu.Lock()
	c.last.CheckedAt = 0
	delete(c.acctStates, target)
	c.mu.Unlock()
	return true, nil
}

func (c *Collector) loadAccounts() ([]StoredAccount, error) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()
	return c.loadAccountsLocked()
}

func (c *Collector) loadAccountsLocked() ([]StoredAccount, error) {
	p := c.accountsStorePath()
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	var accounts []StoredAccount
	if err := json.Unmarshal(raw, &accounts); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", p, err)
	}
	return accounts, nil
}

func (c *Collector) saveAccountsLocked(accounts []StoredAccount) error {
	p := c.accountsStorePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if accounts == nil {
		accounts = []StoredAccount{}
	}
	raw, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (c *Collector) updateStoredAccountTokens(acc StoredAccount) {
	c.accountsMu.Lock()
	defer c.accountsMu.Unlock()
	accounts, err := c.loadAccountsLocked()
	if err != nil {
		return
	}
	for i := range accounts {
		if strings.EqualFold(accounts[i].Email, acc.Email) {
			accounts[i].AccessToken = acc.AccessToken
			accounts[i].TokenExpiry = acc.TokenExpiry
			if acc.RefreshToken != "" {
				accounts[i].RefreshToken = acc.RefreshToken
			}
			_ = c.saveAccountsLocked(accounts)
			return
		}
	}
}

func (c *Collector) resolveUserEmail(ctx context.Context, accessToken, idToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.getUserInfoURL(), nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+accessToken)
		if resp, rerr := c.getHTTPClient().Do(req); rerr == nil {
			defer resp.Body.Close()
			if resp.StatusCode < 300 {
				var info struct {
					Email string `json:"email"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info) == nil && strings.Contains(info.Email, "@") {
					return strings.TrimSpace(info.Email), nil
				}
			}
		}
	}

	if email := decodeJWTEmailClaim(idToken); strings.Contains(email, "@") {
		return email, nil
	}
	return "", errors.New("userinfo and id_token did not contain email")
}

func decodeJWTEmailClaim(jwtStr string) string {
	parts := strings.Split(strings.TrimSpace(jwtStr), ".")
	if len(parts) < 2 {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(decoded, &claims) == nil {
		return strings.TrimSpace(claims.Email)
	}
	return ""
}

func (c *Collector) ensureValidAccessToken(ctx context.Context, acc StoredAccount, forceRefresh bool) (StoredAccount, error) {
	nowUnix := c.now().Unix()
	if !forceRefresh && acc.AccessToken != "" && acc.TokenExpiry > nowUnix+60 {
		return acc, nil
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return acc, errOAuthRevoked
	}

	defID, defSecret := resolveOAuthClientCredentials()
	clientID := acc.ClientID
	if clientID == "" {
		clientID = defID
	}
	clientSecret := acc.ClientSecret
	if clientSecret == "" {
		clientSecret = defSecret
	}

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("refresh_token", acc.RefreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.getTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return acc, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return acc, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPRBodySz))

	var tokResp struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if jerr := json.Unmarshal(raw, &tokResp); jerr != nil {
		return acc, fmt.Errorf("刷新 OAuth Token 解析失败 (%d): %w", resp.StatusCode, jerr)
	}
	if resp.StatusCode == http.StatusUnauthorized ||
		tokResp.Error == "invalid_grant" ||
		tokResp.Error == "unauthorized_client" ||
		(resp.StatusCode == http.StatusBadRequest && tokResp.Error != "") {
		return acc, fmt.Errorf("%w: %s", errOAuthRevoked, firstNonEmptyStr(tokResp.ErrorDescription, tokResp.Error))
	}
	if resp.StatusCode >= 400 || tokResp.AccessToken == "" {
		return acc, fmt.Errorf("刷新 OAuth Token 失败 (HTTP %d): %s", resp.StatusCode, firstNonEmptyStr(tokResp.ErrorDescription, tokResp.Error))
	}

	exp := tokResp.ExpiresIn
	if exp <= 0 {
		exp = 3600
	}
	acc.AccessToken = tokResp.AccessToken
	acc.TokenExpiry = nowUnix + exp
	if tokResp.RefreshToken != "" {
		acc.RefreshToken = tokResp.RefreshToken
	}
	c.updateStoredAccountTokens(acc)
	return acc, nil
}

func (c *Collector) doColabRequest(ctx context.Context, accessToken, method, relPath string, extraHeaders map[string]string) (int, string, error) {
	base := strings.TrimRight(c.getColabBaseURL(), "/")
	u, err := url.Parse(base + relPath)
	if err != nil {
		return 0, "", err
	}
	q := u.Query()
	q.Set("authuser", "0")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Colab-Client-Agent", "colab-cli")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPRBodySz))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, stripXSSI(string(body)), nil
}

type colabCCUInfoPayload struct {
	CurrentBalance        float64  `json:"currentBalance"`
	ConsumptionRateHourly float64  `json:"consumptionRateHourly"`
	AssignmentsCount      int      `json:"assignmentsCount"`
	EligibleGPUs          []string `json:"eligibleGpus"`
	EligibleTPUs          []string `json:"eligibleTpus"`
}

type colabAssignmentsPayload struct {
	Assignments []struct {
		Accelerator  string          `json:"accelerator"`
		Endpoint     string          `json:"endpoint"`
		Variant      json.RawMessage `json:"variant"`
		MachineShape json.RawMessage `json:"machineShape"`
	} `json:"assignments"`
}

func (c *Collector) probeOAuthAccount(ctx context.Context, allowance float64, acc StoredAccount) Snapshot {
	t0 := time.Now()
	nowUnix := c.now().Unix()
	snap := Snapshot{
		Enabled:          true,
		Installed:        true,
		Account:          acc.Email,
		MonthlyAllowance: allowance,
		HoursUntilEmpty:  -1,
		Source:           "oauth",
		Removable:        true,
		CheckedAt:        nowUnix,
	}

	validAcc, err := c.ensureValidAccessToken(ctx, acc, false)
	if err != nil {
		snap.OK = false
		snap.FetchMs = elapsedMs(t0)
		if errors.Is(err, errOAuthRevoked) {
			snap.Status = "unauthenticated"
			snap.LastError = "OAuth 凭据已失效，请重新登录"
		} else {
			snap.Status = "error"
			snap.LastError = err.Error()
		}
		return snap
	}

	status, body, err := c.doColabRequest(ctx, validAcc.AccessToken, http.MethodGet, "/tun/m/ccu-info", nil)
	if err == nil && status == http.StatusUnauthorized {
		if refreshed, rerr := c.ensureValidAccessToken(ctx, validAcc, true); rerr == nil {
			validAcc = refreshed
			status, body, err = c.doColabRequest(ctx, validAcc.AccessToken, http.MethodGet, "/tun/m/ccu-info", nil)
		} else if errors.Is(rerr, errOAuthRevoked) {
			snap.Status = "unauthenticated"
			snap.LastError = "OAuth 凭据已失效，请重新登录"
			snap.FetchMs = elapsedMs(t0)
			return snap
		}
	}
	if err != nil {
		snap.Status = "error"
		snap.LastError = err.Error()
		snap.FetchMs = elapsedMs(t0)
		return snap
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		snap.Status = "unauthenticated"
		snap.LastError = fmt.Sprintf("Colab 授权失败 (HTTP %d)", status)
		snap.FetchMs = elapsedMs(t0)
		return snap
	}
	if status >= 400 {
		snap.Status = "error"
		snap.LastError = fmt.Sprintf("Colab 接口错误 (HTTP %d)", status)
		snap.FetchMs = elapsedMs(t0)
		return snap
	}

	var ccu colabCCUInfoPayload
	if jerr := json.Unmarshal([]byte(body), &ccu); jerr != nil {
		snap.Status = "error"
		snap.LastError = fmt.Sprintf("解析 Colab CCU 响应失败: %v", jerr)
		snap.FetchMs = elapsedMs(t0)
		return snap
	}

	snap.OK = true
	snap.Status = "authenticated"
	snap.CurrentBalance = ccu.CurrentBalance
	snap.EstimatedValueUSD = ccu.CurrentBalance * ccuRetailUSD
	snap.ConsumptionRateHourly = ccu.ConsumptionRateHourly
	snap.BurnUSDPerHour = ccu.ConsumptionRateHourly * ccuRetailUSD
	if ccu.ConsumptionRateHourly > 0.01 && ccu.CurrentBalance > 0 {
		snap.HoursUntilEmpty = ccu.CurrentBalance / ccu.ConsumptionRateHourly
	}
	for _, g := range ccu.EligibleGPUs {
		if g != "" {
			snap.EligibleGPUs = append(snap.EligibleGPUs, g)
		}
	}
	for _, t := range ccu.EligibleTPUs {
		if t != "" {
			snap.EligibleTPUs = append(snap.EligibleTPUs, t)
		}
	}

	sessions := c.fetchOAuthAssignments(ctx, validAcc)
	snap.Sessions = sessions
	snap.AssignmentsCount = maxInt(ccu.AssignmentsCount, len(sessions))
	for _, sess := range sessions {
		if sess.Orphan {
			snap.OrphanCount++
		}
	}
	snap.FetchMs = elapsedMs(t0)
	return snap
}

func (c *Collector) fetchOAuthAssignments(ctx context.Context, acc StoredAccount) []SessionItem {
	status, body, err := c.doColabRequest(ctx, acc.AccessToken, http.MethodGet, "/tun/m/assignments", nil)
	if err != nil || status >= 400 || body == "" {
		return nil
	}
	var parsed colabAssignmentsPayload
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return nil
	}
	out := make([]SessionItem, 0, len(parsed.Assignments))
	for _, a := range parsed.Assignments {
		if strings.TrimSpace(a.Endpoint) == "" {
			continue
		}
		out = append(out, SessionItem{
			Name:        a.Endpoint,
			Orphan:      false,
			Endpoint:    a.Endpoint,
			Accelerator: a.Accelerator,
			Variant:     parseVariantLabel(a.Variant),
			Shape:       parseShapeLabel(a.MachineShape),
			Account:     acc.Email,
		})
	}
	return out
}

func (c *Collector) nukeOAuthAccount(ctx context.Context, acc StoredAccount) (NukeResult, error) {
	nowUnix := c.now().Unix()
	res := NukeResult{
		CheckedAt: nowUnix,
	}

	validAcc, err := c.ensureValidAccessToken(ctx, acc, false)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}

	sessions := c.fetchOAuthAssignments(ctx, validAcc)
	if len(sessions) == 0 {
		res.OK = true
		return res, nil
	}

	var errMsgs []string
	for _, sess := range sessions {
		epPath := "/tun/m/unassign/" + url.PathEscape(sess.Endpoint)
		status, body, rerr := c.doColabRequest(ctx, validAcc.AccessToken, http.MethodGet, epPath, nil)
		if rerr != nil || status >= 400 {
			res.FailedCount++
			if rerr != nil {
				errMsgs = append(errMsgs, rerr.Error())
			} else {
				errMsgs = append(errMsgs, fmt.Sprintf("GET unassign HTTP %d", status))
			}
			continue
		}
		var tokObj struct {
			Token string `json:"token"`
		}
		if jerr := json.Unmarshal([]byte(body), &tokObj); jerr != nil || tokObj.Token == "" {
			res.FailedCount++
			errMsgs = append(errMsgs, "unassign 未返回 XSRF token")
			continue
		}
		pStatus, _, perr := c.doColabRequest(ctx, validAcc.AccessToken, http.MethodPost, epPath, map[string]string{
			"X-Goog-Colab-Token": tokObj.Token,
		})
		if perr != nil || pStatus >= 400 {
			res.FailedCount++
			if perr != nil {
				errMsgs = append(errMsgs, perr.Error())
			} else {
				errMsgs = append(errMsgs, fmt.Sprintf("POST unassign HTTP %d", pStatus))
			}
			continue
		}
		res.Terminated = append(res.Terminated, sess)
		res.TerminatedCount++
	}

	remaining := c.fetchOAuthAssignments(ctx, validAcc)
	res.RemainingCount = len(remaining)
	if res.RemainingCount == 0 && res.FailedCount == 0 {
		res.OK = true
		return res, nil
	}
	res.OK = res.FailedCount == 0 && res.RemainingCount == 0
	if len(errMsgs) > 0 {
		res.Error = strings.Join(errMsgs, "; ")
		return res, errors.New(res.Error)
	}
	if !res.OK {
		res.Error = fmt.Sprintf("清场未完全成功：失败 %d 台，剩余 %d 台", res.FailedCount, res.RemainingCount)
		return res, errors.New(res.Error)
	}
	return res, nil
}

func stripXSSI(raw string) string {
	s := strings.TrimPrefix(raw, ")]}'\r\n")
	s = strings.TrimPrefix(s, ")]}'\n")
	s = strings.TrimPrefix(s, ")]}'")
	return strings.TrimSpace(s)
}

func parseVariantLabel(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "DEFAULT"
	}
	var num int
	if json.Unmarshal(raw, &num) == nil {
		switch num {
		case 1:
			return "GPU"
		case 2:
			return "TPU"
		default:
			return "DEFAULT"
		}
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		up := strings.ToUpper(strings.TrimSpace(str))
		switch up {
		case "1", "GPU":
			return "GPU"
		case "2", "TPU":
			return "TPU"
		case "0", "DEFAULT", "":
			return "DEFAULT"
		default:
			return up
		}
	}
	return "DEFAULT"
}

func parseShapeLabel(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "Standard"
	}
	var num int
	if json.Unmarshal(raw, &num) == nil {
		if num == 1 {
			return "High-RAM"
		}
		return "Standard"
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		up := strings.ToUpper(strings.TrimSpace(str))
		switch up {
		case "1", "HIGH_RAM", "HIGH-RAM", "HIGHMEM", "HM":
			return "High-RAM"
		}
	}
	return "Standard"
}

func extractPort(listenAddr string) string {
	trimmed := strings.TrimSpace(listenAddr)
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	if idx := strings.IndexByte(trimmed, '/'); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if _, port, err := net.SplitHostPort(trimmed); err == nil {
		if p, perr := strconv.Atoi(port); perr == nil && p > 0 && p <= 65535 {
			return port
		}
	}
	if p, perr := strconv.Atoi(trimmed); perr == nil && p > 0 && p <= 65535 {
		return trimmed
	}
	return "3778"
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
