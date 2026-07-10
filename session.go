package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"

	// Cross-platform PTY: Unix pty + Windows ConPTY.
	// API verified against go-pty v0.2.3: New/Command/Start/Wait/Resize/Close.
	pty "github.com/aymanbagabas/go-pty"
)

func sessionHandler(s ssh.Session) {
	ptyReq, winCh, isPty := s.Pty()
	if !isPty {
		io.WriteString(s, "interactive shell only (request a pty)\n")
		_ = s.Exit(1)
		return
	}

	shell, args := defaultShell()

	p, err := pty.New()
	if err != nil {
		log.Printf("pty: %v", err)
		_ = s.Exit(1)
		return
	}
	// Close the PTY exactly once, on any exit path. conPty.Close is not
	// idempotent, so guard it.
	var closeOnce sync.Once
	closePTY := func() { closeOnce.Do(func() { _ = p.Close() }) }
	defer closePTY()

	c := p.Command(shell, args...)
	c.Env = append(os.Environ(), "TERM="+ptyReq.Term)
	if err := c.Start(); err != nil {
		log.Printf("start shell: %v", err)
		_ = s.Exit(1)
		return
	}

	// Initial size + ongoing resizes.
	_ = p.Resize(ptyReq.Window.Width, ptyReq.Window.Height)
	go func() {
		for win := range winCh {
			_ = p.Resize(win.Width, win.Height)
		}
	}()

	// client -> shell
	go func() { _, _ = io.Copy(p, s) }()

	// shell -> client, in the background. On Windows ConPTY the output pipe does
	// NOT reach EOF when the shell exits, so a foreground copy here would block
	// forever and the client's session would hang after typing `exit`. Instead we
	// wait for the process, then close the PTY to unblock this copy.
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(s, p)
		close(copied)
	}()

	waitErr := c.Wait()
	closePTY() // unblocks the shell -> client copy above

	// Let any final output drain, but never hang.
	select {
	case <-copied:
	case <-time.After(2 * time.Second):
	}

	_ = s.Exit(exitStatus(c, waitErr))
}

// exitStatus returns the shell's exit code to report back to the SSH client.
func exitStatus(c *pty.Cmd, waitErr error) int {
	if c.ProcessState != nil {
		return c.ProcessState.ExitCode()
	}
	if waitErr != nil {
		return 1
	}
	return 0
}

func defaultShell() (string, []string) {
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
