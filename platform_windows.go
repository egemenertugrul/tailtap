//go:build windows

package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// becomeSshd re-launches this binary as "sshd.exe" so VS Code Remote-SSH's
// Windows bootstrap finds a parent process named sshd (it walks the process
// tree and gives up otherwise). It copies the binary to %TEMP%\sshd.exe, runs
// it with the same args (minus the relaunch, via an env sentinel), waits, and
// then removes the copy. Returns (true, exitCode) if it relaunched; (false, 0)
// to keep running in-process (already sshd, or on any error).
func becomeSshd() (bool, int) {
	if os.Getenv("TAILTAP_ISSSHD") == "1" {
		return false, 0 // we are the relaunched child
	}
	self, err := os.Executable()
	if err != nil {
		return false, 0
	}
	if strings.EqualFold(filepath.Base(self), "sshd.exe") {
		return false, 0 // already named sshd
	}

	dst := filepath.Join(os.TempDir(), "sshd.exe")
	if err := copyFile(self, dst); err != nil {
		log.Printf("vscode: could not create sshd.exe (%v); running under the original name", err)
		return false, 0
	}

	cmd := exec.Command(dst, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "TAILTAP_ISSSHD=1")
	if err := cmd.Start(); err != nil {
		log.Printf("vscode: could not start sshd.exe (%v); running under the original name", err)
		return false, 0
	}
	waitErr := cmd.Wait()
	_ = os.Remove(dst) // best-effort; the child has exited so the lock is gone

	code := 0
	if ee, ok := waitErr.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if waitErr != nil {
		code = 1
	}
	return true, code
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// On Windows a running .exe is locked, so it cannot unlink itself while running.
// Instead removeSelf is called on exit and spawns a detached cmd that waits for
// this process to release the file, then deletes it. If the process is
// hard-killed (Task Manager > End Process), the file remains.
const cleanupAtStart = false

func removeSelf(path string) error {
	// ping gives a short delay so our process has fully exited (and released the
	// file lock) before del runs.
	cmd := exec.Command("cmd", "/C", `ping 127.0.0.1 -n 3 >nul & del /f /q "`+path+`"`)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008, // DETACHED_PROCESS
	}
	return cmd.Start()
}

// minimizeConsole minimizes the console window this process owns.
func minimizeConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	user32 := syscall.NewLazyDLL("user32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	const swMinimize = 6
	_, _, _ = showWindow.Call(hwnd, swMinimize)
}
