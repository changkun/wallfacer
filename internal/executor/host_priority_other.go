//go:build !darwin && !linux

package executor

// applyAgentPriority is a no-op on platforms without a supported priority lever
// (Windows and other Unixes). Agent processes run at the default priority.
func applyAgentPriority(_, _ int) {}
