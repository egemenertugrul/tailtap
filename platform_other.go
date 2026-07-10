//go:build !windows

package main

import "os"

// On Unix we can unlink the running executable: the process keeps running from
// the already-open inode, so the file is gone from disk immediately. Nothing is
// left behind even if the process is later hard-killed or the box loses power.
const cleanupAtStart = true

func removeSelf(path string) error { return os.Remove(path) }

// minimizeConsole is a no-op off Windows.
func minimizeConsole() {}
