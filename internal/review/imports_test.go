package review_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoPackageImportsToposAdversarial holds the review to its own loop: no
// wallfacer package, test imports included, imports the runtime module's
// adversarial debate engine (latere.ai/x/topos/adversarial or a subpackage).
// The review runs on the runner's review role instead, and the rebuilt runtime
// module has no such package to import.
func TestNoPackageImportsToposAdversarial(t *testing.T) {
	const format = "{{.ImportPath}} {{range .Imports}}{{.}} {{end}}{{range .TestImports}}{{.}} {{end}}{{range .XTestImports}}{{.}} {{end}}"
	out, err := exec.Command("go", "list", "-f", format, "latere.ai/x/wallfacer/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	const engine = "latere.ai/x/topos/adversarial"
	var offenders []string
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for _, imp := range fields[1:] {
			if imp == engine || strings.HasPrefix(imp, engine+"/") {
				offenders = append(offenders, fields[0]+" -> "+imp)
			}
		}
	}
	if len(offenders) > 0 {
		t.Errorf("wallfacer packages import the topos adversarial engine:\n%s", strings.Join(offenders, "\n"))
	}
}
