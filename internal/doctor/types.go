// Package doctor inspects and repairs environment health across official Antigravity clients,
// isolated profiles, Hooks, CDP integration, and Sidecar.
package doctor

// CheckResult describes one diagnostic item produced by the local health doctor.
type CheckResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"` // ok | warn | error
	Summary   string `json:"summary"`
	Detail    string `json:"detail,omitempty"`
	Fixable   bool   `json:"fixable"`
	FixAction string `json:"fixAction,omitempty"`
}

// Report aggregates all health checks plus installed Antigravity client versions.
type Report struct {
	CheckedAt   int64         `json:"checkedAt"`
	MainVersion string        `json:"mainVersion,omitempty"`
	IDEVersion  string        `json:"ideVersion,omitempty"`
	OKCount     int           `json:"okCount"`
	WarnCount   int           `json:"warnCount"`
	ErrorCount  int           `json:"errorCount"`
	Checks      []CheckResult `json:"checks"`
}

// FixReport summarizes automated repairs executed by FixSafe.
type FixReport struct {
	Fixed   []string `json:"fixed,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

func localizeAuthState(state string) string {
	switch state {
	case "ok":
		return "正常"
	case "missing":
		return "未登录"
	case "expired":
		return "已过期"
	case "no_refresh_token":
		return "缺少刷新令牌"
	case "keychain_managed":
		return "系统钥匙串托管"
	default:
		if state == "" {
			return "未知"
		}
		return state
	}
}
