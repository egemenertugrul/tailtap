# tailtap

**A portable binary to quickly let a PC in your tailnet.**

When a compiled tailtap binary is executed, the PC immediately joins your tailnet and serves a shell. Then, you may connect to it by its name. After you stop it, the machine drops off the network.

It's useful to get into a machine at a venue, lab, or client site for the length of a job. You can let AI agents like Claude or GPT to control your headless PCs. Or just use as an automation target.

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

## Features

- **One self-contained binary.** Nothing to install on the target, no admin or root.
- **Joins your Tailscale network by itself** and serves its own SSH server, bound only to the tailnet.
- **Shell, SFTP, and scp** over a single connection.
- **Port forwarding and a SOCKS proxy** (`ssh -L`, `-R`, `-D`), plus **VS Code Remote-SSH** support.
- **Optional file browser** (`-web`) to upload and download from a web browser.
- **Windows, Linux, and macOS.** Static build, no dependencies.
- **Ephemeral by default**, so it leaves nothing behind. `-persist` keeps a stable identity.
- **Auth is your tailnet and ACL**, not passwords or keys on the target.
- **Bake the key, name, and flags at build time** so it runs with a double-click.

---

## Connect

Run the binary on the target. It prints the name it came up under, then you reach it from your laptop by that name. By default the name is the target machine's own hostname, so two machines never clash:

```bash
ssh mymachine
```

