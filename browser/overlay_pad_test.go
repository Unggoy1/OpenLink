package main

import "testing"

func TestPadChoice(t *testing.T) {
	for _, tc := range []struct {
		prev, cur uint16
		n, want   int
	}{
		{0, padView | padUp, 4, 0},
		{padView, padView | padRight, 4, 1},
		{padView, padView | padDown, 4, 2},
		{padView, padView | padLeft, 4, 3},
		{padView, padView | padLeft, 3, -1},       // only 3 options
		{0, padUp, 4, -1},                         // View not held
		{padView | padUp, padView | padUp, 4, -1}, // held, not newly pressed
		{padUp, padView | padUp, 4, -1},           // D-pad before View: no new press
		{0, padView, 4, -1},
	} {
		if got := padChoice(tc.prev, tc.cur, tc.n); got != tc.want {
			t.Errorf("padChoice(%#x, %#x, %d) = %d, want %d", tc.prev, tc.cur, tc.n, got, tc.want)
		}
	}
}
