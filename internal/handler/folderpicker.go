package handler

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"latere.ai/x/pkg/httpjson"
)

// folderPickerArgs returns the command that pops the host OS native folder
// chooser and prints the chosen absolute path to stdout, plus whether the
// current platform has one. macOS drives Finder via osascript; Linux uses
// zenity. Pure (no I/O) so the per-platform shape is unit-testable.
func folderPickerArgs(goos, prompt string) (name string, args []string, ok bool) {
	switch goos {
	case "darwin":
		// `choose folder` returns an alias; POSIX path yields the absolute path.
		script := fmt.Sprintf("POSIX path of (choose folder with prompt %q)", prompt)
		return "osascript", []string{"-e", script}, true
	case "linux":
		return "zenity", []string{"--file-selection", "--directory", "--title", prompt}, true
	default:
		return "", nil, false
	}
}

// PickFolder opens the host's native folder chooser and returns the picked
// absolute path: {"path": "/abs"} on success, {"cancelled": true} when the user
// dismisses the dialog, or 501 when no native picker is available (unknown
// platform or the picker binary is missing) so the frontend can fall back to
// the in-app directory browser.
func (h *Handler) PickFolder(w http.ResponseWriter, r *http.Request) {
	name, args, ok := folderPickerArgs(runtime.GOOS, "Select a folder for this workspace")
	if !ok {
		http.Error(w, "native folder picker not available on this platform", http.StatusNotImplemented)
		return
	}
	if _, err := exec.LookPath(name); err != nil {
		http.Error(w, "native folder picker not installed", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		// A non-zero exit is overwhelmingly the user cancelling the dialog
		// (osascript -128, zenity 1). Either way there is no path to return;
		// report it as a cancel so the frontend simply does nothing.
		httpjson.Write(w, http.StatusOK, map[string]any{"cancelled": true})
		return
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		httpjson.Write(w, http.StatusOK, map[string]any{"cancelled": true})
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"path": path})
}
