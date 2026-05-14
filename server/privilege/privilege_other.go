//go:build !linux

package privilege

import "os"

func CheckPrivilege() bool {
	return os.Geteuid() == 0
}
