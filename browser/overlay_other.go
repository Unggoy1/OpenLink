//go:build !windows

package main

// The vote overlay is Windows-only for now; elsewhere the app's own vote panel
// and the desktop notification are the only alerts.
type voteOverlay struct{}

func newVoteOverlay(func(round uint64, choice int) error) *voteOverlay { return nil }

func (*voteOverlay) update(*BallotView, Settings) {}
func (*voteOverlay) close()                       {}
func (*voteOverlay) conflicts([]string) []string  { return nil }
