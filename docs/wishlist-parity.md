# Wishlist feature audit

Compared on 8 November 2026 against Wishlist revision
[`ce69eab9d599a727062bad19103c6f4a548c6d57`](https://github.com/charmbracelet/wishlist/tree/ce69eab9d599a727062bad19103c6f4a548c6d57).
The reference is the public CLI and documented behavior, not imported Wishlist
code. The user explicitly expanded wishport's original local-only scope to include
hosted mode and discovery.

| Capability | wishport behavior |
| --- | --- |
| Local searchable directory | List and table views, fuzzy search, keyboard navigation, paging, first/last, help, clipboard address (`y`), connect (`Enter`/`o`) |
| SSH config | Literal aliases, recursive Includes, system config fallback; final configuration and connections use native OpenSSH |
| Wishlist YAML | Endpoints, names/descriptions/links, user/address, forwarding, TTY, remote command, jump hosts, environment, multiple identities, authentication preferences, timeout |
| Configuration discovery | Working-directory `.wishlist`, XDG/native Wishlist YAML/files/directories; explicit `--wishlist-config` |
| Direct selection | `wishport NAME` or `wishport connect --target ID`; ambiguous names require an ID |
| Discovery hints | Ordered Wishlist YAML glob hints; native SSH config hints with destination-preserving overrides described below |
| Tailscale | Device discovery using API keys or OAuth `devices:read`, credentials from environment |
| Zeroconf/mDNS | Bounded `_ssh._tcp` browsing with IPv4/IPv6 support |
| DNS SRV/TXT | `_ssh._tcp` records and Wishlist TXT display names |
| Hosted SSH directory | Opt-in `serve`/`server`/`s`, authorized public keys, persistent host key, remote TUI or direct target |
| Remote terminals | PTY allocation, resize, input/output, child exit status and return to directory |
| Forwarded agents | Explicit client agent forwarding through a private per-session socket |
| Periodic refresh | `--refresh-interval`, preserving search/tab/selection and skipping active preparation |
| Metrics | Optional loopback Prometheus endpoint with aggregate connections/authentication/sessions |
| CLI reference tools | Bash/Zsh/Fish completion and `wishport man` output |
| SSH apps | Existing Wish/other SSH apps can be listed as endpoints and reached using OpenSSH |

wishport additionally retains Forge discovery/provisioning, offline Forge cache,
source tabs, server-only search, JSON automation, and progress indicators.

## Intentional differences and remaining limits

This is **not a drop-in replacement for Wishlist's CLI, server YAML, or Go
embedding API**. The features above have wishport's own configuration and APIs.

- Every outbound interactive connection uses system OpenSSH, including within
  hosted sessions. It retains native agent, proxy, known-host and authentication
  behavior rather than using Wishlist's SSH client implementation.
- Sources coexist instead of the first configuration replacing all other
  sources. IDs remain source-qualified. There is no implicit deduplication
  across sources.
- Server startup requires a separate authorized public-keys file. Authorized
  clients share the serving account; username-specific Wishlist `users` rules
  can be loaded explicitly with `--authorized-users PATH`. Server `listen`, `port`, `users`, and `metrics` YAML fields
  are accepted during endpoint import but do not start or configure listeners.
  Use explicit serve flags. Restricted authorized-key options and SSH
  certificates are rejected rather than ignoring their restrictions.
- The hosted listener defaults to localhost, and metrics are loopback-only.
  No anonymous access, automatically generated destination private key, or
  automatic host trust is added. Generated keys are server host keys only.
- Native OpenSSH discovery hints keep the discovered address and explicit port
  or user pinned. Use YAML hints to override an advertised port/user. Unsupported
  aliases and unsafe option values produce warnings/errors rather than being
  silently interpreted. See [configuration details](wishlist-config.md).
- Existing SSH applications can be directory entries, but wishport does not
  implement Wishlist's in-process Wish middleware/factory library for creating
  additional SSH application servers. It hosts the directory itself.
- Linux is runtime-tested; macOS cross-builds are checked. Windows support and
  Wishlist's package-manager distribution channels are not implemented.
- Clipboard support uses terminal OSC 52 and depends on terminal policy.
  Completion scripts provide commands/options, not live inventory completion.

## Verification

Offline fixtures exercise YAML options/hints, native `ssh -G`, invalid inputs,
Tailscale API/OAuth and redirects, DNS SRV/TXT, mDNS cancellation, source isolation,
refresh fallback, direct CLI targets, keyboard navigation and tabs.

Hosted loopback tests cover public-key authentication, command/forwarding
rejection, real PTY dimensions and exit status, private agent socket forwarding
and cleanup, and host-key permissions. `scripts/hosted-smoke.py` uses the built
binary with system OpenSSH, pinned temporary known-host keys, a remote TUI and
a fake nested SSH executable. It never contacts external SSH hosts or uses user
private keys. `scripts/terminal-smoke.py` continues to check local TTY restoration.

Live tailnet/multicast discovery and public hosted deployment have not been
tested with the user's credentials or enabled on this machine. These features
remain opt-in.

## Reference sources

- [Wishlist README](https://github.com/charmbracelet/wishlist/blob/ce69eab9d599a727062bad19103c6f4a548c6d57/README.md)
- [CLI configuration paths, hints, and commands](https://github.com/charmbracelet/wishlist/blob/ce69eab9d599a727062bad19103c6f4a548c6d57/cmd/wishlist/main.go)
- [Endpoint configuration schema](https://github.com/charmbracelet/wishlist/blob/ce69eab9d599a727062bad19103c6f4a548c6d57/config.go)
- [Picker shortcuts](https://github.com/charmbracelet/wishlist/blob/ce69eab9d599a727062bad19103c6f4a548c6d57/wishlist.go)
