// Package ndjson provides helpers for reading and appending newline-delimited
// JSON (NDJSON/JSONL) files. It consolidates the repeated scanner-based
// read/append patterns found throughout the codebase.
package ndjson

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// config holds optional settings for NDJSON scanning.
type config struct {
	bufInitial int
	bufMax     int
	onError    func(lineNum int, err error)
}

// Option configures NDJSON scanning behavior.
type Option func(*config)

// WithBufferSize sets the initial and maximum scanner buffer sizes.
// By default the bufio.Scanner default (64 KB max) is used. Use this
// when lines may exceed that limit.
func WithBufferSize(initial, max int) Option {
	return func(c *config) {
		c.bufInitial = initial
		c.bufMax = max
	}
}

// WithOnError sets a callback invoked for lines that fail to unmarshal.
// The callback receives the 1-based line number and the unmarshal error.
// By default malformed lines are silently skipped.
func WithOnError(fn func(lineNum int, err error)) Option {
	return func(c *config) {
		c.onError = fn
	}
}

// ReadFile opens path, decodes each JSON line into T, and returns the
// collected results. Empty and whitespace-only lines are skipped.
//
// If path does not exist, ReadFile returns (empty non-nil slice, nil).
func ReadFile[T any](path string, opts ...Option) ([]T, error) {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(f)
	if cfg.bufMax > 0 {
		scanner.Buffer(make([]byte, 0, cfg.bufInitial), cfg.bufMax)
	}

	var results []T
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			if cfg.onError != nil {
				cfg.onError(lineNum, err)
			}
			continue
		}
		results = append(results, v)
	}

	scanErr := scanner.Err()
	if err := f.Close(); err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}
	if results == nil {
		results = []T{}
	}
	return results, nil
}

// ReadFileFunc opens path and decodes each JSON line into T, calling fn
// for every successfully decoded record. Return false from fn to stop
// iteration early. Empty and whitespace-only lines are skipped.
//
// If path does not exist, ReadFileFunc returns nil (no error).
func ReadFileFunc[T any](path string, fn func(T) bool, opts ...Option) error {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	if cfg.bufMax > 0 {
		scanner.Buffer(make([]byte, 0, cfg.bufInitial), cfg.bufMax)
	}

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			if cfg.onError != nil {
				cfg.onError(lineNum, err)
			}
			continue
		}
		if !fn(v) {
			break
		}
	}

	scanErr := scanner.Err()
	if err := f.Close(); err != nil {
		return err
	}
	return scanErr
}

// AppendFile atomically appends a single JSON-encoded record followed by
// a newline to path. The file is created if it does not exist. The write
// uses O_APPEND so concurrent appends of records under 4 KB are atomic
// on Linux.
func AppendFile[T any](path string, record T) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	// Write record + newline in a single call for atomicity.
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
