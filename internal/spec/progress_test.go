package spec

import (
	"testing"
)

func leafNode(status Status) *Node {
	return &Node{
		Value:   &Spec{Status: status},
		IsLeaf: true,
	}
}

func TestNodeProgress_Leaf_Complete(t *testing.T) {
	p := NodeProgress(leafNode(StatusComplete))
	if p.Complete != 1 || p.Total != 1 {
		t.Errorf("got %v, want {1, 1}", p)
	}
}

func TestNodeProgress_Leaf_Incomplete(t *testing.T) {
	p := NodeProgress(leafNode(StatusValidated))
	if p.Complete != 0 || p.Total != 1 {
		t.Errorf("got %v, want {0, 1}", p)
	}
}

func TestNodeProgress_NonLeaf_AllComplete(t *testing.T) {
	parent := &Node{
		Value:   &Spec{Status: StatusValidated},
		IsLeaf: false,
		Children: []*Node{
			leafNode(StatusComplete),
			leafNode(StatusComplete),
			leafNode(StatusComplete),
		},
	}
	p := NodeProgress(parent)
	if p.Complete != 3 || p.Total != 3 {
		t.Errorf("got %v, want {3, 3}", p)
	}
}

func TestNodeProgress_NonLeaf_Mixed(t *testing.T) {
	parent := &Node{
		Value:   &Spec{Status: StatusValidated},
		IsLeaf: false,
		Children: []*Node{
			leafNode(StatusComplete),
			leafNode(StatusComplete),
			leafNode(StatusDrafted),
		},
	}
	p := NodeProgress(parent)
	if p.Complete != 2 || p.Total != 3 {
		t.Errorf("got %v, want {2, 3}", p)
	}
}

func TestNodeProgress_DeepNesting(t *testing.T) {
	// Grandparent has 1 leaf child + 1 non-leaf child with 2 leaves.
	mid := &Node{
		Value:   &Spec{Status: StatusValidated},
		IsLeaf: false,
		Children: []*Node{
			leafNode(StatusComplete),
			leafNode(StatusValidated),
		},
	}
	grandparent := &Node{
		Value:   &Spec{Status: StatusValidated},
		IsLeaf: false,
		Children: []*Node{
			leafNode(StatusComplete),
			mid,
		},
	}
	p := NodeProgress(grandparent)
	if p.Complete != 2 || p.Total != 3 {
		t.Errorf("got %v, want {2, 3}", p)
	}
}

func TestNodeProgress_NoChildren(t *testing.T) {
	node := &Node{
		Value:   &Spec{Status: StatusValidated},
		IsLeaf: false, // non-leaf but no children (edge case)
	}
	p := NodeProgress(node)
	if p.Complete != 0 || p.Total != 0 {
		t.Errorf("got %v, want {0, 0}", p)
	}
}

func TestProgress_Fraction(t *testing.T) {
	tests := []struct {
		p    Progress
		want float64
	}{
		{Progress{2, 3}, 2.0 / 3.0},
		{Progress{0, 5}, 0},
		{Progress{3, 3}, 1.0},
		{Progress{0, 0}, 0},
	}
	for _, tc := range tests {
		got := tc.p.Fraction()
		if got != tc.want {
			t.Errorf("Progress%v.Fraction() = %f, want %f", tc.p, got, tc.want)
		}
	}
}

func TestProgress_String(t *testing.T) {
	p := Progress{2, 3}
	if s := p.String(); s != "2/3 leaves done" {
		t.Errorf("String() = %q, want %q", s, "2/3 leaves done")
	}
}

func TestTreeProgress_FullTree(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/parent.md":          {Status: StatusValidated, Track: TrackLocal},
		"local/parent/a.md":        {Status: StatusComplete, Track: TrackLocal},
		"local/parent/b.md":        {Status: StatusValidated, Track: TrackLocal},
		"local/parent/mid.md":      {Status: StatusValidated, Track: TrackLocal},
		"local/parent/mid/c.md":    {Status: StatusComplete, Track: TrackLocal},
		"local/parent/mid/d.md":    {Status: StatusDrafted, Track: TrackLocal},
		"local/standalone.md":      {Status: StatusComplete, Track: TrackLocal},
	})

	progress := TreeProgress(tree)

	// parent.md: 2 complete out of 4 leaves (a, b, c, d).
	if p, ok := progress["local/parent.md"]; !ok {
		t.Error("missing progress for parent.md")
	} else if p.Complete != 2 || p.Total != 4 {
		t.Errorf("parent progress = %v, want {2, 4}", p)
	}

	// mid.md: 1 complete out of 2 leaves (c, d).
	if p, ok := progress["local/parent/mid.md"]; !ok {
		t.Error("missing progress for mid.md")
	} else if p.Complete != 1 || p.Total != 2 {
		t.Errorf("mid progress = %v, want {1, 2}", p)
	}

	// standalone.md is a leaf — should not be in progress map.
	if _, ok := progress["local/standalone.md"]; ok {
		t.Error("leaf spec should not appear in progress map")
	}
}
