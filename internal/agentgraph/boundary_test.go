package agentgraph_test

import (
	"os/exec"
	"strings"
	"testing"
)

// supportedToposPackages is the set of topos import paths a wallfacer package may
// name directly. The root latere.ai/x/topos is the runtime surface; latere.ai/x/
// topos/graph is the canonical authored-graph model (the shared definition type
// both wallfacer and the control plane serialize), so it is supported surface, not
// an engine internal. Every other subpackage (sandbox, hooks, the engine) stays
// behind the seam.
var supportedToposPackages = map[string]bool{
	"latere.ai/x/topos/graph": true,
}

// TestWallfacerImportsOnlyRootTopos enforces the embeddable boundary: no wallfacer
// package may directly import a topos ENGINE subpackage (latere.ai/x/topos/...).
// Only the root package latere.ai/x/topos and the supported authoring surface (see
// supportedToposPackages) are allowed. This keeps the runtime an implementation
// detail behind the agentgraph seam.
func TestWallfacerImportsOnlyRootTopos(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}} {{range .Imports}}{{.}} {{end}}", "./...").CombinedOutput()
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
			if strings.HasPrefix(imp, "latere.ai/x/topos/") && !supportedToposPackages[imp] { // a subpackage, not the root or a supported surface
				offenders = append(offenders, pkg+" -> "+imp)
			}
		}
	}
	if len(offenders) > 0 {
		t.Errorf("wallfacer packages import topos engine subpackages directly (use the root topos package only):\n%s",
			strings.Join(offenders, "\n"))
	}
}
