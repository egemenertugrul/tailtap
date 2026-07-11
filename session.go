package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"

	// Cross-platform PTY: Unix pty + Windows ConPTY.
	// API verified against go-pty v0.2.3: New/Command/Start/Wait/Resize/Close.
	pty "github.com/aymanbagabas/go-pty"
)

// activeSessions counts live SSH sessions, reported by the heartbeat log line.
var activeSessions atomic.Int64

// sessionHandler runs either an interactive shell (no command given) or the
// requested command (e.g. `ssh host 'cmd'`, or the bootstrap commands tools
// like VS Code Remote-SSH send). It works with or without a PTY.
func sessionHandler(s ssh.Session) {
	n := activeSessions.Add(1)
	infoLog("session opened from %s (%d active)", s.RemoteAddr(), n)
	defer func() {
		infoLog("session closed from %s (%d active)", s.RemoteAddr(), activeSessions.Add(-1))
	}()

	ptyReq, winCh, isPty := s.Pty()

	// Pick what to run: an explicit command via the login shell, or the shell
	// itself for an interactive session.
	var name string
	var args []string
	if raw := s.RawCommand(); raw != "" {
		name, args = shellCommand(raw)
	} else {
		name, args = defaultShell()
	}

	if isPty {
		runPTY(s, ptyReq, winCh, name, args)
	} else {
		runPlain(s, name, args)
	}
}

// runPTY runs the process attached to a pseudo-terminal (interactive use).
func runPTY(s ssh.Session, ptyReq ssh.Pty, winCh <-chan ssh.Window, name string, args []string) {
	p, err := pty.New()
	if err != nil {
		log.Printf("pty: %v", err)
		_ = s.Exit(1)
		return
	}
	// conPty.Close is not idempotent, so guard it.
	var closeOnce sync.Once
	closePTY := func() { closeOnce.Do(func() { _ = p.Close() }) }
	defer closePTY()

	c := p.Command(name, args...)
	c.Env = append(sessionEnv(s), "TERM="+ptyReq.Term)
	if err := c.Start(); err != nil {
		log.Printf("start: %v", err)
		_ = s.Exit(1)
		return
	}

	_ = p.Resize(ptyReq.Window.Width, ptyReq.Window.Height)
	go func() {
		for win := range winCh {
			_ = p.Resize(win.Width, win.Height)
		}
	}()

	go func() { _, _ = io.Copy(p, s) }() // client -> shell

	// shell -> client in the background. On Windows ConPTY the output pipe does
	// not reach EOF when the process exits, so a foreground copy would hang the
	// client after `exit`. We wait for the process, then close the PTY.
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(s, p)
		close(copied)
	}()

	waitErr := c.Wait()
	closePTY()
	select {
	case <-copied:
	case <-time.After(2 * time.Second):
	}
	_ = s.Exit(ptyExitStatus(c, waitErr))
}

// runPlain runs the process with plain pipes, no PTY. This is what
// non-interactive clients (scp's peer, VS Code Remote-SSH bootstrap, and
// `ssh host 'cmd'`) use.
func runPlain(s ssh.Session, name string, args []string) {
	c := exec.Command(name, args...)
	c.Env = sessionEnv(s)
	c.Stdout = s
	c.Stderr = s.Stderr()
	stdin, err := c.StdinPipe()
	if err != nil {
		log.Printf("stdin: %v", err)
		_ = s.Exit(1)
		return
	}
	if err := c.Start(); err != nil {
		log.Printf("start: %v", err)
		_ = s.Exit(1)
		return
	}
	go func() {
		_, _ = io.Copy(stdin, s)
		_ = stdin.Close()
	}()
	_ = s.Exit(execExitStatus(c.Wait()))
}

// sessionEnv is the child environment: the agent's own environment plus any
// variables the client requested (SetEnv). The tailnet already authorized the
// peer, so we do not filter these.
func sessionEnv(s ssh.Session) []string {
	return append(os.Environ(), s.Environ()...)
}

// shellCommand wraps a raw command string so the login shell runs it, matching
// how OpenSSH executes `ssh host 'cmd'`.
func shellCommand(raw string) (string, []string) {
	sh, _ := defaultShell()
	if runtime.GOOS == "windows" {
		base := strings.ToLower(filepath.Base(sh))
		if strings.HasPrefix(base, "pwsh") || strings.HasPrefix(base, "powershell") {
			return sh, []string{"-Command", raw}
		}
		return sh, []string{"/c", raw}
	}
	return sh, []string{"-c", raw}
}

func ptyExitStatus(c *pty.Cmd, waitErr error) int {
	if c.ProcessState != nil {
		return c.ProcessState.ExitCode()
	}
	if waitErr != nil {
		return 1
	}
	return 0
}

func execExitStatus(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	if ee, ok := waitErr.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}

func defaultShell() (string, []string) {
	if shellOverride != "" {
		return shellOverride, nil
	}
	if runtime.GOOS == "windows" {
		// Prefer PowerShell 7, then Windows PowerShell, then cmd.
		for _, sh := range []string{"pwsh.exe", "powershell.exe", "cmd.exe"} {
			if p, err := exec.LookPath(sh); err == nil {
				return p, nil
			}
		}
		return "cmd.exe", nil
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh, nil
	}
	return "/bin/bash", nil
}
