//go:build !windows

package hostctl

import (
	"context"
	"errors"
	"os"
)

type ManagedSession struct{ Bridge *Bridge }

func (*ManagedSession) Close() error { return nil }
func StartManaged(context.Context, *os.Process, string, string, bool) (*ManagedSession, error) {
	return nil, errors.New("host-control DLL loading requires Windows x64")
}
