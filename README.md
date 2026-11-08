<img src="assets/icon.svg" width="64" height="64" alt="">

# Wishport

**Your servers, one SSH away.**

`wishport` is a standalone terminal directory for SSH-config aliases, Wishlist/YAML connections, Laravel Forge servers and sites, and optional network discovery. It can also host the directory over SSH. Search a domain or server, press Enter, and connect through your system OpenSSH client.

This is an independent Go application with original code. It has no dependency on Wishlist or another SSH manager. Charm libraries provide the terminal UI. Wishlist-compatible configuration and familiar navigation are the baseline, with Forge discovery and safe key preparation built in.

## Build and run

Requires Go 1.27.2 or newer and OpenSSH (`ssh`, plus `ssh-add` and `ssh-keygen` for key preparation).

```sh
git clone https://github.com/athphane/wishport.git
cd wishport
go build -trimpath -o wishport ./cmd/wishport
./wishport
./wishport list --json
./wishport check --json
```

Install the single executable somewhere on your PATH, for example:

```sh
mkdir -p ~/.local/bin
install -m 755 wishport ~/.local/bin/wishport
```

Linux is the primary platform. macOS builds are supported by the source and CI configuration, but have not been runtime-tested in the development environment. The release workflow produces Linux amd64/arm64 archives and checksums as a draft when a version tag is pushed; no release has been published.

## Keyboard controls

| Key | Action |
| --- | --- |
| `/`, Ctrl+K | Focus fuzzy search |
| Tab / Shift+Tab | Next / previous inventory tab (also while searching) |
| ←/→ | Switch tabs when search is unfocused |
| t | Toggle table/list view when search is unfocused |
| ↑/↓ | Select a result, including while searching |
| j/k | Select when search is unfocused |
| Enter / o | Connect to the selected result (o when search is unfocused) |
| y | Copy the selected address through the terminal clipboard (OSC 52) |
| PgUp/PgDn, b/f | Page through results (b/f when search is unfocused) |
| Home/End, g/G | First/last result when search is unfocused |
| Esc | Clear search, close help, or refuse key installation |
| Ctrl+R | Refresh Forge servers/sites and bypass cache TTL, including while searching |
| R | Same refresh when search is unfocused |
| ? | Toggle help |
| q/Q | Quit when search is unfocused |
| Ctrl+C | Cancel loading/preparation, otherwise quit |

Typing in search never triggers single-character shortcuts. The selected stable ID survives refresh. Local hosts are usable while Forge loads. OpenSSH temporarily takes over the terminal, and the directory returns after it exits. Key installation requires an explicit `y`; Enter never approves it. Resize a small terminal if it cannot display all consent details.

Prefix a search with **`server:`** to search only server/host names and local SSH aliases. For example, `server:production` (or fuzzy `server:prd`) shows matching Forge servers, SSH-config hosts, Wishlist hosts, and YAML hosts. Forge sites are excluded, and domains, IPs, users, organizations, and descriptions do not contribute matches. `server:` by itself lists all server/host entries. Prefixes and names are case-insensitive; remove the prefix to search everything again.

## Local inventory

Press **`t`** to switch between the original list and a compact table with a solid selected row. The table shows name, source, and SSH target, adding server and organization columns as the terminal widens. Small windows fall back to the list automatically. Tabs, search, selection, and SSH handoff behave the same in either view.

Forge key preparation displays a gradient progress bar labeled with its current workflow step, including explicit approval and waiting for installation. It measures completed stages, not elapsed time; successful authentication can skip installation entirely. Inventory refresh and other operations with no known total use a spinner.

Selecting any host shows connection preparation followed by a brief 200 ms launch bar before OpenSSH takes over the terminal. This indicates the handoff to OpenSSH, not successful remote authentication. Ctrl+C can cancel the pending launch.

