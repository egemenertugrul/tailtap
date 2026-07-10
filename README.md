# tailtap

**One small binary that gives you an SSH shell into any machine over your private Tailscale network. No install, no login on the target, no SSH keys to swap.**

Copy `tailtap` to a Windows, Linux, or macOS machine and run it. It joins your tailnet and opens an interactive shell. You connect from your own laptop by hostname, with no password and no key prompt. Kill it and the machine disappears from your network.

It is built for one job: walk up to a machine at a venue, lab, or client site, get remote access for the length of a job, and leave nothing behind. It also works well as a target for automation like Claude Code.

```
     your laptop                          the target machine
  (on your tailnet)                      (any OS, no install)
        │                                        │
        │      encrypted Tailscale tailnet       │
        │        (WireGuard, no open ports)      │
        │                                        ▼
   ssh you@target ───────────────────────►   tailtap
        │                                     ├─ interactive shell
        ▼                                     └─ SFTP / scp
   you get a shell            joins your tailnet with an ephemeral, tagged key
                              and disappears again when it stops
```

The only thing crossing the network is the Tailscale connection. tailtap opens no ports on the local network, and the target needs no Tailscale install, no admin rights, and no inbound firewall changes.

---

## How it works

- It embeds a Tailscale node using [`tsnet`](https://pkg.go.dev/tailscale.com/tsnet). This is userspace networking, so it needs no admin or root and never touches the machine's real port 22.
- It logs in with a pre-authorized, ephemeral auth key that is baked into the binary at build time.
- It runs an SSH server (via [`gliderlabs/ssh`](https://github.com/gliderlabs/ssh)) that listens only on the tailnet, never on the local network.
- The tailnet itself is the login. If your ACL lets you reach the node, you are in. By default the SSH server does no extra password or key check.
- It uses a cross-platform PTY (Unix pty plus Windows ConPTY) via [`go-pty`](https://github.com/aymanbagabas/go-pty), so full-screen apps, colors, and resizing all work.
- It supports file transfer (SFTP and scp) through a [`pkg/sftp`](https://github.com/pkg/sftp) subsystem on the same connection.

Safety does not depend on the code being secret. It depends on your Tailscale auth key and ACL. That is why this repo is safe to make public.

---

## Security model (read this before using)

`tailtap` is a tool for reaching machines you control. Its safety comes entirely from how you set up the key and the ACL:

1. **The auth key must be ephemeral, expiring, and tagged.** Create it with these options: Reusable, Ephemeral, Pre-authorized, Tag `tag:tailtap`, and a short expiry.
2. **The key is a secret once it is baked in.** Every binary in `dist/` contains a live key, so treat each copy like a password. Delete it off the target when the job is done, then revoke or rotate the key.
3. **Fence it in with an ACL** so a tailtap node can be reached only by you and can reach nothing else:

```jsonc
{
  "tagOwners": { "tag:tailtap": ["you@example.com"] },
  "acls": [
    // you can reach tailtap nodes on the SSH port
    { "action": "accept", "src": ["you@example.com"], "dst": ["tag:tailtap:22"] }
    // no rule giving tag:tailtap any outbound access means it cannot touch the rest of your tailnet
  ]
}
```

Note: this ACL controls tailtap's own SSH server. Tailscale's built-in `ssh` ACL block only applies to real Tailscale-SSH nodes, and tailtap runs its own server, so the reachability rule above is the one that matters.

You can also require your own SSH public key on top of the tailnet check. See [Extra hardening](#extra-hardening-require-your-ssh-key-optional) below.

---

## Build

You supply your own tailnet auth key. It is injected at build time and never stored in the repo. There are three ways to provide it:

```bash
./build.sh tskey-auth-xxxxxxxxxxxx                    # pass a key directly
KEY=tskey-auth-xxxx ./build.sh                        # pass it through the environment
TS_CLIENT_ID=... TS_CLIENT_SECRET=... ./build.sh      # auto-create a fresh key (see below)
```

To bake a default hostname into the binary so it needs no `-name` at runtime, set `NAME`:

```bash
NAME=booth ./build.sh tskey-auth-xxxx    # binary defaults to hostname "booth"; -name still overrides
```

### Auto-creating keys with OAuth (optional)

Instead of clicking a key out of the admin console every time, create a Tailscale OAuth client once (scope: Keys, Auth Keys, Write; allowed tag `tag:tailtap`) and let [`mint-key.sh`](./mint-key.sh) create keys for you:

```bash
export TS_CLIENT_ID=...  TS_CLIENT_SECRET=...
./build.sh                      # creates a fresh ephemeral, tagged key, then builds
```

By default it creates a fresh key on every build. This fits the "revoke the key after each job" workflow, because one key maps to one job's binaries. If you would rather reduce the number of keys, set `TS_KEY_REUSE=1`. It then caches the key under `~/.tailtap` and reuses it until it is within `TS_KEY_RENEW_BEFORE_DAYS` (default 7) of expiry, checking the live API each run so revocations are noticed. This needs `curl` and `jq`. See the header of `mint-key.sh` for all the settings.

The build produces static, dependency-free binaries (`CGO_ENABLED=0`, symbols stripped):

| File | Runs on |
|------|--------|
| `tailtap-linux-amd64`        | Intel or AMD Linux |
| `tailtap-linux-arm64`        | ARM Linux (Raspberry Pi, SBCs) |
| `tailtap-windows-amd64.exe`  | 64-bit Windows 10/11 |
| `tailtap-macos-arm64`        | Apple Silicon Mac |
| `tailtap-macos-amd64`        | Intel Mac |

Never commit `dist/`. It is gitignored because every binary carries a live key.

---

## Deploy and run on the target

1. Copy the matching binary to the machine (a USB stick is most reliable at a venue, or a download link if it has internet).
2. Run it. No admin or root is needed:
   - **Linux and macOS:** `./tailtap-linux-amd64 -name job-gallery-1`
   - **Windows:** double-click it, or run `.\tailtap-windows-amd64.exe -name job-gallery-1`. On the SmartScreen "unknown publisher" prompt, click More info, then Run anyway. The binary is unsigned.
   - **macOS Gatekeeper:** right-click and choose Open the first time, or run `xattr -d com.apple.quarantine ./tailtap-macos-arm64`.
3. The machine shows up in your Tailscale device list under `-name`, tagged `tag:tailtap`.

Add `-persist` to reconnect as the same node across reboots. Without it the node is ephemeral and removes itself when it goes offline.

## Connect

```bash
ssh you@job-gallery-1        # by tailnet MagicDNS name, no password, no key prompt
ssh you@100.101.102.103      # or by the tailnet IP that tailtap printed on startup
```

The username (`you@`) does not matter for login. The tailnet authorizes you, and the shell runs as whoever launched `tailtap` on the target. `tailtap` always opens an interactive shell. It ignores a command passed as `ssh host 'cmd'`, so request a PTY. Point your automation at the hostname.

### Pick a memorable name

The `-name` you pass becomes the SSH hostname, so give each machine a short, memorable name and connect with exactly that:

| Run on the target | Connect from your laptop |
|-------------------|--------------------------|
| `tailtap -name booth`      | `ssh booth` |
| `tailtap -name gallery-pc` | `ssh gallery-pc` |
| `tailtap -name lab-01`     | `ssh lab-01` |
| `tailtap -name reception`  | `ssh reception` |

Without `-name`, nodes get the default `tailtap`, and a second one becomes `tailtap-1`, `tailtap-2`, and so on, which is why a name is worth setting. Names must be unique on your tailnet; reusing a name for a new machine makes Tailscale append a number.

### File transfer (SFTP and scp)

The SSH server includes a standard `sftp` subsystem, so anything that speaks SFTP works over the same tailnet connection with no extra setup: `sftp`, `scp`, `rsync` over SSH, and editor remote-file tools like VS Code (Remote-SSH or an SFTP extension).

```bash
sftp you@job-gallery-1                     # interactive, by name
sftp you@100.101.102.103                   # or by tailnet IP
scp ./patch.zip you@job-gallery-1:/tmp/    # push (OpenSSH 9+ runs scp over SFTP)
scp you@100.101.102.103:/tmp/out.log .     # pull, by IP
```

Files are read and written as whatever user launched `tailtap`. Like the shell, SFTP does no login check beyond the tailnet.

---

## Flags

| Flag | Default | What it does |
|------|---------|------|
| `-name` | `tailtap` | Hostname on the tailnet |
| `-persist` | `false` | Reconnect as the same node across runs and reboots (not ephemeral) |
| `-forward` | `false` | Allow SSH port forwarding (`ssh -L` and `-R`) |
| `-quiet` | `false` | Hide tsnet and status logs (errors still print) |
| `-cleanup` | `false` | **Deprecated / experimental.** Delete the binary when done. Unreliable on Windows (see below) |
| `-minimize` | `false` | Minimize the console window on start (Windows only) |
| `-version` | | Print the version and exit |

For local development you can skip the baked-in key and set `TS_AUTHKEY` in the environment instead.

### Port forwarding (`-forward`)

With `-forward` you can tunnel a TCP port through the node. Two directions:

- **`-L` (local):** pull a service the node can reach onto your laptop.
- **`-R` (remote):** push a service on your laptop onto the node.

```bash
ssh -L 8080:localhost:80 you@job-gallery-1     # -L: a web page on the node shows up at localhost:8080 on your laptop
ssh -R 9000:localhost:9000 you@job-gallery-1   # -R: a service on your laptop's :9000 shows up on the node
```

It is off by default to keep the surface small. Only you can reach the node anyway, because of the ACL.

### Extra hardening: require your SSH key (optional)

By default, access is gated by the tailnet alone. To also require your SSH public key, provide it either baked in at build time or at runtime:

```bash
# baked in:
go build -ldflags "-X main.authKey=$KEY -X 'main.authorizedKeys=$(cat ~/.ssh/id_ed25519.pub)'" -o tailtap .
# or at runtime:
TAILTAP_AUTHORIZED_KEYS="$(cat ~/.ssh/id_ed25519.pub)" ./tailtap -name job-gallery-1
```

For more than one key, separate them with newlines (the authorized_keys format).

---

## Cleaning up

- Kill the process. An ephemeral node removes itself from your tailnet within a few minutes.
- Delete the binary off the target. It carries a live key.
- Revoke or rotate the auth key after the job.

### Deleting the binary automatically (`-cleanup`)

> **Deprecated / experimental.** The Windows path is currently unreliable, so `-cleanup` is not recommended yet. For a guaranteed no-trace run, use the USB-stick approach below instead. This flag will be reworked later.

`-cleanup` makes tailtap remove its own binary so nothing is left on disk:

- **Linux and macOS:** the file is unlinked the moment it starts. The process keeps running from memory, so even a hard power-off leaves nothing behind.
- **Windows:** a running `.exe` is locked, so the file is deleted when the process exits normally (Ctrl-C, or closing the console window). If it is hard-killed from Task Manager, the file stays and you delete it by hand.

The simplest zero-trace option needs no flag at all: run the binary straight from a USB stick and pull the stick when you are done.

---

## Design notes: why a custom SSH server?

Tailscale has its own built-in SSH, and you can turn it on inside this kind of binary with one import (`tsnet`'s `ListenSSH`). tailtap deliberately does not use it. Here is the plain reason.

**What tailtap does:** it runs its own small SSH server, and when you connect it opens a normal shell as whoever started the binary. The rule for who gets in is simple: if you can reach the machine over the tailnet, you are in. One thing to check: can you reach it.

**What Tailscale's built-in SSH would do instead:**

- **It needs a heavier permission setup.** It ignores the simple "can you reach it" rule and needs a separate SSH policy block in your Tailscale admin console. Forget to add it and you get locked out instead of let in.
- **It has to pick a real user account** on the target machine and match it to a policy entry. On a random machine you just walked up to, "log in as which user?" becomes a question you have to answer first.
- **It does not work on Windows at all.** Tailscale's SSH server is Linux and Mac only. So using it would not replace tailtap's code. It would mean running Tailscale's SSH on Linux and Mac and keeping the custom server for Windows. That is two systems instead of one.

**So the trade is this:** the built-in option is slightly less code, and only on Linux, in exchange for a more complicated permission setup, a "which user?" question on every machine, and a split between Windows and everything else.

For tailtap's job, which is to drop one identical binary on any machine, get a shell, and leave nothing behind, the custom server wins. It is one code path for every platform and one simple rule for access. Tailscale's built-in SSH is the better choice for a different job: normal servers you own, with real user accounts, where you want per-person, audited logins. That is not this.

---

## Building from source

Requires Go 1.24 or newer. No CGO.

```bash
git clone https://github.com/egemenertugrul/tailtap
cd tailtap
go build .            # dev build; set TS_AUTHKEY at runtime instead of baking a key
```

## Acknowledgements

tailtap is a thin layer of glue over some excellent work:

- [Tailscale `tsnet`](https://pkg.go.dev/tailscale.com/tsnet): the userspace tailnet node that makes "no install, no daemon, no root" possible.
- [`gliderlabs/ssh`](https://github.com/gliderlabs/ssh): the friendly SSH server API.
- [`aymanbagabas/go-pty`](https://github.com/aymanbagabas/go-pty): cross-platform PTY, including Windows ConPTY.
- [`pkg/sftp`](https://github.com/pkg/sftp): the SFTP subsystem.
- [`golang.org/x/crypto/ssh`](https://pkg.go.dev/golang.org/x/crypto/ssh): host-key plumbing.

Related projects worth knowing: [`ts-ssh`](https://github.com/derekg/ts-ssh) (a client-side tsnet SSH tool) and [`rospo`](https://github.com/ferama/rospo) (a single-binary reverse-tunnel SSH tool, no Tailscale). tailtap is different because it is the agent side. It joins your tailnet itself and serves a shell, rather than being a client or a classic reverse tunnel.

## License

MIT. See [LICENSE](./LICENSE).
