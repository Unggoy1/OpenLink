package api

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The OpenLink Launcher (native/launcher) joins through the OpenLink Server proxy like the
// app, so it must declare the same protocol level; otherwise players with the launcher
// would join servers they cannot use, or be told to update for no reason.
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
		t.Fatalf("native/launcher kAppProtocol = %d, api.AppProtocol = %d; update the launcher with the app", got, AppProtocol)
	}
}
