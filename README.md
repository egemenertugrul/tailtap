# tailtap

**Drop one binary on any machine and SSH into it over your Tailscale network. Nothing to install on the target, no login there, no keys to copy.**

Run it on a Windows, Linux, or macOS machine. It joins your tailnet and serves a shell. You connect from your laptop by name. Kill it and the machine drops off your network.

It's for one thing: get into a machine at a venue, lab, or client site for the length of a job, then leave nothing behind. Works well as an automation target too.

```
     your laptop                          the target machine
  (on your tailnet)                      (any OS, no install)
        │                                        │
        │      encrypted Tailscale tailnet       │
        │        (WireGuard, no open ports)      │
        │                                        ▼
   ssh target ────────────────────────────►   tailtap
        │                                     ├─ interactive shell
        ▼                                     └─ SFTP / scp
   you get a shell            joins your tailnet with an ephemeral, tagged key
                              and disappears again when it stops
```

No ports are opened on the local network. The target needs no Tailscale install, no admin rights, and no firewall changes.

---

## Connect

Run it on the target, then from your laptop:

```bash
ssh booth
```

No password, no key prompt. The name is whatever you passed to `-name` (or baked in at build time).

```bash
ssh booth              # started as: tailtap -name booth
ssh 100.101.102.103    # or the tailnet IP it printed on startup
```

The username doesn't matter — the tailnet decides who gets in, and the shell runs as whoever started the binary. Files go over the same connection:

```bash
scp report.pdf booth:/tmp/
sftp booth
```

If the name won't resolve, MagicDNS is probably off on your laptop — use the IP instead.

---

## How it works

