package main

import (
	"os"
	"strings"
	"testing"
)

func TestMakeBuildCreatesFrontendBeforeGoChecks(t *testing.T) {
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(makefile)

	build := makeTargetLine(t, text, "build")
	assertPrereqOrder(t, build, "frontend-build", "lint")
	assertPrereqOrder(t, build, "frontend-build", "build-binary")

	for _, target := range []string{"build-binary", "lint-go", "test-backend"} {
		line := makeTargetLine(t, text, target)
		if !strings.Contains(line, "frontend-build") {
			t.Fatalf("%s target = %q, want frontend-build prerequisite", target, line)
		}
	}
}

func makeTargetLine(t *testing.T, makefile, target string) string {
	t.Helper()
	prefix := target + ":"
	for _, line := range strings.Split(makefile, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("missing Makefile target %s", target)
	return ""
}

func assertPrereqOrder(t *testing.T, line, before, after string) {
	t.Helper()
	beforeIdx := strings.Index(line, before)
	afterIdx := strings.Index(line, after)
	if beforeIdx < 0 || afterIdx < 0 || beforeIdx > afterIdx {
		t.Fatalf("target line %q: want %s before %s", line, before, after)
	}
}
