package agentgraph_test

import (
	"os/exec"
	"strings"
	"testing"
)

// supportedToposPackages is the set of topos import paths ANY wallfacer package
// may name directly. The root latere.ai/x/topos is the runtime surface;
// latere.ai/x/topos/graph is the canonical authored-graph model (the shared
// definition type both wallfacer and the control plane serialize), so it is
// supported surface, not an engine internal. Every other subpackage (sandbox,
// hooks, the engine) stays behind a seam.
var supportedToposPackages = map[string]bool{
	"latere.ai/x/topos/graph": true,
}

// seamPackages maps a wallfacer package to the topos engine subpackages it is
// the designated seam for. A seam confines an engine import to one package so
// the rest of wallfacer depends on the seam rather than the engine directly.
// internal/toposadv is the sole importer of the topos adversarial engine.
var seamPackages = map[string]map[string]bool{
	"latere.ai/x/wallfacer/internal/toposadv": {
		"latere.ai/x/topos/adversarial":        true,
		"latere.ai/x/topos/adversarial/claude": true,
	},
}

// TestWallfacerImportsOnlyRootTopos enforces the embeddable boundary: no
// wallfacer package may directly import a topos ENGINE subpackage
// (latere.ai/x/topos/...) except the root package, the supported authoring
// surface (supportedToposPackages), or a designated seam (seamPackages). This
// keeps the runtime an implementation detail behind a seam. The whole module is
// scanned by import path so the check does not depend on the test's CWD.
func TestWallfacerImportsOnlyRootTopos(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}} {{range .Imports}}{{.}} {{end}}", "latere.ai/x/wallfacer/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	var offenders []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkg := fields[0]
		for _, imp := range fields[1:] {
			if !strings.HasPrefix(imp, "latere.ai/x/topos/") { // root topos is fine
				continue
			}
			if supportedToposPackages[imp] {
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
