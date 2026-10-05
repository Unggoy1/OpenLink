package api

import (
	"strings"
	"testing"
)

func TestGameName(t *testing.T) {
	for in, want := range map[string]string{
		"My Server":                      "My Server",
		"  padded  ":                     "padded",
		"café \U0001F600 club":      "caf  club",
		"tab\tand\nnewline":              "tabandnewline",
		"éè":                   "",
		"   ":                            "",
		strings.Repeat("y", 60):          strings.Repeat("y", MaxGameNameLength),
		strings.Repeat("z", 46) + " end": strings.Repeat("z", 46),
	} {
		if got := GameName(in); got != want {
			t.Fatalf("GameName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidServerName(t *testing.T) {
	for name, want := range map[string]bool{
		"Friday Night Halo": true, strings.Repeat("é", 48): true, " x ": true,
		"": false, "   ": false, strings.Repeat("x", 49): false, "tab\there": false, "del\x7f": false,
	} {
		if got := ValidServerName(name); got != want {
			t.Fatalf("ValidServerName(%q) = %v", name, got)
		}
	}
}
