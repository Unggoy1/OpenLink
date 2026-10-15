package api

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// OpenLink Launcher (native/launcher) joins through the OpenLink Server proxy and declares its
// protocol level in C++, so it must match AppProtocol; otherwise players with the launcher would
// join servers they cannot use, or be told to update for no reason.
func TestLauncherAppProtocol(t *testing.T) {
	src, err := os.ReadFile("../../native/launcher/relay.h")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`constexpr int32_t kAppProtocol=(\d+);`).FindSubmatch(src)
	if m == nil {
		t.Fatal("kAppProtocol not found in native/launcher/relay.h")
	}
	if got, _ := strconv.Atoi(string(m[1])); got != AppProtocol {
		t.Fatalf("native/launcher kAppProtocol = %d, api.AppProtocol = %d; keep them equal", got, AppProtocol)
	}
}
