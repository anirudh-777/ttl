# Updates

ttl ships two update mechanisms: a passive one-line notice on every
command, and an explicit `ttl update` self-updater.

## Background notice

Once a day (per user), `ttl` calls the public GitHub API to ask
"what's the latest release?" and compares it to the running binary's
version. If newer, it prints a single line on **stderr**:

```
ttl: 0.4.1 installed, 0.5.0 available — run `ttl update`
```

This is best-effort:

- Silent if there's no network
- Silent if the API rate-limits
- Cached in `~/.config/ttl/.update-check` so it only hits the network
  once every 24 hours
- Disabled by setting `TTL_NO_UPDATE_CHECK=1`

The notice fires from `main()` before the subcommand runs, so it
shows up no matter which surface you're using (`serve`, `today`,
`cli add`, `version`, etc).

## Explicit upgrade

```bash
ttl update           # download latest, verify SHA256, replace binary
ttl update --check   # just print current vs latest, no download
ttl update --yes     # skip the "proceed? [y/N]" prompt
ttl update --version v0.5.0   # install a specific version
ttl update --to /usr/local/bin/ttl   # custom destination
```

What `ttl update` does, step by step:

1. Hits `api.github.com/repos/anirudh-777/ttl/releases/latest` to find
   the current tag.
2. Fetches `SHA256SUMS` for that tag.
3. Downloads `ttl-<os>-<arch>` for the running platform.
4. Verifies the SHA256 locally.
5. Atomically renames the new binary over the running one
   (POSIX-friendly; on Windows the running binary can't be replaced
   while in use — see "Windows" below).
6. Prints `updated. run \`/path/to/ttl version\` to verify.`

The upgrade is safe to run while `ttl serve` is running: POSIX
kernels keep the old inode pinned in memory, so existing connections
finish on the old code and new ones pick up the new one after the
next process restart.

## CI / scripting

For unattended upgrades in scripts, always pass `--yes`:

```bash
ttl update --yes
```

And check the exit code:

```bash
ttl update --yes || echo "update failed: $?"
```

If you don't want to rely on GitHub at all, you can still install a
specific version via the standalone installer:

```bash
curl -sSL https://raw.githubusercontent.com/anirudh-777/ttl/main/scripts/install.sh \
  | bash -s -- --version v0.5.0
```

## Windows

`ttl update` cannot replace a running executable on Windows (the
kernel holds an exclusive lock). The recommended workflow:

```powershell
# Stop the server first
ttl serve --addr :8093   # ... then Ctrl-C

# Update
ttl update --yes

# Restart
ttl serve --addr :8093
```

In practice most Windows users installed via the standalone installer
or `go install`, so they re-run those instead.

## Disabling

```bash
# Per-invocation
TTL_NO_UPDATE_CHECK=1 ttl today

# Per-user (add to ~/.zshrc / ~/.bashrc / PowerShell profile)
export TTL_NO_UPDATE_CHECK=1
```

## Privacy

The only network calls are unauthenticated GETs to:

- `api.github.com/repos/anirudh-777/ttl/releases/latest`
- `github.com/anirudh-777/ttl/releases/download/<tag>/SHA256SUMS`
- `github.com/anirudh-777/ttl/releases/download/<tag>/ttl-<os>-<arch>`

No telemetry, no user data, no cookies, no analytics. GitHub logs the
source IP per their standard policy.

## Cache file

The "last checked" timestamp and "latest known" version are stored at:

```
$XDG_CONFIG_HOME/ttl/.update-check      # if XDG_CONFIG_HOME is set
~/.config/ttl/.update-check              # otherwise
```

Delete this file to force an immediate re-check.

## Internal: how a release gets noticed

```
you cut a tag                  GitHub publishes the release
   v0.5.0                      /releases/latest returns tag_name="v0.5.0"
       \                          /
        \                        /
    gh release create       ttl reads the API, finds v0.5.0 > v0.4.1
        |                    prints notice on stderr
        v                        |
   users see:                  users see:
   "ttl: 0.4.1 installed,      "ttl: 0.4.1 installed,
    0.5.0 available — run        0.5.0 available — run
    `ttl update`"                `ttl update`"
                                    |
                                    v
                            ttl update downloads v0.5.0,
                            verifies SHA256, replaces binary
```

No central service. No push notifications. Just a public RSS-like
feed that every installed binary polls once a day.
