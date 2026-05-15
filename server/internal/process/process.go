package process

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sagernet/sing/common/atomic"
)

type Process struct {
	path    string
	args    []string
	noOut   bool
	cmd     *exec.Cmd
	stopped atomic.Bool
}

func NewProcess(path string, args []string, noOut bool) *Process {
	return &Process{path: path, args: args, noOut: noOut}
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
		return err
	}

	err := p.cmd.Start()
	if err != nil {
		return err
	}
	p.stopped.Store(false)

	go func() {
		fmt.Println(p.path, ":", "process started, waiting for it to end")
		_ = p.cmd.Wait()
		if !p.stopped.Load() {
			fmt.Println("Extra process exited unexpectedly")
		}
	}()
	return nil
}

func (p *Process) Stop() {
	p.stopped.Store(true)
	_ = p.cmd.Process.Kill()
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
