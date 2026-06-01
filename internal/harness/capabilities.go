package harness

// Permission scopes what a harness is allowed to do within the cwd.
// Each harness maps it onto its native permission knob — see the
// per-harness documentation for the exact translation.
type Permission int

// Permission values.
const (
	PermissionReadOnly Permission = iota
	PermissionEdit
	PermissionFull
)

// Capabilities is the per-harness feature matrix. The zero value
// reports "nothing supported", so callers can safely test against it.
type Capabilities struct {
	SupportsResume       bool
	SupportsMCP          bool
	SupportsSystemPrompt bool
	EmitsUsage           bool
	EmitsCost            bool
	NeedsTTY             bool
}
