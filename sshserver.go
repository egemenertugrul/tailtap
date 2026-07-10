package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Optional belt-and-suspenders: require the operator's SSH public key *on top*
// of the tailnet's authorization. Empty (the default) = tailnet auth only.
//
// Provide keys either baked in at build time:
//
//	go build -ldflags "-X main.authKey=... -X 'main.authorizedKeys=ssh-ed25519 AAAA... you@laptop'"
//
// or at runtime via the TAILTAP_AUTHORIZED_KEYS env var. Multiple keys may be
// separated by newlines (authorized_keys format).
var authorizedKeys string

func newSSHServer(stateDir string, allowForward bool) *ssh.Server {
	srv := &ssh.Server{
		Handler: sessionHandler, // session.go
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			"sftp": sftpHandler, // sftp.go — enables sftp/scp file transfer
		},
	}
	srv.AddHostKey(mustHostKey(stateDir))

	// Auth model: by default NONE beyond the tailnet, which already
	// authenticated + authorized this connection (same guarantee Tailscale SSH
	// gives). If authorizedKeys is set, additionally require a matching pubkey.
	if keys := loadAuthorizedKeys(); len(keys) > 0 {
		srv.PublicKeyHandler = func(_ ssh.Context, key ssh.PublicKey) bool {
			for _, allowed := range keys {
				if ssh.KeysEqual(key, allowed) {
					return true
				}
			}
			return false
		}
		log.Printf("public-key auth required (%d authorized key(s)) in addition to tailnet", len(keys))
	}

	// Port forwarding (opt-in via -forward). Lets the operator tunnel
	// venue-local services back to the laptop (ssh -L) or expose a laptop
	// service to the node (ssh -R). Only reachable by the operator anyway,
	// per the tailnet ACL, but kept off by default to minimize surface.
	if allowForward {
		fwd := &ssh.ForwardedTCPHandler{}
		srv.LocalPortForwardingCallback = func(ssh.Context, string, uint32) bool { return true }
		srv.ReversePortForwardingCallback = func(ssh.Context, string, uint32) bool { return true }
		srv.ChannelHandlers = map[string]ssh.ChannelHandler{
			"session":      ssh.DefaultSessionHandler,
			"direct-tcpip": ssh.DirectTCPIPHandler, // ssh -L (local forward)
		}
		srv.RequestHandlers = map[string]ssh.RequestHandler{
			"tcpip-forward":        fwd.HandleSSHRequest, // ssh -R (remote forward)
			"cancel-tcpip-forward": fwd.HandleSSHRequest,
		}
		log.Printf("port forwarding enabled (-L and -R)")
	}

	return srv
}

// loadAuthorizedKeys parses the baked-in / env-provided authorized keys.
func loadAuthorizedKeys() []ssh.PublicKey {
	raw := authorizedKeys
	if raw == "" {
		raw = os.Getenv("TAILTAP_AUTHORIZED_KEYS")
	}
	var keys []ssh.PublicKey
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			log.Printf("ignoring unparseable authorized key: %v", err)
			continue
		}
		keys = append(keys, pk)
	}
	return keys
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
