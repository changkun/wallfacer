package storetest_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rawConstructor matches a package-qualified store.NewFileStore( or
// store.NewStore( call. "storetest.NewFileStore(" does not match because the
// character before "store." is "t", not a boundary.
var rawConstructor = regexp.MustCompile(`(^|[^\w.])store\.(NewFileStore|NewStore)\(`)

// TestNoRawStoreConstructorsInTests enforces that tests build stores through
// this package (storetest.NewFileStore / storetest.NewStore) rather than
// store.NewFileStore / store.NewStore directly. The wrappers register
// t.Cleanup(s.Close), which drains background trace compaction before
// t.TempDir's RemoveAll runs; a raw constructor leaves that goroutine racing
// the cleanup and intermittently fails CI with "directory not empty".
//
// A line that legitimately needs the raw constructor (e.g. asserting a
// construction error) can opt out with a trailing "// storetest:allow"
// comment.
func TestNoRawStoreConstructorsInTests(t *testing.T) {
	root := repoRoot(t)
	internal := filepath.Join(root, "internal")

	var violations []string
	err := filepath.WalkDir(internal, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip this package: its wrappers and their docs legitimately
			// name store.NewFileStore / store.NewStore.
			if d.Name() == "storetest" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// The store package's own internal tests call the unqualified
		// NewFileStore (they cannot import storetest without a cycle); those
		// are not package-qualified so they never match rawConstructor.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !rawConstructor.MatchString(line) {
				continue
			}
			if strings.Contains(line, "// storetest:allow") {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			violations = append(violations, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("found %d raw store constructor(s) in tests; use storetest.NewFileStore/NewStore "+
			"(or add // storetest:allow if a raw store is required):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

// repoRoot walks up from the test's working directory to the module root
// (the directory containing go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test dir")
		}
		dir = parent
	}
}
