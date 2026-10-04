package main

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"

	"halocommunity/connect"
	"halocommunity/internal/api"
	"halocommunity/internal/directory"
	"halocommunity/internal/sim"
)

// TestAppListFavoritesSettings drives the bound App methods against an
// in-process directory with one probe-answering (proxy) host and one plain host.
func TestAppListFavoritesSettings(t *testing.T) {
	t.Setenv("AppData", t.TempDir()) // os.UserConfigDir on Windows
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HICOMM_DIRECTORY", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ts := httptest.NewServer(directory.New(directory.Config{RegisterKey: "k"}))
	defer ts.Close()
	dc := directory.NewClient(ts.URL, "k")

	echo, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	go sim.Echo(ctx, echo)
	port := echo.LocalAddr().(*net.UDPAddr).Port
	proxyHost, _ := dc.Register(ctx, api.RegisterRequest{Name: "Zulu Proxy", Host: "127.0.0.1", Port: port, Build: "b", Region: "eu"})
	plainHost, _ := dc.Register(ctx, api.RegisterRequest{Name: "Alpha Plain", Host: "127.0.0.9", Port: 1343, Build: "b", Region: "us"})
	dc.Heartbeat(ctx, proxyHost.ID, proxyHost.Token, api.Heartbeat{Status: "ready", Players: 3, Proxy: true, Beacon: sim.Beacon()})
	dc.Heartbeat(ctx, plainHost.ID, plainHost.Token, api.Heartbeat{Status: "ready", Players: -1, Beacon: sim.Beacon()})

	a := NewApp()
	if a.GetSettings().Directory != DefaultDirectory || a.GetSettings().Mode != connect.ModeLoopback {
		t.Fatalf("defaults: %+v", a.GetSettings())
	}
	if err := a.SaveSettings(Settings{Directory: "ftp://nope"}); err == nil {
		t.Fatal("non-http directory accepted")
	}
	if err := a.SaveSettings(Settings{Directory: ts.URL + "/", Mode: "bogus"}); err != nil {
		t.Fatal(err)
	}
	if s := a.GetSettings(); s.Directory != ts.URL || s.Mode != connect.ModeLoopback {
		t.Fatalf("settings not normalised: %+v", s)
	}

	list, err := a.ListServers()
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	byName := map[string]ServerView{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if p := byName["Zulu Proxy"]; p.PingMS < 0 || p.Players != 3 {
		t.Fatalf("proxy host should be pinged and report players: %+v", p)
	}
	if p := byName["Alpha Plain"]; p.PingMS != -1 || p.Players != -1 {
		t.Fatalf("plain host must not be pinged: %+v", p)
	}
	if list[0].Name != "Alpha Plain" {
		t.Fatalf("without favourites, alphabetical: %s first", list[0].Name)
	}

	// Favourite the later-sorting server: it moves to the top and survives a settings save.
	if err := a.SetFavorite(byName["Zulu Proxy"].Key, true); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSettings(Settings{Directory: ts.URL, Mode: connect.ModeLoopback}); err != nil {
		t.Fatal(err)
	}
	list, _ = a.ListServers()
	if list[0].Name != "Zulu Proxy" || !list[0].Favorite {
		t.Fatalf("favourite not first: %+v", list[0])
	}
	if reloaded := NewApp().GetSettings(); len(reloaded.Favorites) != 1 || reloaded.Directory != ts.URL {
		t.Fatalf("settings not persisted: %+v", reloaded)
	}
	a.SetFavorite(byName["Zulu Proxy"].Key, false)
	if len(a.GetSettings().Favorites) != 0 {
		t.Fatal("unfavourite failed")
	}
}
