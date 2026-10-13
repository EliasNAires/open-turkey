# Open Turkey

A command-line productivity blocker for Linux, backed by a `systemd` service.

Open Turkey is an open-source, Linux-native alternative to [Cold Turkey](https://getcoldturkey.com/): it blocks distracting websites and applications and keeps them blocked, even against your own attempts to undo it on impulse. Where Cold Turkey is paid and centered on Windows/macOS, Open Turkey is free, built in Go, and designed around the way Linux actually enforces policy.

It organizes blocking into **blocks** — named groups of:

- **Sites** (domains, e.g. `youtube.com`)
- **Apps** (process names, e.g. `discord`, `telegram-desktop`)

When you **activate** a block, a daemon continuously ensures the blocking layers stay applied.

## How it works — 4 enforcement layers

Open Turkey doesn't rely on a single, easily-bypassed mechanism. Each active block is enforced on four independent layers, and a `systemd` daemon re-applies them every 5 seconds if anything is tampered with:

1. **`/etc/hosts`** — resolves blocked domains to `0.0.0.0` (affects every program, not just browsers).
2. **Firewall (iptables)** — blocks network connections to the blocked sites.
3. **Browser enterprise policies** — managed `URLBlocklist`/`WebsiteFilter` policies for Firefox, Chromium, Google Chrome and Brave. The user cannot disable these from inside the browser, not even in private/incognito mode.
4. **Process kill** — terminates blocked apps that are running (SIGKILL).

Because the hosts and firewall layers act below the browser, blocking works even for browsers installed via Snap or Flatpak (where the system-wide browser policy file may not be read).

## Installation

Prerequisites:

- Linux with `systemd`
- `iptables` available
- Go (to build), at `/usr/local/go/bin` or otherwise on your `PATH`

Recommended:

```bash
sudo ./install.sh
```

This:

- compiles the binary (`CGO_ENABLED=0`, pure-Go SQLite via `modernc.org/sqlite`)
- installs `open-turkey` (a wrapper) and `open-turkey-bin` (the real binary) into `/usr/local/bin`
- adds a rule in `/etc/sudoers.d/open-turkey` so it runs without a password prompt
- installs and starts the `systemd` service `open-turkey.service`
- creates the database at `/var/lib/open-turkey/open-turkey.db`

The installed program is independent of the source directory — once installed, you can remove the project folder and it keeps working.

## Concepts

- **Block**: a set of sites/apps you want to block together.
- **Active**: a block that is currently in effect (the system is enforcing it).
- **Lock (`--lock`)**: when enabled, prevents deactivation via `stop`. The only way to deactivate a locked block is `unlock`, which requires completing a typing challenge — deliberate friction to outlast an impulse.

## Commands

### 1) Create and configure a block

```bash
open-turkey block create social-media
open-turkey block add-site social-media instagram.com x.com facebook.com
open-turkey block add-app  social-media discord telegram
```

Show details:

```bash
open-turkey block info social-media
```

List all blocks:

```bash
open-turkey block list
```

### 2) Activate / deactivate

Activate:

```bash
open-turkey start social-media
```

Activate with a lock:

```bash
open-turkey start social-media --lock
```

Show status:

```bash
open-turkey status
```

Deactivate (only if not locked):

```bash
open-turkey stop social-media
```

Unlock a locked block (this also **deactivates** it):

```bash
open-turkey unlock social-media
```

### 3) Editing a block's lists

Remove a domain from a block:

```bash
open-turkey block remove-site social-media instagram.com
```

Notes:

- If the block is **active**, Open Turkey re-applies the layers so the change takes effect immediately.
- If the block is **active and locked**, you **cannot** edit its sites/apps. Run `open-turkey unlock <block>` first (which deactivates it), make your changes, then activate again.

Remove an app (process) from a block:

```bash
open-turkey block remove-app social-media discord
```

Remove an entire block (must be inactive):

```bash
open-turkey block remove social-media
```

### 4) Weekly schedule and one-off exceptions

`/etc/open-turkey/schedule.json` lists the windows in which a block is
**unblocked**; outside them it is blocked. `open-turkey-schedule.timer`
applies it every minute. Check it with:

```bash
open-turkey-schedule status
open-turkey-schedule status --at "2026-10-14 11:30"
```

For a one-time window (e.g. a class ended early), add an exception with
absolute dates instead of editing the weekly windows:

```json
"exceptions": [
  {"from": "2026-10-13 20:30", "to": "2026-10-13 22:30", "reason": "class ended early"}
]
```

- Exceptions apply once and expire on their own; past ones stay in the file as a log.
- `reason` is required.
- At most **2 h** each and **2 per week** (Monday to Sunday). These limits live in the code, not the file. An exception over a limit is ignored (`status` shows it as `IGNORADA`), and the rest of the schedule still applies.
- A block locked with `--lock` is never unblocked by the schedule or an exception.

If the windows are designed elsewhere (a calendar, a planning page), copy
them into `schedule.json` by hand. Do not wire that source to the blocker:
an outage there must not open or close the block.

## Service (daemon) and logs

The daemon is managed by `systemd`:

```bash
systemctl status open-turkey
systemctl restart open-turkey
journalctl -u open-turkey -f
```

## Uninstall

```bash
sudo make uninstall
```

## License

MIT — see [LICENSE](LICENSE).
