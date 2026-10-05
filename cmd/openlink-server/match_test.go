package main

import (
	"testing"

	"halocommunity/internal/api"
	"halocommunity/internal/playlist"
)

func TestMatchReport(t *testing.T) {
	f, err := playlist.Parse([]byte(playlistDoc(2,
		func(i int) string { return []string{"interference-fiesta", "kusini-ctf"}[i] },
		func(i int) string { return []string{"Fiesta Slayer on Interference", ""}[i] })))
	if err != nil {
		t.Fatal(err)
	}
	fiesta := ballotOption(f.Entries[0])
	ctf := ballotOption(f.Entries[1]) // no name: the ID is shown

	type want struct{ phase, entry string }
	cases := []struct {
		name     string
		voting   bool
		state    int32
		vote     *voteInfo
		rotation *rotationInfo
		want     *want
	}{
		{"rotation lobby shows the next match", false, statePreGame, nil, &rotationInfo{Current: "kusini-ctf"}, &want{api.PhaseLobby, "kusini-ctf"}},
		{"rotation in game", false, stateInGame, nil, &rotationInfo{Current: "interference-fiesta"}, &want{api.PhaseInGame, "interference-fiesta"}},
		{"rotation post game", false, stateEndGame, nil, &rotationInfo{Current: "interference-fiesta"}, &want{api.PhasePostGame, "interference-fiesta"}},
		{"vote waiting in lobby", true, statePreGame, &voteInfo{Phase: "waiting", Current: "kusini-ctf"}, nil, &want{api.PhaseLobby, ""}},
		{"vote open", true, statePreGame, &voteInfo{Phase: "voting", Current: "kusini-ctf"}, nil, &want{api.PhaseVoting, ""}},
		{"vote closed, starting", true, statePreGame, &voteInfo{Phase: "starting", Winner: "interference-fiesta"}, nil, &want{api.PhaseStarting, "interference-fiesta"}},
		{"start requested", true, statePreGame, &voteInfo{Phase: "playing", Current: "interference-fiesta"}, nil, &want{api.PhaseStarting, "interference-fiesta"}},
		{"engine starting", true, stateStarting, &voteInfo{Phase: "playing", Current: "kusini-ctf"}, nil, &want{api.PhaseStarting, "kusini-ctf"}},
		{"vote match in game", true, stateInGame, &voteInfo{Phase: "playing", Current: "kusini-ctf"}, nil, &want{api.PhaseInGame, "kusini-ctf"}},
		{"vote match ended", true, stateEndGame, &voteInfo{Phase: "playing", Current: "kusini-ctf"}, nil, &want{api.PhasePostGame, "kusini-ctf"}},
		{"state not read yet", false, 0, nil, &rotationInfo{Current: "kusini-ctf"}, nil},
	}
	for _, c := range cases {
		a := &agent{status: "ready", playlist: f, lobby: &lobbyInfo{State: c.state}, vote: c.vote, rotation: c.rotation}
		if c.voting {
			a.cfg.Vote = &voteConfig{}
		}
		got := a.match()
		switch {
		case c.want == nil:
			if got != nil {
				t.Errorf("%s: got %+v, want none", c.name, got)
			}
			continue
		case got == nil:
			t.Errorf("%s: got none", c.name)
			continue
		}
		if got.Phase != c.want.phase || got.Entry != c.want.entry || !got.Valid() {
			t.Errorf("%s: got %+v, want phase %s entry %q", c.name, got, c.want.phase, c.want.entry)
		}
		switch got.Entry {
		case "interference-fiesta":
			if got.Name != fiesta.Name || got.Thumb != fiesta.Thumb {
				t.Errorf("%s: name/thumb %+v", c.name, got)
			}
		case "kusini-ctf":
			if got.Name != ctf.ID || got.Thumb != ctf.Thumb {
				t.Errorf("%s: unnamed entry should show its ID: %+v", c.name, got)
			}
		}
	}

	// No report until the server is up.
	a := &agent{status: "starting", playlist: f, lobby: &lobbyInfo{State: stateInGame}}
	if m := a.match(); m != nil {
		t.Errorf("starting server reported %+v", m)
	}
}
