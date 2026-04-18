package cli

import (
	"strings"
	"testing"
)

// TestResolveBackendFlag verifies the user-facing → internal backend name
// translation performed by `wallfacer run --backend`.
func TestResolveBackendFlag(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "local"},
		{in: "container", want: "local"},
		{in: "CONTAINER", want: "local"},
		{in: "  container  ", want: "local"},
		{in: "local", want: "local"},
		{in: "host", want: "host"},
		{in: "Host", want: "host"},
		{in: "k8s", wantErr: true},
		{in: "docker", wantErr: true},
	}
	for _, tc := range cases {
		got, err := resolveBackendFlag(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("resolveBackendFlag(%q): expected error, got %q", tc.in, got)
				continue
			}
			if !strings.Contains(err.Error(), tc.in) {
				t.Errorf("resolveBackendFlag(%q): error should cite input; got %v", tc.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveBackendFlag(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveBackendFlag(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}
