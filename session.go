package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/gliderlabs/ssh"

	// Cross-platform PTY: Unix pty + Windows ConPTY.
	// API verified against go-pty v0.2.3: New/Command/Start/Wait/Resize/Close.
	"github.com/aymanbagabas/go-pty"
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
	defer p.Close()

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

	// Wire stdio both directions.
	go func() { _, _ = io.Copy(p, s) }() // client -> shell
	_, _ = io.Copy(s, p)                 // shell  -> client

	_ = c.Wait()
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
