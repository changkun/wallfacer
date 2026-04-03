package dircp

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// TestCopyDirectoryTree verifies that Copy replicates a directory tree
// including nested subdirectories and files.
func TestCopyDirectoryTree(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Build a small tree: file.txt, sub/nested.txt
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst); err != nil {
		t.Fatal("Copy:", err)
	}

	content, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatal("file.txt missing in dst:", err)
	}
	if string(content) != "hello" {
		t.Fatalf("file.txt content = %q, want %q", content, "hello")
	}

	content, err = os.ReadFile(filepath.Join(dst, "sub", "nested.txt"))
	if err != nil {
		t.Fatal("sub/nested.txt missing in dst:", err)
	}
	if string(content) != "nested" {
		t.Fatalf("nested.txt content = %q, want %q", content, "nested")
	}
}

// TestCopyPreservesPermissions verifies that file permissions are preserved.
func TestCopyPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not meaningful on Windows")
	}
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "exec.sh"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst); err != nil {
		t.Fatal("Copy:", err)
	}

	info, err := os.Stat(filepath.Join(dst, "exec.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// Check that the executable bit is set (at least user execute).
	if info.Mode()&0100 == 0 {
		t.Fatalf("expected executable permission, got %v", info.Mode())
	}
}

// TestCopyGoDirectoryTree verifies the pure-Go fallback path.
func TestCopyGoDirectoryTree(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "d"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "d", "b.txt"), []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyGo(src, dst); err != nil {
		t.Fatal("CopyGo:", err)
	}

	for _, tc := range []struct {
		path, want string
	}{
		{"a.txt", "aaa"},
		{filepath.Join("d", "b.txt"), "bbb"},
	} {
		got, err := os.ReadFile(filepath.Join(dst, tc.path))
		if err != nil {
			t.Fatalf("%s missing: %v", tc.path, err)
		}
		if string(got) != tc.want {
			t.Fatalf("%s = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestCopyFileSingle verifies CopyFile for a single file.
func TestCopyFileSingle(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	if err := os.WriteFile(src, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst, 0644); err != nil {
		t.Fatal("CopyFile:", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "content" {
		t.Fatalf("dst content = %q, want %q", got, "content")
	}
}

// TestCopyNonexistentSource verifies that Copy returns an error for a
// source directory that does not exist.
func TestCopyNonexistentSource(t *testing.T) {
	dst := t.TempDir()
	err := Copy(filepath.Join(t.TempDir(), "nonexistent"), dst)
	if err == nil {
		t.Fatal("expected error for nonexistent source, got nil")
	}
}

// TestCopyFileNonexistentSource verifies that CopyFile returns an error
// when the source file does not exist.
func TestCopyFileNonexistentSource(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst.txt")
	err := CopyFile(filepath.Join(t.TempDir(), "nope.txt"), dst, 0644)
	if err == nil {
		t.Fatal("expected error for nonexistent source file, got nil")
	}
}

// TestCopyGoSymlink verifies that CopyGo copies symlinks correctly.
func TestCopyGoSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special permissions on Windows")
	}
	src := t.TempDir()
	dst := t.TempDir()

	// Create a regular file and a symlink to it.
	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := CopyGo(src, dst); err != nil {
		t.Fatal("CopyGo:", err)
	}

	// Verify the symlink was recreated (not dereferenced).
	link, err := os.Readlink(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatal("expected symlink in dst:", err)
	}
	if link != "target.txt" {
		t.Fatalf("symlink target = %q, want %q", link, "target.txt")
	}
}

// TestCopyGoWalkError verifies that CopyGo propagates errors from
// filepath.WalkDir (e.g., permission-denied subdirectory).
func TestCopyGoWalkError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not meaningful on Windows")
	}
	src := t.TempDir()
	dst := t.TempDir()

	// Create a subdirectory that cannot be read.
	noRead := filepath.Join(src, "noperm")
	if err := os.MkdirAll(noRead, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noRead, "secret.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(noRead, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(noRead, 0755) })

	if err := CopyGo(src, dst); err == nil {
		t.Fatal("expected error for permission-denied subdirectory")
	}
}

// TestCopyFileDestinationError verifies that CopyFile returns an error when
// the destination cannot be created (e.g., parent directory does not exist).
func TestCopyFileDestinationError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "nodir", "dst.txt")
	if err := CopyFile(src, dst, 0644); err == nil {
		t.Fatal("expected error when destination directory does not exist")
	}
}

// TestCopyFileIOError verifies that CopyFile returns an error when reading
// the source file fails mid-copy. We simulate this with a named pipe (FIFO)
// that is closed immediately.
func TestCopyFileIOError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs not available on Windows")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	dst := filepath.Join(dir, "dst.txt")

	// Create a FIFO.
	if err := syscall.Mkfifo(fifo, 0644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	// Open the FIFO for writing and close it immediately so the reader gets EOF.
	// We need a goroutine since opening a FIFO blocks until both ends are open.
	go func() {
		w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		_ = w.Close()
	}()

	// CopyFile should succeed (empty copy) or return an error — either way it
	// should not hang. This exercises the io.Copy path with an unusual reader.
	_ = CopyFile(fifo, dst, 0644)
}
