package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rawCtor matches an unqualified NewFileStore( or NewStore( call (capital N, so
// the lowercase helper names newTestFileStore / newTestStoreBackend never
// match). The store package's own tests cannot import internal/store/storetest
// (import cycle), so they must construct through the local newTestFileStore /
// newTestStoreBackend helpers, which register t.Cleanup(s.Close) and drain
// background trace compaction before t.TempDir cleanup.
var rawCtor = regexp.MustCompile(`(^|[^\w.])(NewFileStore|NewStore)\(`)

// TestNoRawConstructorsInStoreTests is the package-store counterpart of
// storetest.TestNoRawStoreConstructorsInTests: it bans raw NewFileStore /
// NewStore in this package's tests so the trace-compaction drain can't be
// forgotten. helpers_test.go (which defines the wrappers) and this file are
// exempt; a line that genuinely needs the raw form can add // storetest:allow.
func TestNoRawConstructorsInStoreTests(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	var violations []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == "helpers_test.go" || name == "constructor_guard_test.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !rawCtor.MatchString(line) || strings.Contains(line, "// storetest:allow") {
				continue
			}
			violations = append(violations, name+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
		}
	}
	if len(violations) > 0 {
		t.Fatalf("found %d raw store constructor(s); use newTestFileStore/newTestStoreBackend "+
			"(or add // storetest:allow if a raw store is required):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
