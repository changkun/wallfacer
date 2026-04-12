package spec

// TreeResponse is the JSON response for the spec tree API.
type TreeResponse struct {
	Nodes    []NodeResponse      `json:"nodes"`
	Progress map[string]Progress `json:"progress"`

	// Index describes the workspace's top-level specs/README.md
	// roadmap file when one exists in any mounted workspace. Null
	// otherwise. Distinct from Nodes because the roadmap has no
	// frontmatter, no lifecycle, and no dependency edges — it's a
	// plain-markdown index surface that the explorer pins above
	// the regular spec tree.
	Index *Index `json:"index,omitempty"`
}

// NodeResponse represents a single spec node in the API response.
type NodeResponse struct {
	Path     string   `json:"path"`
	Spec     *Spec    `json:"spec"`
	Children []string `json:"children"`
	IsLeaf   bool     `json:"is_leaf"`
	Depth    int      `json:"depth"`
}

// SerializeTree converts a spec tree into a flat API response with
// progress data for all non-leaf nodes.
func SerializeTree(tree *Tree) TreeResponse {
	var nodes []NodeResponse
	for node := range tree.Walk() {
		if node.Value == nil {
			continue
		}
		var children []string
		for _, child := range node.Children {
			children = append(children, child.Key)
		}
		nodes = append(nodes, NodeResponse{
			Path:     node.Key,
			Spec:     node.Value,
			Children: children,
			IsLeaf:   node.IsLeaf,
			Depth:    node.Depth,
		})
	}

	progress := TreeProgress(tree)

	return TreeResponse{
		Nodes:    nodes,
		Progress: progress,
	}
}
