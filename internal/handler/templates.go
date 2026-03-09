package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// PromptTemplate is a named reusable prompt fragment.
type PromptTemplate struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// templatesMu protects all reads and writes to the templates.json file.
var templatesMu sync.RWMutex

func (h *Handler) templatesPath() string {
	return filepath.Join(h.configDir, "templates.json")
}

func (h *Handler) loadTemplates() ([]PromptTemplate, error) {
	data, err := os.ReadFile(h.templatesPath())
	if errors.Is(err, os.ErrNotExist) {
		return []PromptTemplate{}, nil
	}
	if err != nil {
		return nil, err
	}
	var templates []PromptTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

func (h *Handler) saveTemplates(templates []PromptTemplate) error {
	path := h.templatesPath()
	raw, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ListTemplates handles GET /api/templates.
// Returns all templates sorted by created_at descending; empty array when file absent.
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	templatesMu.RLock()
	templates, err := h.loadTemplates()
	templatesMu.RUnlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].CreatedAt.After(templates[j].CreatedAt)
	})
	writeJSON(w, http.StatusOK, templates)
}

// CreateTemplate handles POST /api/templates.
// Expects JSON body {name, body}; returns 201 with the created template.
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == "" || req.Body == "" {
		http.Error(w, "name and body are required", http.StatusBadRequest)
		return
	}
	tmpl := PromptTemplate{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Body:      req.Body,
		CreatedAt: time.Now().UTC(),
	}

	templatesMu.Lock()
	defer templatesMu.Unlock()

	templates, err := h.loadTemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	templates = append(templates, tmpl)
	if err := h.saveTemplates(templates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

// DeleteTemplate handles DELETE /api/templates/{id}.
// Returns 404 if not found, 204 on success.
func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	templatesMu.Lock()
	defer templatesMu.Unlock()

	templates, err := h.loadTemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, t := range templates {
		if t.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	templates = append(templates[:idx], templates[idx+1:]...)
	if err := h.saveTemplates(templates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
