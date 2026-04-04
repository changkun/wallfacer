package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// UpdateFrontmatter updates specific fields in a spec file's YAML frontmatter
// without disturbing the markdown body or field ordering. The updates map keys
// are YAML field names (e.g., "status", "dispatched_task_id", "updated").
//
// Supported value types:
//   - nil: serialized as YAML null
//   - string: plain scalar
//   - *string: nil → null, non-nil → plain scalar
//   - time.Time: formatted as YYYY-MM-DD
//   - Date: formatted as YYYY-MM-DD
//   - other: fmt.Sprint fallback
//
// The write is atomic (temp file + rename).
func UpdateFrontmatter(path string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read spec %s: %w", path, err)
	}

	content := string(data)

	// Split into frontmatter and body using the same logic as parse.go.
	if !strings.HasPrefix(content, "---\n") {
		return errors.New("missing frontmatter: file must start with ---")
	}

	rest := content[4:] // skip opening "---\n"
	end := strings.Index(rest, "\n---\n")
	bodyStart := end + 4 // skip "\n---\n"
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - 3
			bodyStart = len(rest) + 1 // past the closing "---", no body
		} else {
			return errors.New("missing frontmatter: no closing --- delimiter")
		}
	}

	fmText := rest[:end]
	body := ""
	if bodyStart <= len(rest) {
		body = rest[bodyStart:]
	}

	// Parse frontmatter into yaml.Node to preserve ordering.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fmText), &doc); err != nil {
		return fmt.Errorf("parse frontmatter: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return errors.New("unexpected YAML structure: expected document node")
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return errors.New("unexpected YAML structure: expected mapping node")
	}

	// Apply updates.
	for key, val := range updates {
		valNode := toYAMLNode(val)
		if !updateMappingKey(mapping, key, valNode) {
			// Key not found — append.
			mapping.Content = append(mapping.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: key},
				valNode,
			)
		}
	}

	// Re-serialize the frontmatter.
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("serialize frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close yaml encoder: %w", err)
	}

	// yaml.Encoder produces a trailing "...\n" document end marker; strip it.
	fmOut := buf.String()
	fmOut = strings.TrimSuffix(fmOut, "...\n")
	fmOut = strings.TrimRight(fmOut, "\n")

	// Reassemble the file.
	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(fmOut)
	out.WriteString("\n---\n")
	out.WriteString(body)

	// Atomic write: temp file + rename.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".spec-update-*.md")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(out.String()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	// Preserve original file permissions.
	info, err := os.Stat(path)
	if err == nil {
		_ = os.Chmod(tmpName, info.Mode())
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// updateMappingKey finds a key in a YAML mapping node and replaces its value.
// Returns true if the key was found and updated.
func updateMappingKey(mapping *yaml.Node, key string, val *yaml.Node) bool {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = val
			return true
		}
	}
	return false
}

// toYAMLNode converts a Go value to a yaml.Node scalar suitable for
// frontmatter fields.
func toYAMLNode(v any) *yaml.Node {
	if v == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}

	switch val := v.(type) {
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val}
	case *string:
		if val == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: *val}
	case time.Time:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val.Format(time.DateOnly)}
	case Date:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val.Format(time.DateOnly)}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprint(val)}
	}
}
