package handler

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// systemPromptResponse is the JSON shape for a single system prompt template.
type systemPromptResponse struct {
	Name        string `json:"name"`
	HasOverride bool   `json:"has_override"`
	Content     string `json:"content"` // user override content if present, else embedded default
}

// ListSystemPrompts returns all 7 built-in prompt templates with their
// current content (user override when present, embedded default otherwise)
// and override status.
func (h *Handler) ListSystemPrompts(w http.ResponseWriter, _ *http.Request) {
	mgr := h.runner.Prompts()
	names := mgr.KnownNames()
	result := make([]systemPromptResponse, 0, len(names))
	for _, name := range names {
		content, hasOverride, err := mgr.Content(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result = append(result, systemPromptResponse{
			Name:        name,
			HasOverride: hasOverride,
			Content:     content,
		})
	}
	httpjson.Write(w, http.StatusOK, result)
}

// GetSystemPrompt returns a single built-in prompt template by name.
// Returns 404 if the name is not in the known set.
func (h *Handler) GetSystemPrompt(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	mgr := h.runner.Prompts()
	content, hasOverride, err := mgr.Content(name)
	if err != nil {
		if isUnknownTemplateName(err) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusOK, systemPromptResponse{
		Name:        name,
		HasOverride: hasOverride,
		Content:     content,
	})
}

// UpdateSystemPrompt writes a user override for the named built-in prompt
// template. The template is validated before writing; an invalid Go template
// returns 422 with the parse error as the body.
func (h *Handler) UpdateSystemPrompt(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	req, ok := httpjson.DecodeBody[struct {
		Content string `json:"content"`
	}](w, r)
	if !ok {
		return
	}
	mgr := h.runner.Prompts()
	if err := mgr.Validate(name, req.Content); err != nil {
		if isUnknownTemplateName(err) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := mgr.WriteOverride(name, req.Content); err != nil {
		if isUnknownTemplateName(err) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// Template parse errors and other write errors return 422.
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteSystemPrompt removes the user override for the named built-in prompt
// template, restoring the embedded default. Returns 404 if no override exists.
func (h *Handler) DeleteSystemPrompt(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	mgr := h.runner.Prompts()
	if err := mgr.DeleteOverride(name); err != nil {
		if isUnknownTemplateName(err) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "no override found for "+name, http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
}

// isUnknownTemplateName reports whether the error was produced by an unknown
// template name lookup (e.g. prompts.Content returned "unknown template name").
// This relies on string matching because the prompts package uses fmt.Errorf
// rather than a sentinel error type. If the prompts package changes its error
// format, this function must be updated.
func isUnknownTemplateName(err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(err.Error(), "unknown template name")
}
