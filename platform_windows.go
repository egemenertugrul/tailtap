//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

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
