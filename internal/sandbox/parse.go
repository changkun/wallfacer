package sandbox

import (
	"encoding/json"
	"fmt"
	"strings"
)

// containerJSON is used to unmarshal `podman/docker ps --format json` output.
type containerJSON struct {
	ID        string            `json:"Id"`
	Names     json.RawMessage   `json:"Names"`
	Image     string            `json:"Image"`
	State     string            `json:"State"`
	Status    string            `json:"Status"`
	Created   any               `json:"Created"`
	CreatedAt string            `json:"CreatedAt"`
	Labels    map[string]string `json:"Labels"`
}

func (c *containerJSON) name() (string, error) {
	if c.Names == nil {
		return "", nil
	}
	var names []string
	if err := json.Unmarshal(c.Names, &names); err == nil && len(names) > 0 {
		return strings.TrimPrefix(names[0], "/"), nil
	}
	var name string
	if err := json.Unmarshal(c.Names, &name); err == nil {
		return strings.TrimPrefix(name, "/"), nil
	}
	return "", fmt.Errorf("containerJSON.name: cannot decode Names field: %s", c.Names)
}

func (c *containerJSON) createdUnix() int64 {
	if c.Created != nil {
		switch v := c.Created.(type) {
		case float64:
			return int64(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n
			}
		}
	}
	return 0
}

// ParseContainerList parses the JSON output of `ps --format json`, handling
// both Podman (JSON array) and Docker (NDJSON, one object per line) formats.
// Returns parsed entries that can be converted to ContainerInfo via
// ContainerInfoFromParsed.
func ParseContainerList(out []byte) ([]ContainerInfo, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	var raw []containerJSON
	if trimmed[0] == '[' {
		if err := json.Unmarshal(out, &raw); err != nil {
			return nil, fmt.Errorf("parse container list (array): %w", err)
		}
	} else {
		for line := range strings.SplitSeq(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line[0] != '{' {
				continue
			}
			var c containerJSON
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				return nil, fmt.Errorf("parse container list (ndjson line): %w", err)
			}
			raw = append(raw, c)
		}
	}

	result := make([]ContainerInfo, 0, len(raw))
	for _, c := range raw {
		name, nameErr := c.name()
		if nameErr != nil {
			continue
		}

		taskID := ""
		if c.Labels != nil {
			taskID = c.Labels["wallfacer.task.id"]
		}
		if taskID == "" {
			candidate := strings.TrimPrefix(name, "wallfacer-")
			if candidate != name && IsUUID(candidate) {
				taskID = candidate
			}
		}

		result = append(result, ContainerInfo{
			ID:        c.ID,
			Name:      name,
			TaskID:    taskID,
			Image:     c.Image,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: c.createdUnix(),
		})
	}
	return result, nil
}

// IsUUID returns true if s looks like a standard UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
