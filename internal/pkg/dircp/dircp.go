// Package dircp provides platform-aware directory copying utilities.
//
// Copy tries a native `cp -a` on Unix for speed and falls back to a pure-Go
// recursive walk on failure or on Windows. CopyGo is the pure-Go fallback
// exported for testing or when the native tool is not desired. CopyFile copies
// a single file preserving its permissions.
package dircp

import (
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Copy copies all files and directories from src into dst, preserving
// directory structure. On Unix it tries `cp -a src/. dst` first for speed
// and falls back to a pure-Go walk on failure or on Windows.
func Copy(src, dst string) error {
	if runtime.GOOS != "windows" {
		cmd := exec.Command("cp", "-a", src+"/.", dst)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return CopyGo(src, dst)
}

// CopyGo is a pure-Go recursive directory copy. It walks src and recreates
// the directory tree under dst, copying regular files and symlinks.
func CopyGo(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return CopyFile(path, target, info.Mode())
	})
}

// CopyFile copies a single file from src to dst, creating dst with the given
// permissions. If dst already exists it is truncated.
func CopyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
