package apicontract

import (
	"encoding/json"
)

// routeJSON is the JSON representation emitted to docs/internals/api-contract.json.
type routeJSON struct {
	Method      string   `json:"method"`
	Pattern     string   `json:"pattern"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// GenerateContractJSON returns the pretty-printed JSON content for
// docs/internals/api-contract.json. It is deterministic and reflects Routes
// exactly so the staleness test can diff it against the committed file.
func GenerateContractJSON() ([]byte, error) {
	rs := make([]routeJSON, len(Routes))
	for i, r := range Routes {
		rs[i] = routeJSON{
			Method:      r.Method,
			Pattern:     r.Pattern,
			Name:        r.Name,
			Description: r.Description,
			Tags:        r.Tags,
		}
	}
	type contract struct {
		GeneratedFrom string      `json:"generated_from"`
		RouteCount    int         `json:"route_count"`
		Routes        []routeJSON `json:"routes"`
	}
	c := contract{
		GeneratedFrom: "internal/apicontract/routes.go",
		RouteCount:    len(Routes),
		Routes:        rs,
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	// Append a trailing newline for POSIX compliance.
	return append(b, '\n'), nil
}
