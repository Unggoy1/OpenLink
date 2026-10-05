//go:build !demo

package main

// demo is set only in builds with the "demo" tag (demo.go), which fake a
// joined server and a vote so the UI can be previewed without the game.
var demo *demoState

type demoState struct{}

func (*demoState) status() *StatusView { return nil }
func (*demoState) ballot() *BallotView { return nil }
func (*demoState) vote(int) error      { return nil }
