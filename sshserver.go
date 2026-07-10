package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log"
	"os"
	"path/filepath"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func newSSHServer(stateDir string) *ssh.Server {
	srv := &ssh.Server{
		Handler: sessionHandler, // session.go
	}
	srv.AddHostKey(mustHostKey(stateDir))

	// Auth model: NONE. The tailnet already authenticated + authorized this
	// connection (same guarantee Tailscale SSH gives).
	//
	// Belt-and-suspenders option — require YOUR pubkey on top:
	//
	// myPub := "ssh-ed25519 AAAA... you@laptop"
	// allowed, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(myPub))
	// srv.PublicKeyHandler = func(_ ssh.Context, key ssh.PublicKey) bool {
	// 	return ssh.KeysEqual(key, allowed)
	// }

	return srv
}

// Persist the host key in the state dir so the client doesn't warn on reconnect.
// For truly ephemeral one-shot use with a stable identity, bake a fixed key via
// ldflags instead (another secret to protect — usually not worth it).
func mustHostKey(stateDir string) gossh.Signer {
	path := filepath.Join(stateDir, "host_ed25519")

	if pemBytes, err := os.ReadFile(path); err == nil {
		if signer, err := gossh.ParsePrivateKey(pemBytes); err == nil {
			return signer
		}
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		log.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	_ = os.WriteFile(path, pemBytes, 0o600)

	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		log.Fatal(err)
	}
	return signer
}
