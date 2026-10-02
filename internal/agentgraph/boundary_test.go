package agentgraph_test

import (
	"os/exec"
	"strings"
	"testing"
)

// seamPackages maps a wallfacer package to the topos engine subpackages it is
// the designated seam for. A seam confines an engine import to one package so
// the rest of wallfacer depends on the seam rather than the engine directly.
// internal/adversarial is the sole importer of the topos adversarial engine: it
// holds the engine implementations (ReviewVerifier, HarnessCritic, the
// fork-session proposer) and exposes its own Verifier/VerifyInput/VerifyResult
// types, so no other package names an engine type.
var seamPackages = map[string]map[string]bool{
	"latere.ai/x/wallfacer/internal/adversarial": {
		"latere.ai/x/topos/adversarial":        true,
		"latere.ai/x/topos/adversarial/claude": true,
	},
}

// TestWallfacerImportsOnlyRootTopos enforces the embeddable boundary: no
// wallfacer package may directly import a topos subpackage
// (latere.ai/x/topos/...) unless it is the designated seam for that subpackage
// (seamPackages). The root latere.ai/x/topos is the runtime surface and may be
// named anywhere; every subpackage (sandbox, hooks, the engine) stays behind a
// seam. This keeps the runtime an implementation detail. The whole module is
// scanned by import path so the check does not depend on the test's CWD.
//
// Test files are scanned too (TestImports and XTestImports alongside Imports):
// a package whose tests name an engine type has the same coupling to the engine
// as one whose production code does, and the seam is meant to be the only place
// that coupling exists.
func TestWallfacerImportsOnlyRootTopos(t *testing.T) {
	const format = "{{.ImportPath}} {{range .Imports}}{{.}} {{end}}{{range .TestImports}}{{.}} {{end}}{{range .XTestImports}}{{.}} {{end}}"
	out, err := exec.Command("go", "list", "-f", format, "latere.ai/x/wallfacer/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	var offenders []string
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkg := fields[0]
		for _, imp := range fields[1:] {
			if !strings.HasPrefix(imp, "latere.ai/x/topos/") { // root topos is fine
				continue
			}
			if seam := seamPackages[pkg]; seam != nil && seam[imp] {
				continue // this package is the designated seam for imp
			}
			offenders = append(offenders, pkg+" -> "+imp)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("wallfacer packages import topos engine subpackages directly (use the root topos package or a seam):\n%s",
			strings.Join(offenders, "\n"))
	}
}