The **All**, **Local**, and **Forge** tabs filter the directory. Enabling discovery adds a **Discovered** tab. Local includes SSH config, Wishlist, and YAML entries; Forge includes Forge servers and sites. Search, including `server:`, applies within the selected tab. Switching tabs keeps the search text and remembers each tab's selection for the current session; refresh preserves the active tab.

The SSH provider reads `~/.ssh/config` and recursive `Include` globs, falling back to `/etc/ssh/ssh_config` if the user file is absent. Concrete `Host` aliases appear once; wildcard-only and negated patterns do not become connections. Literal HostName/User values provide search hints. These hints do not evaluate `Match` conditions or replace OpenSSH resolution.

When you select an alias, `ssh -G` resolves its effective configuration. The final command still uses the alias, retaining ProxyJump, ProxyCommand, agent, known-hosts and other trusted local settings. `wishport` never rewrites your SSH config or stores destination private keys. Explicit hosted-mode startup creates a server host key if one is missing.

Existing **Wishlist OpenSSH and YAML configurations** load automatically from `.wishlist` in the working directory, then the user config directory (`wishlist.yaml`, `wishlist.yml`, `wishlist`, including directory layouts). Your existing `~/.config/wishlist` file continues to work. Use `--wishlist-config PATH` for an explicit file, or set `sources.wishlist_config` in wishport's config. Entries retain their own IDs and labels alongside other sources. OpenSSH files keep native `-F` semantics; Wishlist YAML supports endpoint SSH options, descriptions, links, and ordered discovery hints. See [Wishlist configuration support](docs/wishlist-config.md) for fields and intentional validation differences. Files are never migrated or rewritten.

Configuration lives at `$XDG_CONFIG_HOME/wishport/config.yaml`, falling back to `~/.config/wishport/config.yaml` on Linux and macOS. Override it with `--config PATH`. A missing default file is fine. A malformed file produces a warning while SSH-config hosts remain usable.

See [config.example.yaml](config.example.yaml). Schema version 1 accepts:

```yaml
version: 1
sources:
  ssh_config: true
  wishlist: true
forge:
  enabled: true
  cache_ttl: 168h
  key_install: ask       # ask or disabled
  public_key: ~/.ssh/id_ed25519.pub
connections:
  - id: home-pi
    name: raspberry-pi
    host: 192.0.2.10
    user: pi
    port: 22
    description: Home Raspberry Pi
  - id: staging
    ssh_alias: staging-alias
    directory: /srv/staging application
    identity: ~/.ssh/id_ed25519
```

Connection `id` is required and unique within YAML. `name` defaults to `id`. Supply `host` or `ssh_alias`; when both are present, the alias is the destination. Optional `user`, `port`, and `identity` override alias options. `directory` safely changes the remote directory before starting the user's login shell with a TTY; a failed change exits rather than falling back. Ports range from 1–65535. Unknown fields, duplicate fields/IDs, controls, invalid hosts/users and unsafe OpenSSH expansion tokens are rejected. Hostnames and aliases accept letters, digits, dots, underscores and hyphens; IPv6 addresses use bare literal form. No configuration field is a local shell fragment.

Stable target IDs are `ssh:<alias>`, `wishlist:<alias>`, `yaml:<id>` and `forge:<account-fingerprint>:<org-slug>:server:<id>` or `…:site:<id>`. Entries are never silently merged, including identical domains/IPs from different sources or organizations.

## Discovery and hosted mode

Discovery is disabled by default. Enable sources independently:

```sh
wishport --mdns
wishport --srv-domain example.com
wishport --tailscale-net example.com
wishport --wishlist-config /path/to/wishlist.yaml --mdns
wishport --refresh-interval 5m
```

Tailscale credentials come from `TAILSCALE_KEY` or `TAILSCALE_CLIENT_ID` plus `TAILSCALE_CLIENT_SECRET`; inject them through your secret manager, not command arguments or YAML. Configure persistent options under `discovery` as shown in [config.example.yaml](config.example.yaml). See [discovery documentation](docs/discovery.md) for API scopes, DNS records, timeouts, and hints. Failed refreshes retain previously discovered entries in memory with a warning.

