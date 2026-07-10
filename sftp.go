package main

import (
	"io"
	"log"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
)

// sftpHandler serves the SSH "sftp" subsystem over a session channel, so
// `sftp`, `scp` (OpenSSH 9+ uses SFTP under the hood), and editor remote-file
// plugins work over the same tailnet connection as the shell.
//
// Like the shell, this does NO extra auth — the tailnet already authorized the
// peer. Files are read/written as whatever user launched tailtap.
func sftpHandler(s ssh.Session) {
	server, err := sftp.NewServer(s)
	if err != nil {
		log.Printf("sftp: %v", err)
		return
	}
	defer server.Close()

	if err := server.Serve(); err != nil && err != io.EOF {
		log.Printf("sftp serve: %v", err)
	}
}
