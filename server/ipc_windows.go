//go:build windows

package main

import (
	"net"
	"time"

	"github.com/tailscale/go-winio"
)

func connectIPC(socketName string) (net.Conn, error) {
	timeout := 5 * time.Second
	return winio.DialPipe(socketName, &timeout)
}
