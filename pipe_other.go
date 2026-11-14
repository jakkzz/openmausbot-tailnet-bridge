//go:build !windows

package main

import (
	"context"
	"errors"
	"net"
)

func newPipeDialer(expectedExe string) (func(context.Context, string, string) (net.Conn, error), error) {
	return nil, errors.New("private Windows named pipes are unsupported on this platform")
}
