// Package ndjson provides reading and appending for newline-delimited JSON
// (NDJSON/JSONL) files.
//
// Wallfacer uses NDJSON for event sourcing trace files and turn-usage records
// where each line is a self-contained JSON object. This package consolidates the
// repeated pattern of opening a file, scanning lines, and unmarshaling JSON into
// a generic [ReadFile] function, plus an atomic [AppendFile] for concurrent-safe
// record appending. Missing files are treated as empty (not errors).
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [store] for reading and
// writing task event traces and turn usage records. Changes
// to the file format or error handling affect all event sourcing persistence.
//
// # Usage
//
//	events, err := ndjson.ReadFile[Event](tracePath)
//	err = ndjson.AppendFile(tracePath, newEvent)
package ndjson
