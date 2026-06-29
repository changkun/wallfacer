package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFolderPickerArgs(t *testing.T) {
	t.Run("darwin uses osascript choose folder", func(t *testing.T) {
		name, args, ok := folderPickerArgs("darwin", "Pick one")
		if !ok || name != "osascript" {
			t.Fatalf("darwin: name=%q ok=%v", name, ok)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "choose folder") || !strings.Contains(joined, "POSIX path") {
			t.Errorf("darwin args missing the choose-folder script: %q", joined)
		}
	})
	t.Run("linux uses zenity directory selection", func(t *testing.T) {
		name, args, ok := folderPickerArgs("linux", "Pick one")
		if !ok || name != "zenity" {
			t.Fatalf("linux: name=%q ok=%v", name, ok)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--directory") || !strings.Contains(joined, "--file-selection") {
			t.Errorf("linux args missing directory flags: %q", joined)
		}
	})
	t.Run("unsupported platforms report no picker", func(t *testing.T) {
		for _, goos := range []string{"windows", "plan9", ""} {
			if _, _, ok := folderPickerArgs(goos, "x"); ok {
				t.Errorf("%q should report no native picker", goos)
			}
		}
	})
}

// TestPickFolder_CloudModeUnavailable ensures the endpoint never shells out to a
// GUI on a shared host: cloud mode returns 501 so the frontend falls back to the
// in-app directory browser.
func TestPickFolder_CloudModeUnavailable(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	h.SetCloudMode(true)
	w := httptest.NewRecorder()
	h.PickFolder(w, httptest.NewRequest(http.MethodPost, "/api/workspaces/pick-folder", nil))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("cloud mode: got %d, want 501", w.Code)
	}
}
