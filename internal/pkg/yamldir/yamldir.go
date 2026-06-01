// Package yamldir reads YAML definition files from a user directory.
//
// Several subsystems (agents, flows, prompts, ...) load user-authored
// YAML files from a single directory: scan the directory, skip
// non-YAML entries, read each .yaml/.yml file into memory, and hand
// the raw bytes to a domain-specific decoder. yamldir.ReadAll
// consolidates the directory-walk scaffold so each consumer keeps
// only its package-specific decoding logic.
package yamldir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File is one .yaml/.yml entry returned by ReadAll: the absolute
// path on disk and the raw file contents. Path is used by callers
// to format parse-error messages that pin the failure to a file.
type File struct {
	Path string
	Body []byte
}

// ReadAll returns one File per .yaml / .yml entry in dir, in the
// order os.ReadDir returns them. label is used in wrapped error
// messages ("read <label> dir <dir>") so each consumer's logs stay
// distinguishable.
//
// An empty dir is a no-op (returns nil, nil). A non-existent dir is
// not an error either: "no user files yet" is a valid state for
// every consumer of this package. Real read errors are wrapped and
// surfaced.
func ReadAll(label, dir string) ([]File, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s dir %s: %w", label, dir, err)
	}
	var files []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		files = append(files, File{Path: path, Body: body})
	}
	return files, nil
}
