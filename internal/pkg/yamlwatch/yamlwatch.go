// Package yamlwatch debounces filesystem events on a YAML directory.
//
// Several subsystems (agents, flows, prompts, ...) load YAML
// definitions from a user directory and want to rebuild their
// in-memory registry whenever the contents change. They share the
// same wire shape: watch a single directory, ignore non-YAML events,
// debounce bursts (editors that write-rename or save rapidly), and
// invoke a callback that the owner uses to reload.
//
// Watch consolidates that shape so each consumer is one call instead
// of ~80 lines of fsnotify plumbing.
package yamlwatch

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceInterval is the window over which fsnotify events are
// coalesced before a single onChange call fires. 150ms is short
// enough to feel instant in the UI and long enough to fold the
// burst of events many editors emit during a save.
const debounceInterval = 150 * time.Millisecond

// Watch starts a goroutine that watches dir for .yaml / .yml file
// changes and invokes onChange after a debounce. label is used in
// the package's slog messages so different consumers' logs are
// distinguishable (e.g. "agents", "flows", "prompts").
//
// An empty dir is a no-op: a nil-safe stop function is returned and
// no goroutine is started. A missing directory is created best-effort
// so the watcher can attach; if that fails the error surfaces.
//
// The returned stop function detaches the watcher and stops the
// goroutine. Cancelling ctx also stops the watcher.
func Watch(ctx context.Context, label, dir string, onChange func()) (stop func(), err error) {
	if dir == "" {
		return func() {}, nil
	}
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil && !errors.Is(mkErr, os.ErrExist) {
		return func() {}, mkErr
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return func() {}, err
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return func() {}, err
	}

	wCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer func() { _ = w.Close() }()

		var timer *time.Timer
		trigger := func() {
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounceInterval, func() {
				defer func() {
					if p := recover(); p != nil {
						slog.Error(label+" watch: onChange panic", "panic", p)
					}
				}()
				onChange()
			})
		}

		for {
			select {
			case <-wCtx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case evt, ok := <-w.Events:
				if !ok {
					return
				}
				ext := strings.ToLower(filepath.Ext(evt.Name))
				if ext != ".yaml" && ext != ".yml" && ext != "" {
					continue
				}
				trigger()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Warn(label+" watch: fsnotify error", "dir", dir, "error", err)
			}
		}
	}()
	return cancel, nil
}
