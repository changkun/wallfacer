package tree

import "testing"

func TestNew_Empty(t *testing.T) {
	tr := New[string, int]()
	if len(tr.Roots) != 0 {
		t.Errorf("Roots = %d, want 0", len(tr.Roots))
	}
	if len(tr.All) != 0 {
		t.Errorf("All = %d, want 0", len(tr.All))
	}
}

func TestAdd_Root(t *testing.T) {
	tr := New[string, int]()
	n := tr.Add("a", 1, nil)
	if n.Key != "a" || n.Value != 1 {
		t.Errorf("node = {%v, %v}, want {a, 1}", n.Key, n.Value)
	}
	if n.Parent != nil {
		t.Error("root should have nil parent")
	}
	if !n.IsLeaf {
		t.Error("root with no children should be leaf")
	}
	if n.Depth != 0 {
		t.Errorf("Depth = %d, want 0", n.Depth)
	}
	if len(tr.Roots) != 1 {
		t.Errorf("Roots = %d, want 1", len(tr.Roots))
	}
}

func TestAdd_Child(t *testing.T) {
	tr := New[string, int]()
	tr.Add("parent", 1, nil)
	parentKey := "parent"
	child := tr.Add("child", 2, &parentKey)

	if child.Parent == nil || child.Parent.Key != "parent" {
		t.Error("child.Parent should be parent")
	}
	if child.Depth != 1 {
		t.Errorf("child.Depth = %d, want 1", child.Depth)
	}

	parent, _ := tr.NodeAt("parent")
	if parent.IsLeaf {
		t.Error("parent should not be leaf after adding child")
	}
	if len(parent.Children) != 1 {
		t.Errorf("parent.Children = %d, want 1", len(parent.Children))
	}
}

func TestAdd_DeepNesting(t *testing.T) {
	tr := New[string, string]()
	tr.Add("a", "root", nil)
	aKey := "a"
	tr.Add("b", "mid", &aKey)
	bKey := "b"
	tr.Add("c", "leaf", &bKey)

	c, ok := tr.NodeAt("c")
	if !ok {
		t.Fatal("c not found")
	}
	if c.Depth != 2 {
		t.Errorf("c.Depth = %d, want 2", c.Depth)
	}
	if c.Parent.Key != "b" {
		t.Error("c.Parent should be b")
	}
	if c.Parent.Parent.Key != "a" {
		t.Error("c grandparent should be a")
	}
}

func TestNodeAt(t *testing.T) {
	tr := New[string, int]()
	tr.Add("x", 42, nil)

	n, ok := tr.NodeAt("x")
	if !ok || n.Value != 42 {
		t.Errorf("NodeAt(x) = %v, %v; want 42, true", n, ok)
	}

	_, ok = tr.NodeAt("missing")
	if ok {
		t.Error("NodeAt(missing) should return false")
	}
}

func TestLeaves(t *testing.T) {
	tr := New[string, int]()
	tr.Add("parent", 1, nil)
	parentKey := "parent"
	tr.Add("child1", 2, &parentKey)
	tr.Add("child2", 3, &parentKey)
	tr.Add("solo", 4, nil)

	count := 0
	for n := range tr.Leaves() {
		if !n.IsLeaf {
			t.Errorf("Leaves() yielded non-leaf: %v", n.Key)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("Leaves() yielded %d, want 3", count)
	}
}

func TestLeaves_EarlyBreak(t *testing.T) {
	tr := New[string, int]()
	tr.Add("a", 1, nil)
	tr.Add("b", 2, nil)
	tr.Add("c", 3, nil)

	count := 0
	for range tr.Leaves() {
		count++
		if count == 1 {
			break
		}
	}
	if count != 1 {
		t.Errorf("early break: got %d, want 1", count)
	}
}

func TestWalk(t *testing.T) {
	tr := New[string, int]()
	tr.Add("a", 1, nil)
	aKey := "a"
	tr.Add("b", 2, &aKey)
	tr.Add("c", 3, &aKey)
	tr.Add("d", 4, nil)

	var visited []string
	for n := range tr.Walk() {
		visited = append(visited, n.Key)
	}
	if len(visited) != 4 {
		t.Errorf("Walk visited %d nodes, want 4", len(visited))
	}
}

func TestWalk_EarlyBreak(t *testing.T) {
	tr := New[string, int]()
	tr.Add("a", 1, nil)
	aKey := "a"
	tr.Add("b", 2, &aKey)
	tr.Add("c", 3, &aKey)

	count := 0
	for range tr.Walk() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("early break: got %d, want 2", count)
	}
}

func TestAdd_OrphanParent(t *testing.T) {
	tr := New[string, int]()
	missingKey := "nonexistent"
	n := tr.Add("orphan", 1, &missingKey)

	if n.Parent != nil {
		t.Error("orphan with missing parent should be root")
	}
	if len(tr.Roots) != 1 {
		t.Errorf("Roots = %d, want 1", len(tr.Roots))
	}
}

func TestIntKeys(t *testing.T) {
	tr := New[int, string]()
	tr.Add(1, "root", nil)
	parentKey := 1
	tr.Add(2, "child", &parentKey)

	n, ok := tr.NodeAt(2)
	if !ok || n.Value != "child" {
		t.Error("int-keyed tree lookup failed")
	}
}
