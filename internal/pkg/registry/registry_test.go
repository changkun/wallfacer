package registry

import "testing"

type item struct {
	Slug    string
	Builtin bool
}

func slugOf(i item) string { return i.Slug }

func TestMergeUnique_OrdersBuiltinsThenUser(t *testing.T) {
	builtins := []item{{Slug: "a"}, {Slug: "b"}}
	user := []item{{Slug: "c"}}
	all, err := MergeUnique("item", builtins, user, slugOf, nil)
	if err != nil {
		t.Fatalf("MergeUnique: %v", err)
	}
	got := []string{all[0].Slug, all[1].Slug, all[2].Slug}
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestMergeUnique_RejectsShadow(t *testing.T) {
	builtins := []item{{Slug: "a"}}
	user := []item{{Slug: "a"}}
	if _, err := MergeUnique("flow", builtins, user, slugOf, nil); err == nil {
		t.Fatal("expected error when user item shadows a built-in slug")
	}
}

func TestMergeUnique_AppliesMarkToBuiltinsOnly(t *testing.T) {
	builtins := []item{{Slug: "a"}}
	user := []item{{Slug: "b"}}
	all, err := MergeUnique("item", builtins, user, slugOf, func(i *item) { i.Builtin = true })
	if err != nil {
		t.Fatalf("MergeUnique: %v", err)
	}
	if !all[0].Builtin {
		t.Error("built-in should be marked")
	}
	if all[1].Builtin {
		t.Error("user item should not be marked")
	}
}

func TestContainsSlug(t *testing.T) {
	items := []item{{Slug: "a"}, {Slug: "b"}}
	if !ContainsSlug(items, "b", slugOf) {
		t.Error("expected b to be found")
	}
	if ContainsSlug(items, "z", slugOf) {
		t.Error("did not expect z to be found")
	}
}