No password, no key prompt. To choose the name yourself, pass `-name`, or bake one in at build time (`NAME=booth`, see [Build](#build)), and connect by that instead:

```bash
ssh booth              # if you ran: tailtap -name booth
```

You can also use the tailnet IP the binary prints when it starts:

```bash
ssh 100.101.102.103
```

The username doesn't matter. The tailnet decides who gets in, and the shell runs as whoever started the binary. Files go over the same connection:

```bash
scp report.pdf booth:/tmp/
sftp booth
```

If the name won't resolve, MagicDNS is probably off on your laptop. Use the IP instead.

---

## How it works

- Embeds a Tailscale node with [`tsnet`](https://pkg.go.dev/tailscale.com/tsnet). It's userspace, so no admin or root, and it never touches the machine's real port 22.
- Authenticates with an ephemeral auth key baked into the binary at build time.
- Runs its own SSH server ([`gliderlabs/ssh`](https://github.com/gliderlabs/ssh)) bound only to the tailnet.
- The tailnet is the login: if your ACL lets you reach the node, you're in. No separate password or key by default.
- Cross-platform PTY ([`go-pty`](https://github.com/aymanbagabas/go-pty)) so full-screen apps, colors, and resize work. SFTP and scp too.

Nothing here relies on the code being secret. Safety is your auth key and ACL. That's why it's fine to make public.

---

## Where it fits

There are two halves to reaching a machine over Tailscale: the **client** you connect from, and the **server** you connect to.

tailtap is the server. You run it on the machine you want to reach. A tool like [`ts-ssh`](https://github.com/derekg/ts-ssh) is a client. It connects to machines that already run a server. Use plain `ssh` or ts-ssh to reach a tailtap node.

Reach for tailtap when a machine has no SSH server and no Tailscale yet, and you want to fix that in one step and undo it cleanly.

---

## Security

Safety is all in how you make the key and the ACL:

1. Make the auth key **ephemeral, expiring, and tagged**: Reusable, Ephemeral, Pre-authorized, tag `tag:tailtap`, short expiry.
2. The key is baked into every binary you build, so treat each one like a password: delete it off the target after the job, then revoke the key.
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

You can also require your own SSH key on top of the tailnet check. See [Flags](#flags).

---

## Build

### Where the key comes from

tailtap needs a Tailscale auth key to join your tailnet, and there are two ways to give it one. They lead to a different experience:

- **Baked in at build time.** Run `./build.sh <key>` with your own key and it ends up inside the binary. This is the drop-it-and-go path: the exe runs with nothing to type, so someone can double-click it or you can leave it on a USB stick. It is also why a keyed binary is a live credential, which is why you never publish one.
- **Given at runtime.** A binary with no baked key reads `TS_AUTHKEY` from the environment instead:

  ```bash
  TS_AUTHKEY=tskey-auth-xxxx ./tailtap -name booth
  ```

Anything published here is keyless, because a public download can't safely carry a key. That means the [GitHub Releases](../../releases) and a plain `go build`. A keyless binary is the same tool and behaves the same once it's up, but you supply the key on every run and there is no double-click-and-done. For that experience, build your own with your key, as below.

### Build with your key baked in

Bring your own auth key. It's injected at build time and never stored in the repo.

```bash
./build.sh tskey-auth-xxxx                           # or: KEY=tskey-auth-xxxx ./build.sh
NAME=booth ./build.sh tskey-auth-xxxx                # bake a default hostname
FLAGS="-vscode -persist" ./build.sh tskey-auth-xxxx  # bake in flags so it needs no arguments
```

Baked-in flags run automatically at startup, so someone can just double-click the exe with nothing to type. Anything passed on the command line still overrides them. You can combine `NAME` and `FLAGS`. Baked flag values cannot contain spaces.

Output is static and dependency-free (`CGO_ENABLED=0`, stripped):

| File | Runs on |
|------|--------|
| `tailtap-linux-amd64`        | Intel or AMD Linux |
| `tailtap-linux-arm64`        | ARM Linux (Raspberry Pi, SBCs) |
| `tailtap-windows-amd64.exe`  | 64-bit Windows 10/11 |
| `tailtap-macos-arm64`        | Apple Silicon Mac |
| `tailtap-macos-amd64`        | Intel Mac |

Don't commit `dist/`. Every binary carries a live key. It's gitignored.

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
2. Run it (no admin needed):
   - **Linux and macOS:** `./tailtap-linux-amd64 -name booth`
   - **Windows:** double-click, or `.\tailtap-windows-amd64.exe -name booth`. It's unsigned, so SmartScreen warns: click More info, then Run anyway. The first run may also show a Windows Firewall prompt. It asks to let the app talk on the network, not for admin over the machine, and you can allow or cancel it. Tailscale still connects through its relay either way.
   - **macOS:** first run, right-click → Open (or `xattr -d com.apple.quarantine ./tailtap-macos-arm64`).
3. It shows up in your device list, tagged `tag:tailtap`, under the name you gave it (or the machine's hostname by default). Connect with `ssh booth`.

The examples above assume the key is baked in (a `./build.sh` binary). If you downloaded a keyless one (a Release, or `go build`), pass your key on the run, since nothing is baked in: `TS_AUTHKEY=tskey-auth-xxxx ./tailtap-linux-amd64 -name booth`. See [Where the key comes from](#where-the-key-comes-from).

Give each machine a unique name, or Tailscale appends a number (`booth-1`). Add `-persist` to keep the same identity across reboots. Without it the node is ephemeral and vanishes when it stops.

It keeps running in the foreground and prints a line for each connection plus a heartbeat every so often. `-quiet` silences those. Stop it with Ctrl+C or by closing the window.

---

## Files and editors

SFTP is always on, over the same connection, so `scp`, `sftp`, `rsync` over SSH, and VS Code SFTP extensions work with no setup. Files are read and written as whoever started the binary.

It also runs one-off commands (`ssh booth 'whoami'`) and no-PTY sessions, so **VS Code Remote-SSH works** (tested against a Windows target). Start it with `-vscode`:

```powershell
.\tailtap.exe -vscode -persist
```

`-vscode` turns on `-forward` (Remote-SSH uses dynamic forwarding) and, on Windows, runs the server as `sshd.exe`. Remote-SSH's Windows bootstrap walks the process tree for a parent named `sshd` and gives up otherwise, so `-vscode` copies the binary to `%TEMP%\sshd.exe`, runs from there, and cleans it up on exit. Use `-persist` too, so the host key stays stable; an ephemeral node's key changes each run, which trips SSH's "host key changed" check and disables forwarding. (If that happens, clear it with `ssh-keygen -R <name>`.)

Then in VS Code connect to the node by name, just like `ssh`.

Since you get a real interactive shell, you can also run CLI tools on the target and drive them from your laptop, for example a coding agent like Claude Code.

### Browse files from a browser

Add `-web` and tailtap serves a small file page over the tailnet, on the node's port 80:

```powershell
.\tailtap.exe -web
```

Open `http://booth/` on your laptop to list, download, and upload files. It serves the current directory by default; point it elsewhere with `-webroot`. The same tailnet ACL guards it, and files are read and written as whoever started the binary. Useful when the other end can't use scp or SFTP.

---

## Flags

| Flag | Default | What it does |
|------|---------|------|
| `-name` | machine hostname | Hostname on the tailnet |
| `-persist` | `false` | Keep the same identity across reboots (not ephemeral) |
| `-forward` | `false` | Allow port forwarding and SOCKS (`ssh -L`, `-R`, `-D`) |
| `-shell` | auto | Shell to run for sessions, overriding auto-detection |
| `-web` | `false` | Serve a browser file page over the tailnet (download/upload) |
| `-webroot` | `.` | Directory `-web` serves |
| `-vscode` | `false` | Tune for VS Code Remote-SSH (enables `-forward`, runs as `sshd.exe` on Windows) |
| `-quiet` | `false` | Hide status logs and the heartbeat (errors still print) |
| `-cleanup` | `false` | **Deprecated.** Auto-delete the binary; unreliable on Windows |
| `-minimize` | `false` | Minimize the console window (Windows only) |
| `-version` | | Print the version and exit |

For local dev you can skip the baked key and set `TS_AUTHKEY` in the environment.

### Port forwarding (`-forward`)

Off by default. Turn it on to tunnel a port or proxy through the node:

```bash
ssh -L 8080:localhost:80 booth     # pull a service the node can reach onto your laptop
ssh -R 9000:localhost:9000 booth   # push a service on your laptop onto the node
ssh -D 1080 booth                  # SOCKS proxy out through the node
```

`-L` reaches a specific host the node can see, like a printer or a device on the venue LAN, so you rarely need a Tailscale subnet router. `-D` gives you a SOCKS proxy that goes out through the node, which covers the exit-node case of "come out where the node is." Both run in userspace with no admin and no extra setup, which a real subnet router or exit node would need.

### Require your SSH key (optional)

Add your public key on top of the tailnet check:

```bash
TAILTAP_AUTHORIZED_KEYS="$(cat ~/.ssh/id_ed25519.pub)" ./tailtap -name booth
# or bake it in: go build -ldflags "-X main.authKey=$KEY -X 'main.authorizedKeys=ssh-ed25519 AAAA... you@laptop'" .
```

Multiple keys: one per line.

---

## Cleaning up

- Kill the process. The node goes offline right away, and Tailscale auto-removes an ephemeral node from the list a bit later (normally 30 to 60 minutes). To free the name sooner, delete it in the admin console.
- Delete the binary off the target; it carries a live key.
- Revoke the key.

The `-cleanup` flag (auto-delete the binary) is **deprecated** for now because it's unreliable on Windows. Simplest no-trace option: run from a USB stick and pull it out when you're done.

---

## Why a custom SSH server?

Tailscale ships its own SSH, and `tsnet` can expose it with one import. tailtap doesn't use it, on purpose:

- Tailscale SSH needs a separate SSH policy in the admin console and maps you to a real local user. More to set up, and easy to lock yourself out.
- It doesn't run on Windows at all.

Using it would mean two systems (Tailscale SSH on Linux and Mac, a custom one on Windows) and a fussier setup. One small server that acts the same everywhere fits this job better. If you run normal servers with real accounts and want audited per-user logins, Tailscale SSH is the better pick. That's a different job.

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
