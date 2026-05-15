//go:build windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// applyPrivilegeDrop makes the child run as the unprivileged interactive user.
//
// Windows has no setuid: a UAC-elevated parent spawns children with its own
// elevated admin token. We obtain a non-elevated primary token for the real
// user and hand it to CreateProcessAsUser via SysProcAttr.Token. If no
// unprivileged token can be obtained, we refuse to launch an arbitrary binary
// with admin rights.
func applyPrivilegeDrop(cmd *exec.Cmd) error {
	self, err := selfToken()
	if err != nil {
		return fmt.Errorf("cannot open process token: %w", err)
	}
	defer self.Close()

	if !self.IsElevated() {
		return nil // not elevated, run as-is
	}

	tok, err := unprivilegedToken(self)
	if err != nil {
		return fmt.Errorf("refusing to start extra process: cannot obtain an unprivileged user token: %w", err)
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(tok)
	cmd.SysProcAttr.HideWindow = true
	return nil
}

func selfToken() (windows.Token, error) {
	var tok windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &tok)
	return tok, err
}

// unprivilegedToken returns a primary token for the real, non-elevated user,
// trying in order: the UAC linked token of the same user (the common
// elevated-admin case), the active console session user, then the interactive
// shell (explorer.exe) user (covers a SYSTEM service).
func unprivilegedToken(self windows.Token) (windows.Token, error) {
	if t, err := linkedToken(self); err == nil {
		return t, nil
	}
	if t, err := consoleSessionToken(); err == nil {
		return t, nil
	}
	if t, err := shellToken(); err == nil {
		return t, nil
	}
	return 0, errors.New("no linked, console-session, or shell token available")
}

func linkedToken(self windows.Token) (windows.Token, error) {
	linked, err := self.GetLinkedToken()
	if err != nil {
		return 0, err
	}
	defer linked.Close()
	if linked.IsElevated() {
		return 0, errors.New("linked token is still elevated")
	}
	return primaryToken(linked)
}

func consoleSessionToken() (windows.Token, error) {
	session := windows.WTSGetActiveConsoleSessionId()
	if session == 0xFFFFFFFF {
		return 0, errors.New("no active console session")
	}
	var tok windows.Token
	if err := windows.WTSQueryUserToken(session, &tok); err != nil {
		return 0, err
	}
	defer tok.Close()
	return primaryToken(tok)
}

func shellToken() (windows.Token, error) {
	pid, err := findProcess("explorer.exe")
	if err != nil {
		return 0, err
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(proc)

	var tok windows.Token
	if err := windows.OpenProcessToken(proc, windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY, &tok); err != nil {
		return 0, err
	}
	defer tok.Close()
	return primaryToken(tok)
}

func primaryToken(src windows.Token) (windows.Token, error) {
	var dup windows.Token
	err := windows.DuplicateTokenEx(
		src,
		windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|
			windows.TOKEN_ADJUST_DEFAULT|windows.TOKEN_ADJUST_SESSIONID,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&dup,
	)
	if err != nil {
		return 0, err
	}
	return dup, nil
}

func findProcess(name string) (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			return entry.ProcessID, nil
		}
	}
	return 0, fmt.Errorf("process %q not found", name)
}

// MakeConfigReadable best-effort grants the local Users group read access to
// path so a de-privileged child can read it. With the linked-token path the
// child is the same user at a lower integrity level and can already read it,
// so a failure here is non-fatal.
func MakeConfigReadable(path string) error {
	usersSid, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return nil
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return nil
	}
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_READ,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(usersSid),
		},
	}}
	merged, err := windows.ACLFromEntries(entries, dacl)
	if err != nil {
		return nil
	}
	_ = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION, nil, nil, merged, nil)
	return nil
}
