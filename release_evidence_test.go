package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestReleaseWorkflowWiresEvidence guards that release.yml keeps the smoke
// evidence flowing into the published release. Since the move to the shared
// latere-ai/ci service-release.yml pipeline, the OUTPUT_MD/TAG/COMMIT plumbing
// and the notes-file append live centrally in that reusable workflow; what
// this repo must still guarantee is that release.yml delegates to it with the
// local smoke script (the generator TestSmokeReleaseEmitsEvidence proves) and
// the secrets it needs. The bug this protects against is the delegation
// silently dropping the smoke_script input or the reusable-workflow call.
func TestReleaseWorkflowWiresEvidence(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	yml := string(data)

	for _, want := range []string{
		// Tag push triggers the release pipeline.
		"tags: ['v*']",
		// The central pipeline owns the evidence wiring (OUTPUT_MD, the
		// release-evidence artifact, and the notes-file append).
		"uses: latere-ai/ci/.github/workflows/service-release.yml@",
		// The pipeline must run this repo's evidence-emitting smoke script.
		"smoke_script: tools/smoke/release.sh",
		// Deploy/smoke/publish credentials reach the reusable workflow.
		"secrets: inherit",
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("release.yml missing evidence wiring: %q", want)
		}
	}
}

// TestSmokeReleaseEmitsEvidence runs the real smoke script against a fake
// production surface and asserts the evidence block it writes carries the
// release identity and smoke result. This proves the generator the workflow
// depends on keeps working end to end.
func TestSmokeReleaseEmitsEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("smoke script is bash; not run on the Windows job")
	}
	for _, bin := range []string{"bash", "curl", "grep"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available: %v", bin, err)
		}
	}

	const asset = "assets/app-deadbeef.js"
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/api/debug/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `<!doctype html><html><head><script src="%s"></script></head><body></body></html>`, asset)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "evidence.md")
	cmd := exec.Command("bash", "tools/smoke/release.sh")
	cmd.Env = append(os.Environ(),
		"BASE_URL="+srv.URL,
		"TAG=v9.9.9-test",
		"COMMIT=abcdef0",
		"BUILD_URL=https://build.example/run/1",
		"DEPLOY_URL=https://deploy.example",
		"OUTPUT_MD="+out,
	)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smoke script failed: %v\n%s", err, combined)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("evidence not written: %v", err)
	}
	evidence := string(body)
	for _, want := range []string{
		"<!-- release-evidence -->",
		"v9.9.9-test",
		"abcdef0",
		asset,
		"/healthz",
	} {
		if !strings.Contains(evidence, want) {
			t.Errorf("evidence missing %q:\n%s", want, evidence)
		}
	}
}
