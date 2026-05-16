//go:build unix

package process

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// applyPrivilegeDrop makes the child run as the unprivileged real user.
//
// The Core is a setuid-root binary, so when an unprivileged user launches it
// the effective uid is 0 but the real uid still identifies that user. We hand
// that real uid/gid to the kernel via SysProcAttr.Credential, which performs
// the setuid/setgid in the child before exec — race-free.
//
// If the Core is genuinely running as root (real uid 0, e.g. a root login or
// `sudo`), there is no unprivileged user to recover, so we refuse to launch an
// arbitrary binary with root privileges.
func applyPrivilegeDrop(cmd *exec.Cmd) error {
	if os.Geteuid() != 0 {
		return nil // not elevated, run as-is
	}
	ruid := os.Getuid()
	rgid := os.Getgid()
	if ruid == 0 {
		return errors.New("refusing to start extra process as root: no unprivileged user to drop to")
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    uint32(ruid),
			Gid:    uint32(rgid),
			Groups: supplementaryGroups(ruid, rgid),
		},
	}
	cmd.Env = userEnv(cmd.Env, ruid)
	return nil
}

// supplementaryGroups resolves the real user's groups so the child keeps the
// access the user normally has. If the user database can't be read, fall back
// to just the primary gid (strictly fewer privileges, never more).
func supplementaryGroups(ruid, rgid int) []uint32 {
	fallback := []uint32{uint32(rgid)}
	u, err := user.LookupId(strconv.Itoa(ruid))
	if err != nil {
		return fallback
	}
	ids, err := u.GroupIds()
	if err != nil {
		return fallback
	}
	groups := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if n, err := strconv.Atoi(id); err == nil {
			groups = append(groups, uint32(n))
		}
	}
	if len(groups) == 0 {
		return fallback
	}
	return groups
}

// userEnv replaces HOME/USER/LOGNAME with the dropped user's values so the
// child doesn't see root's identity.
func userEnv(env []string, ruid int) []string {
	u, err := user.LookupId(strconv.Itoa(ruid))
	if err != nil {
		return env
	}
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok {
			switch name {
			case "HOME", "USER", "LOGNAME":
				continue
			}
		}
		out = append(out, kv)
	}
	return append(out, "HOME="+u.HomeDir, "USER="+u.Username, "LOGNAME="+u.Username)
}

// makeConfigReadable ensures path can be read by the de-privileged extra
// process. The exact target uid/gid is known, so ownership is transferred and
// the mode kept tight (0600) rather than world-exposing a config that may
// carry secrets.
func makeConfigReadable(path string) error {
	if os.Geteuid() != 0 {
		return os.Chmod(path, 0o600)
	}
	ruid := os.Getuid()
	rgid := os.Getgid()
	if ruid == 0 {
		// Start() will refuse to launch in this case anyway.
		return nil
	}
	if err := os.Chown(path, ruid, rgid); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
