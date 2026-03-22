package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/sandbox"
)

// imagePull tracks an in-progress or recently completed image pull.
type imagePull struct {
	ID      string        `json:"pull_id"`
	Image   string        `json:"image"`
	Sandbox sandbox.Type  `json:"sandbox"`
	Lines   chan string   `json:"-"`
	Done    chan struct{} `json:"-"`
	Err     error         `json:"-"`
	Success bool          `json:"-"`
	DoneAt  time.Time     `json:"-"`
}

// imageStatus describes a single sandbox image.
type imageStatus struct {
	Sandbox sandbox.Type `json:"sandbox"`
	Image   string       `json:"image"`
	Cached  bool         `json:"cached"`
	Size    string       `json:"size,omitempty"`
	Created string       `json:"created,omitempty"`
}

// pullTracker manages active and recently completed image pulls.
type pullTracker struct {
	mu    sync.Mutex
	pulls map[string]*imagePull
}

func newPullTracker() *pullTracker {
	return &pullTracker{pulls: make(map[string]*imagePull)}
}

func (pt *pullTracker) get(id string) *imagePull {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.pulls[id]
}

func (pt *pullTracker) store(p *imagePull) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.pulls[p.ID] = p
}

// activeForImage returns an active pull for the given image, or nil.
func (pt *pullTracker) activeForImage(image string) *imagePull {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	for _, p := range pt.pulls {
		if p.Image == image {
			select {
			case <-p.Done:
				continue // completed
			default:
				return p
			}
		}
	}
	return nil
}

// cleanup removes completed pulls older than the retention duration.
func (pt *pullTracker) cleanup(retention time.Duration) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	cutoff := time.Now().Add(-retention)
	for id, p := range pt.pulls {
		if !p.DoneAt.IsZero() && p.DoneAt.Before(cutoff) {
			delete(pt.pulls, id)
		}
	}
}

// pullProgress describes structured progress for a single pull output line.
type pullProgress struct {
	Line       string `json:"line"`
	Phase      string `json:"phase"` // resolving, copying, manifest, done, error, unknown
	LayersDone int    `json:"layers_done"`
}

// parsePullLine classifies a pull output line into a phase and tracks layer count.
func parsePullLine(line string, prevLayersDone int) pullProgress {
	p := pullProgress{Line: line, LayersDone: prevLayersDone}
	switch {
	case strings.HasPrefix(line, "Trying to pull"):
		p.Phase = "resolving"
	case strings.HasPrefix(line, "Getting image source"):
		p.Phase = "resolving"
	case strings.HasPrefix(line, "Copying blob"):
		p.Phase = "copying"
		p.LayersDone = prevLayersDone + 1
	case strings.HasPrefix(line, "Copying config"):
		p.Phase = "copying"
		p.LayersDone = prevLayersDone + 1
	case strings.HasPrefix(line, "Writing manifest"):
		p.Phase = "manifest"
	case strings.HasPrefix(line, "Storing signatures"):
		p.Phase = "done"
	case strings.HasPrefix(line, "Pull complete:"):
		p.Phase = "done"
	case strings.HasPrefix(line, "error:"):
		p.Phase = "error"
	default:
		p.Phase = "unknown"
	}
	return p
}

// scanLinesOrCR is a bufio.SplitFunc that splits on \n, \r\n, or \r.
// This handles container runtimes that use \r for in-place progress updates.
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			return i + 1, data[:i], nil
		}
		if data[i] == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// GetImageStatus returns the availability of sandbox images.
func (h *Handler) GetImageStatus(w http.ResponseWriter, _ *http.Request) {
	cmd := h.runner.Command()
	claudeImage := h.runner.SandboxImage()
	codexImage := testCodexImage(claudeImage)

	images := []imageStatus{
		h.inspectImage(cmd, sandbox.Claude, claudeImage),
		h.inspectImage(cmd, sandbox.Codex, codexImage),
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"images":            images,
		"container_runtime": cmd,
	})
}

// inspectImage checks whether an image is cached and retrieves metadata.
func (h *Handler) inspectImage(cmd string, sb sandbox.Type, image string) imageStatus {
	status := imageStatus{
		Sandbox: sb,
		Image:   image,
	}
	if cmd == "" || image == "" {
		return status
	}
	out, err := exec.Command(cmd, "images", "--format",
		"{{.Size}}\t{{.CreatedAt}}", image).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return status
	}
	status.Cached = true
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) >= 1 {
		status.Size = parts[0]
	}
	if len(parts) >= 2 {
		status.Created = parts[1]
	}
	return status
}

