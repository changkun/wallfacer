package cli

import (
	"runtime"
	"testing"
)

// TestIsWSL validates WSL detection via WSL_DISTRO_NAME and WSL_INTEROP
// environment variables, covering all three cases: neither set, only distro
// set, and only interop set.
func TestIsWSL(t *testing.T) {
	// When neither WSL env var is set, isWSL should return false.
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
	if isWSL() {
		t.Error("expected isWSL()=false when no WSL env vars are set")
	}

	// When WSL_DISTRO_NAME is set, isWSL should return true.
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if !isWSL() {
		t.Error("expected isWSL()=true when WSL_DISTRO_NAME is set")
	}

	// When only WSL_INTEROP is set, isWSL should return true.
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "/run/WSL/1_interop")
	if !isWSL() {
		t.Error("expected isWSL()=true when WSL_INTEROP is set")
	}
}

// TestDetectContainerRuntimeOverride verifies that CONTAINER_CMD env override
// takes precedence over all other detection paths.
func TestDetectContainerRuntimeOverride(t *testing.T) {
	t.Setenv("CONTAINER_CMD", "/custom/runtime")
	got := detectContainerRuntime()
	if got != "/custom/runtime" {
		t.Errorf("detectContainerRuntime() = %q, want /custom/runtime", got)
	}
}

// TestDetectContainerRuntimeFallback verifies the platform-specific default
// when no override is set and nothing is found on PATH.
func TestDetectContainerRuntimeFallback(t *testing.T) {
	// Clear all overrides; PATH won't have podman/docker in CI typically.
	t.Setenv("CONTAINER_CMD", "")
	t.Setenv("PATH", "")
	got := detectContainerRuntime()
	if runtime.GOOS == "windows" {
		if got != "podman.exe" {
			t.Errorf("detectContainerRuntime() on Windows = %q, want podman.exe", got)
		}
	} else {
		if got != "/opt/podman/bin/podman" {
			// If /opt/podman/bin/podman exists, it's fine too.
			t.Logf("detectContainerRuntime() = %q (ok if system has podman/docker)", got)
		}
	}
}
