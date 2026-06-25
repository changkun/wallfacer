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

func TestGoLintVersionIsPinned(t *testing.T) {
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(makefile)

	if !strings.Contains(text, "GOLANGCI_LINT_VERSION ?= 2.11.3") {
		t.Fatal("Makefile must pin GOLANGCI_LINT_VERSION to 2.11.3")
	}
	lintGo := makeTargetBlock(t, text, "lint-go")
	for _, want := range []string{
		"$(GOLANGCI_LINT) --version",
		"$(GOLANGCI_LINT_VERSION)",
		"$(GOLANGCI_LINT) run ./...",
	} {
		if !strings.Contains(lintGo, want) {
			t.Fatalf("lint-go target missing %q:\n%s", want, lintGo)
		}
	}
	if strings.Contains(lintGo, "go vet") || strings.Contains(lintGo, "latest") {
		t.Fatalf("lint-go target must not use an unpinned fallback:\n%s", lintGo)
	}

	workflow, err := os.ReadFile(".github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "version: v2.11.3") {
		t.Fatal("CI golangci-lint action must pin version v2.11.3")
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

func makeTargetBlock(t *testing.T, makefile, target string) string {
	t.Helper()
	lines := strings.Split(makefile, "\n")
	start := -1
	prefix := target + ":"
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("missing Makefile target %s", target)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func assertPrereqOrder(t *testing.T, line, before, after string) {
	t.Helper()
	beforeIdx := strings.Index(line, before)
	afterIdx := strings.Index(line, after)
	if beforeIdx < 0 || afterIdx < 0 || beforeIdx > afterIdx {
		t.Fatalf("target line %q: want %s before %s", line, before, after)
	}
}
