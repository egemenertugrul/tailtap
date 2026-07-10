# tailtap

**One self-contained binary that gives you an SSH shell into any machine over your private Tailscale network — no install, no login on the target, no SSH keys to exchange.**

Copy `tailtap` to a Windows / Linux / macOS box, run it, and it joins *your* tailnet and exposes an interactive shell. You connect from your own laptop by hostname — no password, no key prompt. Kill it and the node disappears.

Built for "walk up to a machine at a venue / lab / client site, get remote access for the duration of a job, leave nothing behind." Works great as a target for automation (e.g. Claude Code).

---

## How it works

- Embeds a Tailscale node via **[`tsnet`](https://pkg.go.dev/tailscale.com/tsnet)** (userspace networking — no admin/root, never touches the machine's real port 22).
- Authenticates with a **pre-authorized, ephemeral auth key** baked in at build time (`-ldflags -X`).
- Runs an SSH server (via [`gliderlabs/ssh`](https://github.com/gliderlabs/ssh)) bound **only** to the tailnet listener.
- **Auth is the tailnet itself:** if your ACL lets you reach the node, you're in — same model as Tailscale SSH. The SSH server does no separate password/key check by default.
- Cross-platform PTY (Unix pty + Windows ConPTY) via [`go-pty`](https://github.com/aymanbagabas/go-pty), so full-screen TUIs, colors, and resize work.

Security does **not** rely on this code being secret — it rests on your Tailscale auth key + ACL. That's why this repo is safe to publish.

---

## Security model — read this before using

`tailtap` is an **authorized-access** tool for machines *you control*. Its safety comes entirely from how you provision the key and ACL:

1. **Auth key must be ephemeral + expiring + tagged.** Generate it with: Reusable, Ephemeral, Pre-authorized, Tag `tag:tailtap`, short expiry.
2. **The key is a secret once baked in.** Every built binary in `dist/` contains a live key — treat each copy like a password. Delete it off the target when the job is done, and revoke/rotate the key.
3. **Contain it with an ACL** so a tailtap node is reachable only by you and can reach nothing else:

```jsonc
{
  "tagOwners": { "tag:tailtap": ["you@example.com"] },
  "acls": [
    // you can reach tailtap nodes on the SSH port
    { "action": "accept", "src": ["you@example.com"], "dst": ["tag:tailtap:22"] }
    // no rule granting tag:tailtap outbound = it can't touch the rest of your tailnet
  ]
}
```

> This ACL governs **this custom SSH server**. Tailscale's own `ssh` ACL block only applies to real Tailscale-SSH nodes — tailtap runs its own server, so the reachability rule above is what matters.

Optionally require your own SSH public key *on top* of the tailnet check — see the commented `PublicKeyHandler` in [`sshserver.go`](./sshserver.go).

---

## Build

You supply your own tailnet auth key; it is injected at build time and never stored in the repo.

```bash
./build.sh tskey-auth-xxxxxxxxxxxx      # builds every target into dist/
# or
KEY=tskey-auth-xxxx ./build.sh
```

Produces static, zero-dependency binaries (`CGO_ENABLED=0`, symbols stripped):

| File | Target |
|------|--------|
| `tailtap-linux-amd64`        | Intel/AMD Linux |
| `tailtap-linux-arm64`        | ARM Linux (Raspberry Pi, SBCs) |
| `tailtap-windows-amd64.exe`  | 64-bit Windows 10/11 |
| `tailtap-macos-arm64`        | Apple Silicon Mac |
| `tailtap-macos-amd64`        | Intel Mac |

> **Never commit `dist/`.** It's gitignored because every binary carries a live key.

---

## Deploy & run on the target

1. Copy the matching binary to the machine (USB stick, or a download link if it has internet).
2. Run it — no admin/root needed:
   - **Linux/macOS:** `./tailtap-linux-amd64 -name job-gallery-1`
   - **Windows:** double-click, or `.\tailtap-windows-amd64.exe -name job-gallery-1`
     - SmartScreen ("unknown publisher") → *More info → Run anyway* (binary is unsigned).
   - **macOS Gatekeeper:** right-click → Open the first time, or `xattr -d com.apple.quarantine ./tailtap-macos-arm64`.
3. The machine appears in your Tailscale device list under `-name`, tagged `tag:tailtap`.

Add `-persist` to reconnect as the same node across reboots (otherwise it's ephemeral and auto-deregisters when it goes offline).

## Connect

```bash
ssh you@job-gallery-1     # tailnet MagicDNS name — no password, no key prompt
```

`tailtap` always launches an **interactive shell** (it ignores a command passed as `ssh host 'cmd'` — request a PTY). Point your automation at the hostname.

---

## Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-name` | `tailtap` | Hostname on the tailnet |
| `-persist` | `false` | Reconnect as the same node across runs/reboots (non-ephemeral) |

You can also skip the baked-in key for local dev by setting `TS_AUTHKEY` in the environment.

---

## Cleaning up

- Kill the process → an ephemeral node auto-deregisters from your tailnet within a few minutes.
- **Delete the binary** off the target — it carries a live key.
- **Revoke/rotate the auth key** after the job.

---

## Design notes — why a custom SSH server?

Tailscale has its own built-in SSH, and you can turn it on inside this kind of binary with one import (`tsnet`'s `ListenSSH`). We deliberately **don't** use it. Here's the plain-English reason.

**What tailtap does:** it runs its own tiny SSH server and, when you connect, just opens a normal shell as whoever started the binary. The rule for "who's allowed in" is simple: *if you can reach the machine over the tailnet, you're in.* One switch: reachability.

**What Tailscale's built-in SSH would do instead:**

- **Different, heavier permission setup.** It ignores the simple "can you reach it" rule and instead needs a separate *SSH policy* block in your Tailscale admin console. Forget to add it → you get locked out, not let in.
- **It has to pick a real user account** on the target machine and match it to a policy entry. On a random machine you just walked up to, "log in as *which* user?" becomes a question you have to answer up front.
- **It doesn't work on Windows at all.** Tailscale's SSH server is Linux/Mac only. So using it wouldn't *replace* our code — it would force us to run Tailscale's SSH on Linux/Mac **and** keep our custom server for Windows. Two systems instead of one.

**So the trade is:** the built-in option is slightly less code *on Linux only*, in exchange for a more complicated permission setup, a "which user?" question on every machine, and a split between Windows and everything else.

For tailtap's job — *drop one identical binary on any machine, get a shell, leave nothing behind* — the custom server wins: **one code path for all platforms, and one dead-simple rule for access.** Tailscale's built-in SSH is the better choice for a different job: normal servers you own, with real user accounts, where you want per-identity, audited logins. That's not this.

---

## Building from source

Requires Go 1.24+. No CGO.

```bash
git clone https://github.com/egemenertugrul/tailtap
cd tailtap
go build .            # dev build; set TS_AUTHKEY at runtime instead of baking a key
```

## License

MIT — see [LICENSE](./LICENSE).
