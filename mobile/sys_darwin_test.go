//go:build darwin

package mobile

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// openUtun creates a real utun the way the kernel expects: a PF_SYSTEM socket connected to the
// "com.apple.net.utun_control" kernel control. Creating one requires privilege, so callers skip
// when this returns EPERM.
func openUtun(t *testing.T) int {
	t.Helper()

	fd, err := unix.Socket(unix.AF_SYSTEM, unix.SOCK_DGRAM, sysprotoControl)
	if err != nil {
		t.Skipf("cannot open a PF_SYSTEM socket: %v", err)
	}

	// CtlInfo.Name is a fixed-size array and IoctlCtlInfo fills the struct in place.
	info := &unix.CtlInfo{}
	copy(info.Name[:], "com.apple.net.utun_control")
	if err := unix.IoctlCtlInfo(fd, info); err != nil {
		unix.Close(fd)
		t.Skipf("cannot look up the utun kernel control: %v", err)
	}

	// Unit 0 asks the kernel to pick the next free utunN.
	if err := unix.Connect(fd, &unix.SockaddrCtl{ID: info.Id, Unit: 0}); err != nil {
		unix.Close(fd)
		if errors.Is(err, unix.EPERM) {
			t.Skip("creating a utun needs privilege; run as root to exercise this")
		}
		t.Skipf("cannot create a utun: %v", err)
	}
	return fd
}

func TestGetTunnelNameReturnsTheUtunName(t *testing.T) {
	fd := openUtun(t)
	defer unix.Close(fd)

	name, err := getTunnelName(int32(fd))
	if err != nil {
		t.Fatalf("getTunnelName on a real utun: %v", err)
	}
	if !strings.HasPrefix(name, "utun") {
		t.Fatalf("expected a utunN name, got %q", name)
	}
	t.Logf("named the tunnel %q", name)
}

// A descriptor that is not a utun must fail here rather than downstream, once sing-box has already
// started writing packets into it.
func TestGetTunnelNameRejectsANonTunnelDescriptor(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])

	if name, err := getTunnelName(int32(fds[0])); err == nil {
		t.Fatalf("expected an error for a plain unix socket, got name %q", name)
	}
}

func TestGetTunnelNameRejectsAClosedDescriptor(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	unix.Close(fds[0])
	unix.Close(fds[1])

	if _, err := getTunnelName(int32(fds[0])); err == nil {
		t.Fatal("expected an error for a closed descriptor")
	}
}

// The provider owns the descriptor it hands us and closes it on teardown, so the core has to hold
// its own copy that survives that close.
func TestDupSurvivesTheOriginalClosing(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer unix.Close(fds[1])

	copied, err := dup(fds[0])
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	defer unix.Close(copied)

	if copied == fds[0] {
		t.Fatal("dup returned the same descriptor rather than a copy")
	}

	// Close the original, as the provider would.
	unix.Close(fds[0])

	// The copy must still be usable.
	if _, err := unix.Write(copied, []byte("still open")); err != nil {
		t.Fatalf("the duplicated descriptor died with the original: %v", err)
	}
}

func TestLinkFlagsMapsUpAndRunning(t *testing.T) {
	f := linkFlags(unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_MULTICAST)
	if f&1 == 0 { // net.FlagUp
		t.Fatalf("expected FlagUp in %v", f)
	}
	if got := linkFlags(0); got != 0 {
		t.Fatalf("expected no flags, got %v", got)
	}
}
