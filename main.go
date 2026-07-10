package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"tailscale.com/tsnet"
)

// Injected at build time:  go build -ldflags "-X main.authKey=tskey-auth-..."
// MUST be a var, not a const — ldflags -X only patches package-level vars.
var authKey string

func main() {
	name := flag.String("name", "tailtap", "hostname on the tailnet")
	persist := flag.Bool("persist", false, "reconnect as the same node across runs/reboots")
	forward := flag.Bool("forward", false, "allow SSH port forwarding (-L / -R)")
	quiet := flag.Bool("quiet", false, "suppress tsnet and informational logs (errors still print)")
	flag.Parse()

	infof := func(format string, args ...any) {
		if !*quiet {
			log.Printf(format, args...)
		}
	}

	if authKey == "" {
		authKey = os.Getenv("TS_AUTHKEY") // fallback for local dev
	}
	if authKey == "" {
		log.Fatal(`no auth key: build with -ldflags "-X main.authKey=..." or set TS_AUTHKEY`)
	}

	// State dir. Ephemeral one-shot -> temp dir (fresh registration each run).
	// Persistent -> fixed dir so identity + host key survive reboots.
	var stateDir string
	if *persist {
		cfg, _ := os.UserConfigDir()
		stateDir = filepath.Join(cfg, "tailtap")
		os.MkdirAll(stateDir, 0o700)
	} else {
		d, err := os.MkdirTemp("", "tailtap-*")
		if err != nil {
			log.Fatal(err)
		}
		stateDir = d
		defer os.RemoveAll(stateDir)
	}

	s := &tsnet.Server{
		Hostname:  *name,
		AuthKey:   authKey,
		Ephemeral: !*persist, // ephemeral nodes auto-deregister when they go offline
		Dir:       stateDir,
	}
	if *quiet {
		noop := func(string, ...any) {}
		s.Logf = noop
		s.UserLogf = noop
	}
	defer s.Close()

	// Bring the node up and report the assigned tailnet IP.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := s.Up(ctx)
	if err != nil {
		log.Fatalf("tailscale up failed: %v", err)
	}
	infof("online as %q  ip=%v", *name, st.TailscaleIPs)

	// SECURITY: this listener accepts ONLY tailnet connections.
	// It MUST be s.Listen (tsnet), never net.Listen on 0.0.0.0.
	ln, err := s.Listen("tcp", ":22")
	if err != nil {
		log.Fatal(err)
	}

	srv := newSSHServer(stateDir, *forward)
	infof("ssh server listening on tailnet:22")
	log.Fatal(srv.Serve(ln))
}