To start a hosted directory, provide an existing public authorized-keys file:

```sh
wishport serve --authorized-keys /path/to/authorized_keys
ssh -t -p 2222 user@localhost
ssh -t -p 2222 user@localhost connect --target wishlist:production
```

The default listener is **127.0.0.1:2222**. `--listen` changes it; `--host-key` selects the persistent host key; `--metrics-listen 127.0.0.1:9090` enables aggregate Prometheus metrics. `--refresh-interval 5m` refreshes each active directory automatically. All authorized clients share the serving account's inventory and destination access; use a dedicated account where appropriate. Hosted mode accepts a directory/target request, not arbitrary local shell commands. See [hosted mode](docs/server.md).

Direct local selection accepts a stable ID or an unambiguous exact name:

```sh
wishport connect --target wishlist:production
wishport production
```

Generate shell completion with `wishport completion bash`, `wishport completion zsh`, or `wishport completion fish`; `wishport man` prints a roff manual page. These commands do not query the network or read credentials.

See the [Wishlist feature audit](docs/wishlist-parity.md) for coverage and differences. This is behavioral compatibility in an independent application, not Wishlist's Go embedding API or a drop-in CLI replacement.

## Connect Forge

Create a token in [Forge's API settings](https://forge.laravel.com/user-profile/api). Minimum read scopes are **organization:view** and **server:view**. Add **server:create-keys** only if you want optional SSH-key installation. These permissions are verified against the [current API contract](docs/forge-api.md).

```sh
wishport login
wishport list --refresh --json
```

`login` reads the token without echo and stores it through Linux Secret Service's `secret-tool` when available (provided by `libsecret` on Arch). If the keyring is unavailable, it offers a one-session directory. `wishport login --session` explicitly keeps the token in process memory for that session. macOS currently supports session login and environment tokens, not native Keychain storage.

For automation, inject `FORGE_API_TOKEN` using your secret manager. It overrides the desktop keyring. Tokens never appear in subprocess arguments, YAML, logs or JSON. There is no token CLI argument.

```sh
wishport forget-token
```

This clears the stored token and the matching inventory caches for the stored token and current environment override. A running process cannot unset its parent shell's environment; unset `FORGE_API_TOKEN` yourself if present. If the desktop keyring cannot be accessed, the command reports that it could not verify/clear the stored token.

Forge server entries use the API's server IP, SSH port and `forge` user. Site entries use that server IP, the site's own SSH user, and its actual `root_directory`—never the site's DNS. Missing relationships, missing roots, revoked servers and unready servers are visibly unavailable. No deployment or general server management endpoints are used.

## Offline cache and refresh

Caches live at `$XDG_CACHE_HOME/wishport/`, falling back to `~/.cache/wishport/`. Each API base/token fingerprint has a separate snapshot. Only an explicit allowlist of connection/display fields is saved, with 0700 directories, 0600 files, and atomic replacement. Tokens, private keys, deployment URLs/scripts, and raw API responses are excluded.

The default TTL is seven days. The UI displays cached data and its timestamp promptly, then refreshes stale data in the background. `R` and `--refresh` bypass TTL. Failed or partial refreshes retain the last complete snapshot; without a snapshot, usable partial results are shown with warnings. Credentials must still be available to select their cache partition; the application never guesses an account from leftover files. Local sources work without a token.

## Safe public-key preparation

Forge connections first perform a bounded, noninteractive public-key probe with strict host-key checking. Already-authorized keys connect without any Forge write. Otherwise installation requires all of the following:

1. A known, unchanged host key and an unambiguous public-key rejection.
2. Live Forge verification of the exact organization, server, IP, port, site user and root.
3. An existing public key whose matching private key is available through the agent or usable locally without a passphrase prompt.
4. Evidence that OpenSSH offered that exact key, followed by explicit approval of the target and fingerprint.

The target is verified again after consent. Upload names are deterministic from the public-key fingerprint and SSH user. Existing key records are reused; one POST is followed by bounded status polling and authentication retries. Canceling after submission does not delete the key, and the UI warns that Forge may still finish.

An unknown host opens normal interactive OpenSSH verification without uploading anything. Independently verify the host fingerprint before accepting it; if authentication then fails, return to the directory and select again. A changed host key is a hard stop. The app never uses `ssh-keyscan` to establish trust and never sets permissive host-key options. Network failures, locked keys, missing keys, mismatched private/public keys and ambiguous output do not trigger uploads.

Set `forge.public_key` or use `prepare --public-key` to select a key. Without a configured path, a sole `id_ed25519.pub` or `id_rsa.pub` is usable; if both exist, the app asks you to configure a choice. Other public-key locations must be selected explicitly. Load encrypted keys with `ssh-add` first. Public-key parsing and the optional hosted listener use `x/crypto/ssh`; **all outbound connections to destination servers use system OpenSSH**.

Set `forge.key_install: disabled` to connect normally without smart preparation. Public-key provisioning is never performed by startup, inventory refresh, `list`, `check`, or `command`.

## CLI and JSON

```sh
wishport list --json
wishport list --refresh --json
wishport check --json
wishport command --target 'yaml:home-pi' --json
wishport prepare --target '<id-from-list>' --public-key ~/.ssh/id_ed25519.pub --json
```

`prepare` may upload a public key. It asks for the exact word `yes` on a terminal; unattended uploads require `--yes`. This flag approves consent only—it never bypasses target validation, host-key verification or usable-key checks. `prepare` does not open an interactive shell. `command` returns an argument array without connecting; resolving trusted SSH config via `ssh -G` may evaluate the user's existing `Match exec` directives.

JSON uses `{"schema_version":1,"data":...}` or `{"schema_version":1,"error":"..."}`. Inventory data contains `connections`, `warnings`, and optional `status`. Command data contains `connection` diagnostics and `command: {program,args}`. Stdout is machine-readable; diagnostic/progress messages go to stderr. Partial inventory is returned with a nonzero exit status so automation cannot mistake an incomplete refresh for success. `check` reports config/inventory issues and whether `ssh` is installed. Global options work before or after the subcommand.

## Tests and development

```sh
go test ./...
go test -race ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
```

Tests use temporary files, fake subprocess runners and HTTP fixtures. They require no real Forge token, external SSH server, or user private key. Hosted tests create temporary loopback servers and throwaway keys. Packages separate inventory/config, provider/cache/API code, SSH execution, preparation, application orchestration, and the TUI. The UI uses a generic consent/progress interface and has no Forge HTTP dependency.

Milestones 1–3 are implemented. Release setup, mocked/terminal verification, and live read-only Forge inventory checks cover Milestone 4; live Forge key installation and macOS runtime verification remain release checks. See [validation notes](docs/validation.md) for what was actually exercised. This repository has independent history and original code. Wishlist configuration compatibility is an optional import layer; no other manager or background service is required.

MIT licensed. [Wishlist](https://github.com/charmbracelet/wishlist) informed the familiar local directory interaction; its code and server mode are not dependencies.

## From the development name `sshdir`

The executable is now `wishport`. Existing Wishlist and OpenSSH files need no changes, and connection IDs stay the same.

Wishport reads `~/.config/sshdir/config.yaml` when its own default config is absent (respecting `XDG_CONFIG_HOME`). It also falls back to the original Secret Service entry and matching cached inventory, so an existing Forge login continues working. New logins and cache writes use the `wishport` namespace. `forget-token` checks both credential namespaces and removes matching caches from both locations. Existing deterministic Forge key names retain their original prefix to avoid duplicate uploads.

Hosted installations should keep their existing host key and authorization file by passing `--host-key` and `--authorized-keys` explicitly. New installations default to `~/.config/wishport/`. Files are never moved automatically.
