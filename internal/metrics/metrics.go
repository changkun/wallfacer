// Package metrics implements a lightweight Prometheus-compatible metrics
// registry without external dependencies.
//
// It supports:
//   - Labeled counters (push-based, incremented during request handling)
//   - Labeled histograms (push-based, observed during request handling)
//   - Scrape-time gauges (pull-based, computed on each /metrics call)
//
// The text exposition format follows the Prometheus specification:
// https://prometheus.io/docs/instrumenting/exposition_formats/
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
)

// DefaultDurationBuckets are histogram upper bounds (in seconds) suited for
// HTTP request latency instrumentation.
var DefaultDurationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
}

// LabeledValue is a single metric sample with its label set.
// Used by scrape-time gauge collectors to return current values.
type LabeledValue struct {
	Labels map[string]string
	Value  float64
}

// gaugeEntry is a registered scrape-time gauge collector.
type gaugeEntry struct {
	name string
	help string
	fn   func() []LabeledValue
}

// Registry is a lightweight Prometheus-compatible metrics registry.
// All methods are safe for concurrent use.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]*Counter
	histograms map[string]*Histogram
	gauges     []gaugeEntry
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		histograms: make(map[string]*Histogram),
	}
}

// Counter returns the named counter, creating it with the given help text if
// it does not yet exist. Repeated calls with the same name return the same
// Counter regardless of the help text supplied.
func (r *Registry) Counter(name, help string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := newCounter(name, help)
	r.counters[name] = c
	return c
}

// Histogram returns the named histogram, creating it with the given help text
// and bucket upper bounds if it does not yet exist. Buckets are sorted
// automatically; a +Inf bucket is always appended during exposition. Repeated
// calls with the same name return the existing Histogram.
func (r *Registry) Histogram(name, help string, buckets []float64) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.histograms[name]; ok {
		return h
	}
	h := newHistogram(name, help, buckets)
	r.histograms[name] = h
	return h
}

// Gauge registers a scrape-time gauge collector. The function fn is called on
// every WritePrometheus invocation. Each returned LabeledValue becomes one
// time series in the gauge family. Registering multiple gauges with the same
// name is not deduplicated; all will be written.
func (r *Registry) Gauge(name, help string, fn func() []LabeledValue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, gaugeEntry{name: name, help: help, fn: fn})
}

// WritePrometheus writes all registered metrics to w in the Prometheus text
// exposition format. Gauge collectors are evaluated on each call.
// Output order: counters (alphabetical) → histograms (alphabetical) → gauges
// (registration order).
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	cs := make([]*Counter, 0, len(r.counters))
	for _, c := range r.counters {
		cs = append(cs, c)
	}
	hs := make([]*Histogram, 0, len(r.histograms))
	for _, h := range r.histograms {
		hs = append(hs, h)
	}
	gs := make([]gaugeEntry, len(r.gauges))
	copy(gs, r.gauges)
	r.mu.Unlock()

	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	sort.Slice(hs, func(i, j int) bool { return hs[i].name < hs[j].name })

	for _, c := range cs {
		c.writeTo(w)
	}
	for _, h := range hs {
		h.writeTo(w)
	}
	for _, g := range gs {
		vals := g.fn()
		if len(vals) == 0 {
			continue
		}
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n", g.name, g.help)
		_, _ = fmt.Fprintf(w, "# TYPE %s gauge\n", g.name)
		for _, v := range vals {
			writeMetricLine(w, g.name, v.Labels, v.Value)
		}
	}
}

// ---------------------------------------------------------------------------
// Counter
// ---------------------------------------------------------------------------

// Counter is a labeled monotonically-increasing counter metric family.
// All methods are safe for concurrent use.
type Counter struct {
	name string
	help string

	mu  sync.Mutex
	obs map[string]*counterCell // canonical label key → cell
}

type counterCell struct {
	labels map[string]string
	value  uint64
}

func newCounter(name, help string) *Counter {
	return &Counter{
		name: name,
		help: help,
		obs:  make(map[string]*counterCell),
	}
}

// Inc increments the counter for the given label set by 1.
func (c *Counter) Inc(labels map[string]string) {
	c.Add(labels, 1)
}

// Add increments the counter for the given label set by delta.
func (c *Counter) Add(labels map[string]string, delta uint64) {
	key := canonicalLabelKey(labels)
	c.mu.Lock()
	cell, ok := c.obs[key]
	if !ok {
		// Deep-copy labels so the caller can reuse its map safely.
		cp := make(map[string]string, len(labels))
		for k, v := range labels {
			cp[k] = v
		}
		cell = &counterCell{labels: cp}
		c.obs[key] = cell
	}
	cell.value += delta
	c.mu.Unlock()
}