// DeleteImage removes a cached sandbox image.
func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sandbox string `json:"sandbox"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	sb := sandbox.Default(req.Sandbox)
	claudeImage := h.runner.SandboxImage()
	image := claudeImage
	if sb == sandbox.Codex {
		image = testCodexImage(claudeImage)
	}

	cmd := h.runner.Command()
	if cmd == "" || image == "" {
		http.Error(w, "no container runtime or image configured", http.StatusBadRequest)
		return
	}

	out, err := exec.Command(cmd, "rmi", image).CombinedOutput()
	if err != nil {
		http.Error(w, strings.TrimSpace(string(out)), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"removed": image})
}

// PullImage starts an async image pull.
func (h *Handler) PullImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sandbox string `json:"sandbox"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	sb := sandbox.Default(req.Sandbox)
	claudeImage := h.runner.SandboxImage()
	image := claudeImage
	if sb == sandbox.Codex {
		image = testCodexImage(claudeImage)
	}
	if image == "" {
		http.Error(w, "no sandbox image configured", http.StatusBadRequest)
		return
	}

	cmd := h.runner.Command()
	if cmd == "" {
		http.Error(w, "no container runtime configured", http.StatusBadRequest)
		return
	}

	// Deduplicate: return existing active pull.
	if existing := h.pulls.activeForImage(image); existing != nil {
		writeJSON(w, http.StatusOK, map[string]string{"pull_id": existing.ID})
		return
	}

	p := &imagePull{
		ID:      uuid.New().String(),
		Image:   image,
		Sandbox: sb,
		Lines:   make(chan string, 64),
		Done:    make(chan struct{}),
	}
	h.pulls.store(p)

	go h.runPull(r.Context(), cmd, p)

	writeJSON(w, http.StatusAccepted, map[string]string{"pull_id": p.ID})
}

// runPull executes the container pull and streams output lines to the pull tracker.
func (h *Handler) runPull(_ context.Context, cmd string, p *imagePull) {
	defer func() {
		p.DoneAt = time.Now()
		close(p.Done)
		// Clean up old pulls periodically.
		h.pulls.cleanup(5 * time.Minute)
	}()

	c := exec.Command(cmd, "pull", p.Image)
	stdout, err := c.StdoutPipe()
	if err != nil {
		p.Err = err
		p.Lines <- "error: " + err.Error()
		return
	}
	c.Stderr = c.Stdout // merge stderr into stdout

	if err := c.Start(); err != nil {
		p.Err = err
		p.Lines <- "error: " + err.Error()
		return
	}

	scanner := bufio.NewScanner(stdout)
	// Container runtimes use \r for in-place progress updates.
	// Split on both \n and \r so progress lines stream individually.
	scanner.Split(scanLinesOrCR)
	var layersDone int
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		prog := parsePullLine(line, layersDone)
		layersDone = prog.LayersDone
		data, _ := json.Marshal(prog)
		select {
		case p.Lines <- string(data):
		default:
			// Drop line if consumer is too slow.
		}
	}

	if err := c.Wait(); err != nil {
		p.Err = err
		prog := parsePullLine("error: "+err.Error(), layersDone)
		data, _ := json.Marshal(prog)
		p.Lines <- string(data)
	} else {
		p.Success = true
		prog := parsePullLine("Pull complete: "+p.Image, layersDone)
		data, _ := json.Marshal(prog)
		p.Lines <- string(data)
	}
}

// StreamImagePull streams pull progress via SSE.
func (h *Handler) StreamImagePull(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	pullID := r.URL.Query().Get("pull_id")
	p := h.pulls.get(pullID)
	if p == nil {
		http.Error(w, "unknown pull_id", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-p.Lines:
			if !ok {
				// Channel closed unexpectedly.
				return
			}
			// Lines are already JSON-encoded pullProgress structs.
			if _, err := fmt.Fprintf(w, "event: progress\ndata: %s\n\n", line); err != nil {
				return
			}
			flusher.Flush()
		case <-p.Done:
			// Drain remaining lines.
			for {
				select {
				case line := <-p.Lines:
					if _, err := fmt.Fprintf(w, "event: progress\ndata: %s\n\n", line); err != nil {
						return
					}
				default:
					goto drained
				}
			}
		drained:
			if p.Err != nil {
				data, _ := json.Marshal(map[string]string{"error": p.Err.Error()})
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
			} else {
				data, _ := json.Marshal(map[string]any{"success": true, "image": p.Image})
				_, _ = fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
			}
			flusher.Flush()
			return
		}
	}
}
