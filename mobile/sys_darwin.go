//go:build darwin

package mobile

import (
	"fmt"
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Neither constant is exported by x/sys/unix on Darwin.
const (
	// SYSPROTO_CONTROL from <sys/kern_control.h>; the getsockopt level for a PF_SYSTEM socket.
	sysprotoControl = 2
	// UTUN_OPT_IFNAME from <net/if_utun.h>; the only way to learn which utunN a descriptor is.
	utunOptIfname = 2
)

// getTunnelName names the utun behind a descriptor handed to us by the platform.
//
// On Apple platforms a TUN is a PF_SYSTEM/SYSPROTO_CONTROL socket rather than a character device,
// so there is no ioctl equivalent of Linux's TUNGETIFF; the name is a socket option. This doubles
// as validation: a descriptor that is not a utun fails here rather than later, when sing-box has
// already begun writing packets into the wrong thing.
func getTunnelName(fd int32) (string, error) {
	// getsockopt alone is not enough to identify a utun. On a descriptor of another family the
	// call can still succeed and return unrelated bytes -- an AF_UNIX socket yields "Sc" -- and a
	// bogus name would be handed to sing-box as a real interface. Check the family first.
	sa, err := unix.Getsockname(int(fd))
	if err != nil {
		return "", fmt.Errorf("failed to inspect TUN device: %w", err)
	}
	if _, ok := sa.(*unix.SockaddrCtl); !ok {
		return "", fmt.Errorf("descriptor is not a kernel control socket (%T), so it is not a utun", sa)
	}

	name, err := unix.GetsockoptString(int(fd), sysprotoControl, utunOptIfname)
	if err != nil {
		return "", fmt.Errorf("failed to get name of TUN device: %w", err)
	}
	// Belt and braces: every utun the kernel hands out is named utunN.
	if !strings.HasPrefix(name, "utun") {
		return "", fmt.Errorf("unexpected tunnel name %q, refusing to treat it as a utun", name)
	}
	return name, nil
}

// dup copies the descriptor so the core's lifetime is independent of the caller's.
//
// The NetworkExtension provider owns the descriptor it passes in and closes it when the system
// tears the tunnel down. Without a private copy, a stop on either side would pull the descriptor
// out from under the other.
func dup(fd int) (int, error) {
	return syscall.Dup(fd)
}

// copied from net.linkFlags
func linkFlags(rawFlags uint32) net.Flags {
	var f net.Flags
	if rawFlags&syscall.IFF_UP != 0 {
		f |= net.FlagUp
	}
	if rawFlags&syscall.IFF_RUNNING != 0 {
		f |= net.FlagRunning
	}
	if rawFlags&syscall.IFF_BROADCAST != 0 {
		f |= net.FlagBroadcast
	}
	if rawFlags&syscall.IFF_LOOPBACK != 0 {
		f |= net.FlagLoopback
	}
	if rawFlags&syscall.IFF_POINTOPOINT != 0 {
		f |= net.FlagPointToPoint
	}
	if rawFlags&syscall.IFF_MULTICAST != 0 {
		f |= net.FlagMulticast
	}
	return f
}
