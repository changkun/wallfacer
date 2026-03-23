package sortedkeys

import "testing"

func TestOf_StringKeys(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	var keys []string
	for k := range Of(m) {
		keys = append(keys, k)
	}
	want := []string{"a", "b", "c"}
	if len(keys) != len(want) {
		t.Fatalf("expected %d keys, got %d", len(want), len(keys))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("keys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestOf_IntKeys(t *testing.T) {
	m := map[int]string{3: "c", 1: "a", 2: "b"}
	var keys []int
	for k := range Of(m) {
		keys = append(keys, k)
	}
	want := []int{1, 2, 3}
	if len(keys) != len(want) {
		t.Fatalf("expected %d keys, got %d", len(want), len(keys))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("keys[%d] = %d, want %d", i, k, want[i])
		}
	}
}

func TestOf_Empty(t *testing.T) {
	count := 0
	for range Of(map[string]int{}) {
		count++
	}
	if count != 0 {
		t.Fatalf("expected 0 iterations, got %d", count)
	}
}

func TestOfMap_KeyValuePairs(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	var keys []string
	var vals []int
	for k, v := range OfMap(m) {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	wantKeys := []string{"a", "b", "c"}
	wantVals := []int{1, 2, 3}
	for i := range keys {
		if keys[i] != wantKeys[i] {
			t.Errorf("key[%d] = %q, want %q", i, keys[i], wantKeys[i])
		}
		if vals[i] != wantVals[i] {
			t.Errorf("val[%d] = %d, want %d", i, vals[i], wantVals[i])
		}
	}
}

func TestOfMap_EarlyBreak(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	count := 0
	for range OfMap(m) {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 iterations before break, got %d", count)
	}
}

func TestOfMap_Empty(t *testing.T) {
	count := 0
	for range OfMap(map[string]int{}) {
		count++
	}
	if count != 0 {
		t.Fatalf("expected 0 iterations, got %d", count)
	}
}

func TestOf_EarlyBreak(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	count := 0
	for range Of(m) {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 iterations before break, got %d", count)
	}
}