func (c *Counter) writeTo(w io.Writer) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", c.name, c.help)
	_, _ = fmt.Fprintf(w, "# TYPE %s counter\n", c.name)

	c.mu.Lock()
	keys := make([]string, 0, len(c.obs))
	for k := range c.obs {
		keys = append(keys, k)
	}
	// snapshot values while locked
	snapshot := make(map[string]*counterCell, len(c.obs))
	for k, cell := range c.obs {
		snapshot[k] = cell
	}
	c.mu.Unlock()

	sort.Strings(keys)
	for _, k := range keys {
		cell := snapshot[k]
		writeMetricLine(w, c.name, cell.labels, float64(cell.value))
	}
}

// ---------------------------------------------------------------------------
// Histogram
// ---------------------------------------------------------------------------

// Histogram is a labeled histogram metric family using cumulative buckets.
// All methods are safe for concurrent use.
type Histogram struct {
	name    string
	help    string
	buckets []float64 // sorted finite upper bounds (without +Inf)

	mu  sync.Mutex
	obs map[string]*histogramCell // canonical label key → cell
}

type histogramCell struct {
	labels map[string]string
	counts []uint64 // len == len(Histogram.buckets)+1; last slot is +Inf
	sum    float64
	count  uint64
}

func newHistogram(name, help string, buckets []float64) *Histogram {
	bs := make([]float64, len(buckets))
	copy(bs, buckets)
	sort.Float64s(bs)
	return &Histogram{
		name:    name,
		help:    help,
		buckets: bs,
		obs:     make(map[string]*histogramCell),
	}
}

// Observe records a single observation with the given value and label set.
func (h *Histogram) Observe(labels map[string]string, value float64) {
	key := canonicalLabelKey(labels)
	h.mu.Lock()
	cell, ok := h.obs[key]
	if !ok {
		cp := make(map[string]string, len(labels))
		for k, v := range labels {
			cp[k] = v
		}
		cell = &histogramCell{
			labels: cp,
			counts: make([]uint64, len(h.buckets)+1),
		}
		h.obs[key] = cell
	}
	// Cumulative: increment all buckets whose upper bound >= value.
	for i, bound := range h.buckets {
		if value <= bound {
			cell.counts[i]++
		}
	}
	cell.counts[len(h.buckets)]++ // +Inf always incremented
	cell.sum += value
	cell.count++
	h.mu.Unlock()
}

func (h *Histogram) writeTo(w io.Writer) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", h.name, h.help)
	_, _ = fmt.Fprintf(w, "# TYPE %s histogram\n", h.name)

	h.mu.Lock()
	keys := make([]string, 0, len(h.obs))
	for k := range h.obs {
		keys = append(keys, k)
	}
	snapshot := make(map[string]*histogramCell, len(h.obs))
	for k, cell := range h.obs {
		snapshot[k] = cell
	}
	h.mu.Unlock()

	sort.Strings(keys)
	for _, k := range keys {
		cell := snapshot[k]
		// Finite buckets.
		for i, bound := range h.buckets {
			leLabels := labelSetWithLE(cell.labels, formatFloat(bound))
			writeMetricLine(w, h.name+"_bucket", leLabels, float64(cell.counts[i]))
		}
		// +Inf bucket.
		infLabels := labelSetWithLE(cell.labels, "+Inf")
		writeMetricLine(w, h.name+"_bucket", infLabels, float64(cell.counts[len(h.buckets)]))
		// Sum and count.
		writeMetricLine(w, h.name+"_sum", cell.labels, cell.sum)
		writeMetricLine(w, h.name+"_count", cell.labels, float64(cell.count))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// canonicalLabelKey returns a stable string key for a label set by sorting
// key names and joining them. The separator \x00 cannot appear in label names
// or values per the Prometheus data model.
func canonicalLabelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('\x00')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(labels[k])
	}
	return sb.String()
}

// writeMetricLine writes one line in Prometheus text format:
//
//	name{label="value",...} value\n
func writeMetricLine(w io.Writer, name string, labels map[string]string, value float64) {
	_, _ = fmt.Fprint(w, name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		_, _ = fmt.Fprint(w, "{")
		for i, k := range keys {
			if i > 0 {
				_, _ = fmt.Fprint(w, ",")
			}
			_, _ = fmt.Fprintf(w, `%s="%s"`, k, escapeLabel(labels[k]))
		}
		_, _ = fmt.Fprint(w, "}")
	}
	_, _ = fmt.Fprintf(w, " %s\n", formatMetricValue(value))
}

// escapeLabel escapes special characters in label values per the Prometheus
// text format specification: backslash, double-quote, and newline.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// formatMetricValue formats a float64 for Prometheus text exposition.
// Special float values are rendered as +Inf, -Inf, or NaN.
// Integers are rendered without a decimal point; other values use %g.
func formatMetricValue(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return fmt.Sprintf("%g", v)
}

// formatFloat formats a finite bucket boundary for the "le" label.
func formatFloat(v float64) string {
	return formatMetricValue(v)
}

// labelSetWithLE returns a shallow copy of labels with the "le" key added.
func labelSetWithLE(labels map[string]string, le string) map[string]string {
	cp := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		cp[k] = v
	}
	cp["le"] = le
	return cp
}
