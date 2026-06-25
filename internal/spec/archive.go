package spec

import "strings"

// Archived specs live under a parallel specs/.archive/ tree that mirrors the
// live structure. Every reference (tree keys, depends_on, affects, dispatch
// linkage, the UI) uses the LOGICAL path (specs/local/foo.md); only the bytes
// move to the PHYSICAL path (specs/.archive/local/foo.md). These helpers map
// between the two.

// archiveSegment is the directory under specs/ that holds relocated archived
// specs.
const archiveSegment = ".archive"

const specsPrefix = "specs/"
const archivePrefix = specsPrefix + archiveSegment + "/"

// ArchivePath maps a logical spec path to its physical location under
// specs/.archive/. A path already under .archive/, or not under specs/, is
// returned unchanged.
func ArchivePath(logical string) string {
	if strings.HasPrefix(logical, archivePrefix) {
		return logical
	}
	rest, ok := strings.CutPrefix(logical, specsPrefix)
	if !ok {
		return logical
	}
	return archivePrefix + rest
}

// LogicalPath maps a physical specs/.archive/ path back to its logical path. A
// path not under .archive/ is returned unchanged.
func LogicalPath(physical string) string {
	rest, ok := strings.CutPrefix(physical, archivePrefix)
	if !ok {
		return physical
	}
	return specsPrefix + rest
}

// IsArchivedPath reports whether a path lives under the specs/.archive/ tree.
func IsArchivedPath(p string) bool {
	return strings.HasPrefix(p, archivePrefix)
}
