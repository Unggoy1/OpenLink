package main

import (
	"context"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/hostctl"
)

type nameController interface {
	Closed() bool
	SetName(context.Context, string) (hostctl.Reply, error)
}

// setGameName puts the configured server name into the in-game server list
// (Custom Game → Create Match → Server), which otherwise shows the PC name.
// The game's list holds printable ASCII up to 47 characters, so the name is
// sanitized first; if nothing is left, the PC name stays.
func (a *agent) setGameName(ctx context.Context, controller nameController, retry time.Duration) {
	name := api.GameName(a.cfg.Name)
	if name == "" {
		a.log.Warn("server name has no printable ASCII characters; the in-game server list shows the PC name", "name", a.cfg.Name)
		return
	}
	// A tick that has not run yet answers Pending; retry briefly.
	for attempt := 0; attempt < 10 && ctx.Err() == nil && !controller.Closed(); attempt++ {
		request, cancel := context.WithTimeout(ctx, 4*time.Second)
		reply, err := controller.SetName(request, name)
		cancel()
		switch {
		case err != nil:
			a.log.Warn("in-game server name not set; the list shows the PC name", "err", err)
			return
		case reply.Code == hostctl.CodeOK:
			if name != a.cfg.Name {
				a.log.Info("in-game server name set (shortened or cleaned to printable ASCII)", "name", name, "configured", a.cfg.Name)
			} else {
				a.log.Info("in-game server name set", "name", name)
			}
			return
		case reply.Code != hostctl.CodeNativePending && reply.Code != hostctl.CodeBusy:
			a.log.Warn("in-game server name not set; the list shows the PC name", "code", reply.Code)
			return
		}
		sleep(ctx, retry)
	}
	if ctx.Err() == nil {
		a.log.Warn("in-game server name not set: the server did not answer in time; the list shows the PC name")
	}
}