- Embeds a Tailscale node with [`tsnet`](https://pkg.go.dev/tailscale.com/tsnet) — userspace, so no admin or root, and it never touches the machine's real port 22.
- Authenticates with an ephemeral auth key baked into the binary at build time.
- Runs its own SSH server ([`gliderlabs/ssh`](https://github.com/gliderlabs/ssh)) bound only to the tailnet.
- The tailnet is the login: if your ACL lets you reach the node, you're in. No separate password or key by default.
- Cross-platform PTY ([`go-pty`](https://github.com/aymanbagabas/go-pty)) so full-screen apps, colors, and resize work. SFTP and scp too.

Nothing here relies on the code being secret — safety is your auth key and ACL. That's why it's fine to make public.

---

## Where it fits

There are two halves to reaching a machine over Tailscale: the **client** you connect from, and the **server** you connect to.

tailtap is the server. You run it on the machine you want to reach. A tool like [`ts-ssh`](https://github.com/derekg/ts-ssh) is a client — it connects to machines that already run a server. Use plain `ssh` or ts-ssh to reach a tailtap node.

Reach for tailtap when a machine has no SSH server and no Tailscale yet, and you want to fix that in one step and undo it cleanly.

---

## Security

Safety is all in how you make the key and the ACL:

1. Make the auth key **ephemeral, expiring, and tagged** — Reusable, Ephemeral, Pre-authorized, tag `tag:tailtap`, short expiry.
2. The key lives inside every binary in `dist/`. Treat those like passwords: delete them off the target after the job, then revoke the key.
3. Fence the node in so only you can reach it and it can reach nothing else:

```jsonc
{
  "tagOwners": { "tag:tailtap": ["you@example.com"] },
  "acls": [
    // you can reach tailtap nodes on the SSH port
    { "action": "accept", "src": ["you@example.com"], "dst": ["tag:tailtap:22"] }
    // nothing grants tag:tailtap outbound access, so it can't touch the rest of your tailnet
  ]
}
```

This ACL governs tailtap's own SSH server. Tailscale's built-in `ssh` ACL block doesn't apply here.

You can also require your own SSH key on top of the tailnet check — see [Flags](#flags).

---

## Build

Bring your own auth key. It's injected at build time and never stored in the repo.

```bash
./build.sh tskey-auth-xxxx               # or: KEY=tskey-auth-xxxx ./build.sh
NAME=booth ./build.sh tskey-auth-xxxx    # bake a default hostname; -name still overrides
```

Output is static and dependency-free (`CGO_ENABLED=0`, stripped):

| File | Runs on |
|------|--------|
| `tailtap-linux-amd64`        | Intel or AMD Linux |
| `tailtap-linux-arm64`        | ARM Linux (Raspberry Pi, SBCs) |
| `tailtap-windows-amd64.exe`  | 64-bit Windows 10/11 |
| `tailtap-macos-arm64`        | Apple Silicon Mac |
| `tailtap-macos-amd64`        | Intel Mac |

Don't commit `dist/` — every binary carries a live key. It's gitignored.

### Minting keys automatically (optional)

Set up a Tailscale OAuth client once (scope: Auth Keys write, tag `tag:tailtap`) and let [`mint-key.sh`](./mint-key.sh) create keys:

```bash
export TS_CLIENT_ID=... TS_CLIENT_SECRET=...
./build.sh
```

It mints a fresh key per build by default, which matches the "revoke after each job" habit. Set `TS_KEY_REUSE=1` to cache one under `~/.tailtap` and reuse it until it nears expiry. Needs `curl` and `jq`; see the script header for the rest.

---

## Run it

1. Copy the right binary over (a USB stick is most reliable at a venue).
2. Run it — no admin needed:
   - **Linux and macOS:** `./tailtap-linux-amd64 -name booth`
   - **Windows:** double-click, or `.\tailtap-windows-amd64.exe -name booth`. It's unsigned, so SmartScreen warns — More info → Run anyway.
   - **macOS:** first run, right-click → Open (or `xattr -d com.apple.quarantine ./tailtap-macos-arm64`).
3. It shows up in your device list under `-name`, tagged `tag:tailtap`. Connect with `ssh booth`.

Give each machine a unique name, or Tailscale appends a number (`booth-1`). Add `-persist` to keep the same identity across reboots — otherwise it's ephemeral and vanishes when it stops.

---

## Files and editors

SFTP is always on, over the same connection — so `scp`, `sftp`, `rsync` over SSH, and VS Code SFTP extensions work with no setup. Files are read and written as whoever started the binary.

It also runs one-off commands (`ssh booth 'whoami'`) and no-PTY sessions, which is what VS Code Remote-SSH needs to bootstrap. That path is experimental. Run it with `-vscode`:

```powershell
.\tailtap.exe -vscode -persist
```

`-vscode` turns on `-forward` (Remote-SSH uses dynamic forwarding) and, on Windows, runs the server as `sshd.exe`. Remote-SSH's Windows bootstrap walks up the process tree looking for a parent named `sshd` and gives up if there isn't one; `-vscode` copies the binary to `%TEMP%\sshd.exe` and runs from there so the check passes, then cleans it up on exit. Add `-persist` too, or the changing host key of an ephemeral node trips SSH's "host key changed" check (clear it with `ssh-keygen -R <name>` if that happens).

---

## Flags

| Flag | Default | What it does |
|------|---------|------|
| `-name` | `tailtap` | Hostname on the tailnet |
| `-persist` | `false` | Keep the same identity across reboots (not ephemeral) |
| `-forward` | `false` | Allow port forwarding (`ssh -L` and `-R`) |
| `-vscode` | `false` | Tune for VS Code Remote-SSH (enables `-forward`, runs as `sshd.exe` on Windows) |
| `-quiet` | `false` | Hide status logs (errors still print) |
| `-cleanup` | `false` | **Deprecated.** Auto-delete the binary; unreliable on Windows |
| `-minimize` | `false` | Minimize the console window (Windows only) |
| `-version` | | Print the version and exit |

For local dev you can skip the baked key and set `TS_AUTHKEY` in the environment.

### Port forwarding (`-forward`)

Off by default. Turn it on to tunnel a port:

```bash
ssh -L 8080:localhost:80 booth     # pull a service the node can reach onto your laptop
ssh -R 9000:localhost:9000 booth   # push a service on your laptop onto the node
```

### Require your SSH key (optional)

Add your public key on top of the tailnet check:

```bash
TAILTAP_AUTHORIZED_KEYS="$(cat ~/.ssh/id_ed25519.pub)" ./tailtap -name booth
# or bake it in: go build -ldflags "-X main.authKey=$KEY -X 'main.authorizedKeys=ssh-ed25519 AAAA... you@laptop'" .
```

Multiple keys: one per line.

---

## Cleaning up

- Kill the process — an ephemeral node drops off your tailnet in a few minutes.
- Delete the binary off the target; it carries a live key.
- Revoke the key.

The `-cleanup` flag (auto-delete the binary) is **deprecated** for now — it's unreliable on Windows. Simplest no-trace option: run from a USB stick and pull it out when you're done.

---

## Why a custom SSH server?

Tailscale ships its own SSH, and `tsnet` can expose it with one import. tailtap doesn't use it, on purpose:

- Tailscale SSH needs a separate SSH policy in the admin console and maps you to a real local user. More to set up, and easy to lock yourself out.
- It doesn't run on Windows at all.

Using it would mean two systems — Tailscale SSH on Linux and Mac, a custom one on Windows — and a fussier setup. One small server that acts the same everywhere fits this job better. If you run normal servers with real accounts and want audited per-user logins, Tailscale SSH is the better pick. That's a different job.

---

## Build from source

Go 1.24 or newer, no CGO.

```bash
git clone https://github.com/egemenertugrul/tailtap
cd tailtap
go build .    # dev build; set TS_AUTHKEY at runtime instead of baking a key
```

## Built on

[`tsnet`](https://pkg.go.dev/tailscale.com/tsnet), [`gliderlabs/ssh`](https://github.com/gliderlabs/ssh), [`go-pty`](https://github.com/aymanbagabas/go-pty), [`pkg/sftp`](https://github.com/pkg/sftp), and [`x/crypto/ssh`](https://pkg.go.dev/golang.org/x/crypto/ssh).

## License

MIT. See [LICENSE](./LICENSE).
