package github

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFileStore_SaveLoadRoundTrip(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	ctx := context.Background()
	p := Principal{OrgID: "org-1", Sub: "user-1"}
	want := &Token{
		AccessToken:    "ghu_access",
		RefreshToken:   "ghr_refresh",
		Expiry:         time.Now().Add(time.Hour).Round(time.Second),
		Login:          "octocat",
		InstallationID: 42,
		Account:        "latere",
		Permissions:    []string{"contents", "pull_requests"},
	}

	if err := store.Save(ctx, p, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(ctx, p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load returned nil token after Save")
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken ||
		got.Login != want.Login || got.InstallationID != want.InstallationID ||
		got.Account != want.Account || !got.Expiry.Equal(want.Expiry) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
	if len(got.Permissions) != 2 || got.Permissions[0] != "contents" {
		t.Errorf("permissions not preserved: %v", got.Permissions)
	}
}

func TestFileStore_LoadMissingReturnsNilNil(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	got, err := store.Load(context.Background(), Principal{OrgID: "o", Sub: "absent"})
	if err != nil {
		t.Fatalf("Load of absent token errored: %v", err)
	}
	if got != nil {
		t.Errorf("Load of absent token = %+v, want nil", got)
	}
}

func TestFileStore_ClearIsIdempotent(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	ctx := context.Background()
	p := Principal{OrgID: "o", Sub: "u"}

	// Clear on an empty store is a no-op.
	if err := store.Clear(ctx, p); err != nil {
		t.Fatalf("Clear on empty store: %v", err)
	}
	if err := store.Save(ctx, p, &Token{AccessToken: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Clear(ctx, p); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, err := store.Load(ctx, p)
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if got != nil {
		t.Errorf("token survived Clear: %+v", got)
	}
	// Clearing again stays a no-op.
	if err := store.Clear(ctx, p); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
}

// Two principals that differ only in Sub (or only in OrgID) must not share a
// credential file; this guards the per-principal scoping the cloud story needs.
func TestFileStore_PrincipalScoping(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	ctx := context.Background()
	a := Principal{OrgID: "org", Sub: "alice"}
	b := Principal{OrgID: "org", Sub: "bob"}
	c := Principal{OrgID: "other", Sub: "alice"}

	if err := store.Save(ctx, a, &Token{AccessToken: "alice-tok"}); err != nil {
		t.Fatalf("Save a: %v", err)
	}
	if err := store.Save(ctx, b, &Token{AccessToken: "bob-tok"}); err != nil {
		t.Fatalf("Save b: %v", err)
	}

	got, err := store.Load(ctx, a)
	if err != nil || got == nil || got.AccessToken != "alice-tok" {
		t.Fatalf("principal a load = %+v, err %v", got, err)
	}
	// Same OrgID, different Sub must be isolated.
	got, err = store.Load(ctx, b)
	if err != nil || got == nil || got.AccessToken != "bob-tok" {
		t.Fatalf("principal b load = %+v, err %v", got, err)
	}
	// Same Sub, different OrgID must be absent (never written).
	got, err = store.Load(ctx, c)
	if err != nil {
		t.Fatalf("principal c load err: %v", err)
	}
	if got != nil {
		t.Errorf("cross-org leak: principal c saw %+v", got)
	}
}

func TestFileStore_FilePermsAre0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file mode not meaningful on windows")
	}
	// Point at a not-yet-existing subdir so Save creates it with 0700 (the
	// production path); t.TempDir() itself is pre-created at 0755.
	dir := filepath.Join(t.TempDir(), "github")
	store, _ := NewFileStore(dir)
	p := Principal{OrgID: "o", Sub: "u"}
	if err := store.Save(context.Background(), p, &Token{AccessToken: "secret"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(store.path(p))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credential file mode = %o, want 600", perm)
	}
	// The containing dir must also be private.
	dinfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := dinfo.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("token dir mode = %o, want no group/other access", perm)
	}
}

func TestNewFileStore_RejectsEmptyDir(t *testing.T) {
	if _, err := NewFileStore(""); err == nil {
		t.Error("NewFileStore(\"\") = nil error, want error")
	}
}

func TestSave_RejectsNilToken(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	if err := store.Save(context.Background(), Principal{Sub: "u"}, nil); err == nil {
		t.Error("Save(nil) = nil error, want error")
	}
}

// Save must not leave .tmp turds in the directory on success.
func TestFileStore_NoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	if err := store.Save(context.Background(), Principal{Sub: "u"}, &Token{AccessToken: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
