package main

import "testing"

func TestGameListName(t *testing.T) {
	for in, want := range map[string]string{
		"Friday Night Halo": "FRIDAY NIGHT HALO",
		"Café Night":        "CAF NIGHT",
		"★★":                "",
	} {
		if got := gameListName(in); got != want {
			t.Fatalf("gameListName(%q) = %q, want %q", in, got, want)
		}
	}
}
