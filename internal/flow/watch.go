package flow

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

// Watch starts a goroutine that watches dir for YAML file changes
// and invokes onChange after a short debounce whenever the
// directory contents might have shifted. Returns a cleanup
// function the caller runs on shutdown; cancelling ctx also stops
// the watcher.
//
// Mirrors agents.Watch. The debounce coalesces editor bursts so
// the flow registry isn't rebuilt repeatedly for one edit.
func Watch(ctx context.Context, dir string, onChange func()) (stop func(), err error) {
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

		const debounce = 150 * time.Millisecond
		var timer *time.Timer
		trigger := func() {
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				defer func() {
					if p := recover(); p != nil {
						slog.Error("flow watch: onChange panic", "panic", p)
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
				slog.Warn("flow watch: fsnotify error", "dir", dir, "error", err)
			}
		}
	}()
	return cancel, nil
}
