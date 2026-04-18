package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/x/wallfacer/internal/runner"
)

// TestGetConfig_HostModeFlag verifies the host_mode field on /api/config
// reflects the runner's HostMode() value so the Settings UI can toggle the
// host-mode warning banner.
func TestGetConfig_HostModeFlag(t *testing.T) {
	cases := []struct {
		name string
		host bool
	}{
		{"container", false},
		{"host", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandlerWithMockRunner(t, &runner.MockRunner{Host: tc.host})

			req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			w := httptest.NewRecorder()
			h.GetConfig(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var resp map[string]any
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got, ok := resp["host_mode"].(bool)
			if !ok {
				t.Fatalf("host_mode missing or not bool: %+v", resp)
			}
			if got != tc.host {
				t.Errorf("host_mode = %v; want %v", got, tc.host)
			}
		})
	}
}
