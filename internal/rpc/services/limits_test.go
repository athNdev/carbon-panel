package services

import "testing"

func TestClampLimit(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, defaultListLimit},
		{-1, defaultListLimit},
		{-1000, defaultListLimit},
		{1, 1},
		{50, 50},
		{500, 500},
		{501, maxListLimit},
		{2147483647, maxListLimit},
	}
	for _, tc := range cases {
		if got := clampLimit(tc.in); got != tc.want {
			t.Errorf("clampLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
