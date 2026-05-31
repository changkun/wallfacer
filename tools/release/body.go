// Command release-evidence-body assembles a GitHub release body by
// combining a manual / generated prefix with the smoke evidence block
// produced by tools/smoke/release.sh. It is the single canonical
// composer used by .github/workflows/deploy.yml so that the checked-in
// release template, the generated release notes, and the smoke
// evidence cannot drift apart silently.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Marker is the HTML comment that begins the generated release-evidence
// block in a release body. Anything before it is treated as the
// human-authored prefix; anything from it onward is regenerated each
// release so re-runs replace rather than accumulate.
const Marker = "<!-- release-evidence -->"

// Build composes a release body from prefix (manual notes or
// auto-generated changelog) and evidence (the smoke markdown). Any
// pre-existing release-evidence block in prefix is dropped so a re-run
// replaces it rather than stacking duplicates.
func Build(prefix, evidence string) string {
	if i := strings.Index(prefix, Marker); i >= 0 {
		prefix = prefix[:i]
	}
	prefix = strings.TrimRight(prefix, " \t\r\n")
	evidence = strings.TrimSpace(evidence)
	if prefix == "" {
		return evidence + "\n"
	}
	return prefix + "\n\n" + evidence + "\n"
}

func main() {
	prefixPath := flag.String("prefix", "", "file containing manual notes or generated changelog (use '-' for stdin; missing file is treated as empty)")
	evidencePath := flag.String("evidence", "", "file containing the smoke evidence markdown (required)")
	outPath := flag.String("out", "-", "output path ('-' for stdout)")
	flag.Parse()

	if *evidencePath == "" {
		fmt.Fprintln(os.Stderr, "release-evidence-body: --evidence is required")
		os.Exit(2)
	}

	evidence, err := os.ReadFile(*evidencePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "release-evidence-body: read evidence: %v\n", err)
		os.Exit(1)
	}

	var prefix []byte
	switch *prefixPath {
	case "":
		// empty prefix
	case "-":
		prefix, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "release-evidence-body: read stdin: %v\n", err)
			os.Exit(1)
		}
	default:
		prefix, err = os.ReadFile(*prefixPath)
		if err != nil {
			if !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "release-evidence-body: read prefix: %v\n", err)
				os.Exit(1)
			}
			prefix = nil
		}
	}

	body := Build(string(prefix), string(evidence))
	if *outPath == "-" {
		fmt.Print(body)
		return
	}
	if err := os.WriteFile(*outPath, []byte(body), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "release-evidence-body: write: %v\n", err)
		os.Exit(1)
	}
}
