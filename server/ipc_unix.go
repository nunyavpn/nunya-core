//go:build linux || darwin

package main

import "net"

func connectIPC(socketName string) (net.Conn, error) {
	return net.Dial("unix", socketName)
}
