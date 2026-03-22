package tail

import (
	"testing"
)

func TestOf(t *testing.T) {
	tests := []struct {
		name string
		s    []int
		n    int
		want []int
	}{
		{name: "nil slice", s: nil, n: 3, want: nil},
		{name: "empty slice", s: []int{}, n: 3, want: []int{}},
		{name: "n=0 no-op", s: []int{1, 2, 3}, n: 0, want: []int{1, 2, 3}},
		{name: "n negative no-op", s: []int{1, 2, 3}, n: -1, want: []int{1, 2, 3}},
		{name: "n greater than len", s: []int{1, 2}, n: 5, want: []int{1, 2}},
		{name: "n equals len", s: []int{1, 2, 3}, n: 3, want: []int{1, 2, 3}},
		{name: "n less than len", s: []int{1, 2, 3, 4, 5}, n: 2, want: []int{4, 5}},
		{name: "n=1", s: []int{10, 20, 30}, n: 1, want: []int{30}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Of(tc.s, tc.n)
			if len(got) != len(tc.want) {
				t.Fatalf("Of(%v, %d) = %v (len %d), want %v (len %d)",
					tc.s, tc.n, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Of(%v, %d)[%d] = %d, want %d",
						tc.s, tc.n, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestOf_SharesBackingArray(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	got := Of(s, 3)
	got[0] = 99
	if s[2] != 99 {
		t.Fatal("Of result should share backing array with original slice")
	}
}
