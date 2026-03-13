package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
)

// --------------------------------------------------------------------------
// fileIndex unit tests
// --------------------------------------------------------------------------

// TestFileIndex_CacheHit verifies that a second call within TTL and with no
// filesystem change does not re-walk the workspace.
func TestFileIndex_CacheHit(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "file1.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	idx := newFileIndex()
	var walkCount atomic.Int32
	idx.buildFn = func(w string) ([]string, time.Time) {
		walkCount.Add(1)
		return buildFiles(w)
	}

	// First call: cold cache → one build.
	files1 := idx.Files(ws)
	if walkCount.Load() != 1 {
		t.Fatalf("expected 1 walk on cold cache, got %d", walkCount.Load())
	}
	if len(files1) == 0 {
		t.Fatal("expected at least one file from first call")
	}

	// Second call: cache is fresh → no additional build.
	files2 := idx.Files(ws)
	if walkCount.Load() != 1 {
		t.Fatalf("expected still 1 walk after cache hit, got %d", walkCount.Load())
	}
	if len(files2) != len(files1) {
		t.Errorf("cache hit returned different file count: got %d, want %d", len(files2), len(files1))
	}
}

// TestFileIndex_InvalidationOnMtimeChange verifies that when a file is added to
// the workspace (changing the directory mtime), stale data is served immediately
// and a background refresh eventually delivers the updated list.
func TestFileIndex_InvalidationOnMtimeChange(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "file1.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pin the directory mtime to a known time 2 seconds in the past so that
	// any subsequent file creation will reliably advance the mtime, even on
	// filesystems with coarse timestamp granularity.
	past := time.Now().Add(-2 * time.Second).Truncate(time.Second)
	if err := os.Chtimes(ws, past, past); err != nil {
		t.Fatal(err)
	}

	idx := newFileIndex()

	// Warm the cache: rootMTime == past.
	files1 := idx.Files(ws)
	if len(files1) != 1 {
		t.Fatalf("expected 1 file initially, got %d: %v", len(files1), files1)
	}

	// Add a second file — directory mtime now advances beyond past.
	if err := os.WriteFile(filepath.Join(ws, "file2.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	// Immediate call: entry is stale (mtime changed) but cached data is served
	// right away; background refresh is triggered.
	stale := idx.Files(ws)
	if len(stale) != 1 {
		t.Errorf("expected stale result with 1 file, got %d: %v", len(stale), stale)
	}

	// Poll until the background refresh completes and the updated list is served.
	deadline := time.Now().Add(5 * time.Second)
	var fresh []string
	for time.Now().Before(deadline) {
		idx.mu.RLock()
		stillRefreshing := idx.refreshing[ws]
		idx.mu.RUnlock()
		if !stillRefreshing {
			fresh = idx.Files(ws)
			if len(fresh) == 2 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(fresh) != 2 {
		t.Errorf("expected 2 files after background refresh, got %d: %v", len(fresh), fresh)
	}
}

// TestFileIndex_TTLExpiry verifies that after TTL has elapsed the cached entry
// is treated as stale: stale data is returned immediately and a background
// refresh is triggered.
func TestFileIndex_TTLExpiry(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "file1.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	fakeNow := time.Now()
	idx := newFileIndex()
	idx.ttl = 30 * time.Second
	idx.now = func() time.Time { return fakeNow }

	var walkCount atomic.Int32
	idx.buildFn = func(w string) ([]string, time.Time) {
		walkCount.Add(1)
		return buildFiles(w)
	}

	// Cold cache: first build.
	idx.Files(ws)
	if walkCount.Load() != 1 {
		t.Fatalf("expected 1 walk, got %d", walkCount.Load())
	}

	// Advance the fake clock past TTL.
	fakeNow = fakeNow.Add(31 * time.Second)

	// Call again: entry is stale due to TTL; stale data returned, background
	// refresh triggered.
	idx.Files(ws)

	// Wait for background goroutine to complete.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if walkCount.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if walkCount.Load() < 2 {
		t.Errorf("expected background refresh after TTL expiry, got %d total walks", walkCount.Load())
	}
}

// TestFileIndex_OnlyOneRefreshInFlight verifies that concurrent stale reads
// trigger only a single background refresh goroutine, not one per caller.
func TestFileIndex_OnlyOneRefreshInFlight(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "file1.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	fakeNow := time.Now()
	idx := newFileIndex()
	idx.ttl = 30 * time.Second
	idx.now = func() time.Time { return fakeNow }

	// Phase 1: warm the cache with the default (non-blocking) buildFn.
	idx.Files(ws)

	// Phase 2: install a counting, gate-blocked buildFn before advancing the
	// clock.  The gate is a channel we close to unblock all blocked builds.
	gate := make(chan struct{})
	var refreshCount atomic.Int32
	idx.buildFn = func(w string) ([]string, time.Time) {
		refreshCount.Add(1)
		<-gate
		return buildFiles(w)
	}

	// Make the cached entry stale.
	fakeNow = fakeNow.Add(31 * time.Second)

	// Launch N concurrent stale reads.
	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx.Files(ws)
		}()
	}

	// Wait for ALL goroutines to finish returning stale data. The background
	// refresh goroutine is still blocked on <-gate, so its refreshCount
	// increment happened before any goroutine returned.
	wg.Wait()

	// While gate is still closed, exactly one build is in progress.
	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 in-flight refresh with gate closed, got %d", got)
	}

	// Broadcast-unblock all blocked builds (close acts as a broadcast).
	close(gate)

	// Wait for the background goroutine to finish.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		idx.mu.RLock()
		r := idx.refreshing[ws]
		idx.mu.RUnlock()
		if !r {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Exactly 1 refresh ran.
	if got := refreshCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 total refresh, got %d", got)
	}
}

