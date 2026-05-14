//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func checkPrivilege() bool {
	if os.Geteuid() == 0 {
		return true
	}
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return false
	}
	const capNetAdmin = 12
	return (data[0].Effective>>capNetAdmin)&1 == 1
}
