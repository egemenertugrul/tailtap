package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"tailscale.com/tsnet"
)

// Injected at build time:  go build -ldflags "-X main.authKey=tskey-auth-..."
// MUST be a var, not a const — ldflags -X only patches package-level vars.
var authKey string

// Set by the release build via -ldflags "-X main.version=v1.2.3".
var version = "dev"

// Default tailnet hostname. Bake a per-build default via
// -ldflags "-X main.defaultName=booth"; -name at runtime still overrides it.
var defaultName = "tailtap"

// Flags applied automatically at startup, baked in at build time via
// -ldflags "-X 'main.bakedFlags=-vscode -persist'". Anything typed on the
// command line overrides them. Values cannot contain spaces.
var bakedFlags string

// infoLog prints status and heartbeat lines to the console (stderr). It is
// swapped for a no-op under -quiet.
var infoLog = log.Printf

// shellOverride, when set via -shell, replaces the auto-detected login shell.
var shellOverride string

func main() {
	name := flag.String("name", defaultName, "hostname on the tailnet (default: this machine's hostname)")
	persist := flag.Bool("persist", false, "reconnect as the same node across runs/reboots")
	forward := flag.Bool("forward", false, "allow SSH port forwarding (-L / -R / -D)")
	shell := flag.String("shell", "", "shell to run for sessions (default: auto-detect)")
	web := flag.Bool("web", false, "serve a file browser over HTTP on the tailnet (download/upload)")
	webRoot := flag.String("webroot", ".", "directory the -web file browser serves")
	quiet := flag.Bool("quiet", false, "suppress tsnet and informational logs (errors still print)")
	cleanup := flag.Bool("cleanup", false, "[DEPRECATED/experimental] delete this binary when done; unreliable on Windows")
	minimize := flag.Bool("minimize", false, "minimize the console window on start (Windows only)")
	vscode := flag.Bool("vscode", false, "tune for VS Code Remote-SSH: enables -forward and runs as sshd.exe (Windows)")
	showVersion := flag.Bool("version", false, "print version and exit")

	// Baked-in flags come first so anything on the real command line overrides them.
	args := os.Args[1:]
	if bakedFlags != "" {
		args = append(strings.Fields(bakedFlags), args...)
	}
	flag.CommandLine.Parse(args)

	if *showVersion {
		fmt.Println("tailtap", version)
		return
	}

	// VS Code Remote-SSH needs port forwarding and, on Windows, a parent process
	// named sshd. Handle both so the user does not have to rename the binary.
	if *vscode {
		*forward = true
		if relaunched, code := becomeSshd(); relaunched {
			os.Exit(code)
		}
	}

	if *minimize {
		minimizeConsole()
	}

	if *quiet {
		infoLog = func(string, ...any) {}
	}
	shellOverride = *shell

	// Default the node name to this machine's hostname, so several machines each
	// running tailtap don't collide on one name. An explicit -name or a baked-in
	// NAME= still wins.
	nameSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "name" {
			nameSet = true
		}
	})
	if !nameSet && defaultName == "tailtap" {
		if h, err := os.Hostname(); err == nil {
			if clean := sanitizeName(h); clean != "" {
				*name = clean
			}
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

	// Self-delete when asked, so nothing is left on the target machine.
	if *cleanup {
		// Deprecated: the Windows path is unreliable. Left in but discouraged
		// until it is reworked. Always warn, even under -quiet.
		log.Printf("warning: -cleanup is deprecated and experimental; it is unreliable on Windows. Prefer running from a USB stick.")
		if self, err := os.Executable(); err != nil {
			infoLog("cleanup: cannot resolve own path: %v", err)
		} else if cleanupAtStart {
			// Unix: unlink now; we keep running from the open inode.
			if err := removeSelf(self); err != nil {
				infoLog("cleanup: could not remove binary: %v", err)
			} else {
				infoLog("cleanup: removed on-disk binary (running from memory)")
			}
		} else {
			// Windows: the file is locked while running, so delete on exit.
			sigc := make(chan os.Signal, 1)
			signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigc
				infoLog("cleanup: deleting binary on exit")
				_ = removeSelf(self)
				s.Close()
				os.Exit(0)
			}()
		}
	}

	// Bring the node up and report the assigned tailnet IP.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := s.Up(ctx)
	if err != nil {
		log.Fatalf("tailscale up failed: %v", err)
	}
	infoLog("online as %q  ip=%v", *name, st.TailscaleIPs)

	// SECURITY: this listener accepts ONLY tailnet connections.
	// It MUST be s.Listen (tsnet), never net.Listen on 0.0.0.0.
	ln, err := s.Listen("tcp", ":22")
	if err != nil {
		log.Fatal(err)
	}

	// Optional file browser over HTTP, also bound only to the tailnet.
	if *web {
		wln, err := s.Listen("tcp", ":80")
		if err != nil {
			log.Fatalf("web listen: %v", err)
		}
		root, err := filepath.Abs(*webRoot)
		if err != nil {
			root = *webRoot
		}
		infoLog("file browser on http://%s/ (serving %s)", *name, root)
		go func() {
			if err := serveWeb(wln, root); err != nil {
				log.Printf("web server stopped: %v", err)
			}
		}()
	}

	// Heartbeat: a periodic sign-of-life with uptime and live session count.
	// Silent under -quiet (infoLog is a no-op then).
	start := time.Now()
	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for range t.C {
			infoLog("heartbeat: up %s, %d active session(s)",
				time.Since(start).Round(time.Second), activeSessions.Load())
		}
	}()

	srv := newSSHServer(stateDir, *forward)
	infoLog("ssh server listening on tailnet:22")
	log.Fatal(srv.Serve(ln))
}

// sanitizeName lowercases a machine hostname and keeps only characters valid in
// a tailnet name, so an unusual hostname still yields a usable node name.
func sanitizeName(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	var b strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '.', r == ' ', r == '_':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
