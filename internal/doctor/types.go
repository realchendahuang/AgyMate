// Package doctor inspects and repairs environment health across Antigravity Main,
// IDE, Antigravity-Box, ParaGravity sandboxes, Hooks, CDP localization, and Sidecar.
package doctor

// CheckResult describes one diagnostic item produced by the 6-point health doctor.
type CheckResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"` // ok | warn | error
	Summary   string `json:"summary"`
	Detail    string `json:"detail,omitempty"`
	Fixable   bool   `json:"fixable"`
	FixAction string `json:"fixAction,omitempty"`
}

// Report aggregates all 6 health checks plus installed Antigravity app bundle versions.
type Report struct {
	CheckedAt   int64         `json:"checkedAt"`
	MainVersion string        `json:"mainVersion,omitempty"`
	BoxVersion  string        `json:"boxVersion,omitempty"`
	IDEVersion  string        `json:"ideVersion,omitempty"`
	OKCount     int           `json:"okCount"`
	WarnCount   int           `json:"warnCount"`
	ErrorCount  int           `json:"errorCount"`
	Checks      []CheckResult `json:"checks"`
}

// FixReport summarizes automated repairs executed by FixSafe.
type FixReport struct {
	Fixed     []string `json:"fixed,omitempty"`
	Skipped   []string `json:"skipped,omitempty"`
	Errors    []string `json:"errors,omitempty"`
	SyncedBox bool     `json:"syncedBox,omitempty"`
}
