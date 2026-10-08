package main

import (
	"context"
	"runtime"

	"halocommunity/internal/release"
)

// version is set at build time (-ldflags "-X main.version=v0.1.0").
var version = "dev"

// UpdateInfo describes a newer release, if there is one.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
}

// Version returns the app version.
func (a *App) Version() string { return version }

// CheckUpdate looks for a newer release of the app on GitHub. Releases that
// carry only OpenLink Server are skipped: a server-only update never asks
// players to update. Dev builds never get a notice.
func (a *App) CheckUpdate() UpdateInfo {
	return UpdateInfo(release.Check(context.Background(), version, release.AppAsset(runtime.GOOS, runtime.GOARCH)))
}
