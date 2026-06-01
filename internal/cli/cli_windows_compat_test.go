package cli

import (
	"testing"
)

// TestIsWSL validates WSL detection via WSL_DISTRO_NAME and WSL_INTEROP
// environment variables, covering all three cases: neither set, only distro
// set, and only interop set.
func TestIsWSL(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
	if isWSL() {
		t.Error("expected isWSL()=false when no WSL env vars are set")
	}

	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if !isWSL() {
		t.Error("expected isWSL()=true when WSL_DISTRO_NAME is set")
	}

	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "/run/WSL/1_interop")
	if !isWSL() {
		t.Error("expected isWSL()=true when WSL_INTEROP is set")
	}
}
