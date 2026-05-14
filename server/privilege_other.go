//go:build !linux

package main

import "os"

func checkPrivilege() bool {
	return os.Geteuid() == 0
}