// --------------------------------------------------------------------------
// GetFiles handler integration tests (via fileIndex)
// --------------------------------------------------------------------------

// newTestHandlerWithTwoWorkspaces creates a Handler with two separate temp-dir
// workspaces so that multi-workspace behaviour can be exercised.
func newTestHandlerWithTwoWorkspaces(t *testing.T) (*Handler, string, string) {
	t.Helper()
	ws1 := t.TempDir()
	ws2 := t.TempDir()
	configDir := t.TempDir()

	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:    envPath,
		Workspaces: ws1 + " " + ws2,
	})
	t.Cleanup(r.WaitBackground)
	h := NewHandler(s, r, configDir, []string{ws1, ws2}, nil)
	return h, ws1, ws2
}

// TestGetFiles_MaxCapEnforcedAcrossWorkspaces verifies that when the combined
// file count across workspaces exceeds maxFileListSize the response is capped.
func TestGetFiles_MaxCapEnforcedAcrossWorkspaces(t *testing.T) {
	h, ws1, ws2 := newTestHandlerWithTwoWorkspaces(t)

	// Fill each workspace with more than half of maxFileListSize files.
	half := maxFileListSize/2 + 100
	for i := 0; i < half; i++ {
		name := filepath.Join(ws1, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(name, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < half; i++ {
		name := filepath.Join(ws2, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(name, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	files, ok := resp["files"].([]any)
	if !ok {
		t.Fatalf("expected files array, got %v", resp["files"])
	}
	if len(files) > maxFileListSize {
		t.Errorf("expected at most %d files, got %d", maxFileListSize, len(files))
	}
}

// TestGetFiles_PathsPrefixedByBasename verifies that files from multiple
// workspaces are each prefixed with their own workspace basename.
func TestGetFiles_PathsPrefixedByBasename(t *testing.T) {
	h, ws1, ws2 := newTestHandlerWithTwoWorkspaces(t)

	os.WriteFile(filepath.Join(ws1, "a.go"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(ws2, "b.go"), []byte("b"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	files, _ := resp["files"].([]any)

	base1 := filepath.Base(ws1)
	base2 := filepath.Base(ws2)

	var sawBase1, sawBase2 bool
	for _, f := range files {
		s := f.(string)
		if strings.HasPrefix(s, base1+"/") {
			sawBase1 = true
		}
		if strings.HasPrefix(s, base2+"/") {
			sawBase2 = true
		}
	}
	if !sawBase1 {
		t.Errorf("expected files from workspace1 (%s/), got: %v", base1, files)
	}
	if !sawBase2 {
		t.Errorf("expected files from workspace2 (%s/), got: %v", base2, files)
	}
}

// TestGetFiles_ConcurrentSafe verifies that many concurrent GetFiles calls do
// not race or panic (run with -race to exercise the RWMutex paths).
func TestGetFiles_ConcurrentSafe(t *testing.T) {
	h, ws, _ := newTestHandlerWithTwoWorkspaces(t)

	// Populate workspace so the index has something to cache.
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(ws, fmt.Sprintf("f%d.go", i)), []byte("x"), 0644)
	}

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
			w := httptest.NewRecorder()
			h.GetFiles(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		}()
	}
	wg.Wait()
}
