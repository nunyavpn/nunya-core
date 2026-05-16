package process

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sagernet/sing/common/atomic"
)

type Process struct {
	path     string
	args     []string
	noOut    bool
	confPath string
	cmd      *exec.Cmd
	stopped  atomic.Bool
}

func NewProcess(path string, args []string, noOut bool) *Process {
	return &Process{path: path, args: args, noOut: noOut}
}

// SetConfigFile registers a config file whose lifetime is bound to the process:
// it is removed when the process is stopped, exits, or fails to start. Pass ""
// for no config file.
func (p *Process) SetConfigFile(path string) {
	p.confPath = path
}

func (p *Process) Start() error {
	p.cmd = exec.Command(p.path, p.args...)

	p.cmd.Stdout = &pipeLogger{prefix: "Extra Core", noOut: p.noOut}
	p.cmd.Stderr = &pipeLogger{prefix: "Extra Core", noOut: p.noOut}

	p.cmd.Env = childEnv()

	// The Core may run elevated (setuid-root on unix, UAC-elevated on Windows),
	// but the extra process is an arbitrary user-supplied binary that must not
	// inherit those privileges. Drop to the unprivileged real user, or refuse
	// to start it at all.
	if err := applyPrivilegeDrop(p.cmd); err != nil {
		p.cleanupConf()
		return err
	}

	err := p.cmd.Start()
	if err != nil {
		p.cleanupConf()
		return err
	}
	p.stopped.Store(false)

	go func() {
		fmt.Println(p.path, ":", "process started, waiting for it to end")
		_ = p.cmd.Wait()
		if !p.stopped.Load() {
			fmt.Println("Extra process exited unexpectedly")
		}
		p.cleanupConf()
	}()
	return nil
}

func (p *Process) Stop() {
	p.stopped.Store(true)
	_ = p.cmd.Process.Kill()
	p.cleanupConf()
}

// cleanupConf removes the bound config file, if any. It is safe to call
// multiple times and from multiple goroutines (os.Remove on a missing path is
// a no-op for our purposes).
func (p *Process) cleanupConf() {
	if p.confPath != "" {
		_ = os.Remove(p.confPath)
	}
}

// CreateExtraConfig securely writes the extra process configuration to a fresh
// file in the system temp directory and returns its path.
//
// The Core may be running elevated, so the file must be created in a way that
// cannot be hijacked: os.CreateTemp opens with O_CREATE|O_EXCL and mode 0600
// under an unpredictable random name, so the call fails rather than following
// or clobbering a pre-planted symlink or any pre-existing file. The returned
// path is owned by Process and removed via SetConfigFile/cleanup.
func CreateExtraConfig(content string) (string, error) {
	f, err := os.CreateTemp("", "throne-extra-*.conf")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err = f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	// The extra process is de-privileged (see applyPrivilegeDrop), so it must
	// still be able to read the config this (possibly elevated) Core wrote.
	if err = makeConfigReadable(path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// childEnv returns the parent environment minus any THRONE-prefixed variables,
// which carry app-internal configuration the external process must not see.
func childEnv() []string {
	parent := os.Environ()
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		if name, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(strings.ToUpper(name), "THRONE") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
