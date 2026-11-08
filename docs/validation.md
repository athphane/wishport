# Validation and release status

Development validation on Linux, 8 November 2026:

- `go test ./...`: all packages pass.
- `go test -race ./...`: all packages pass with the race detector.
- `go vet ./...`: passes.
- Staticcheck `v0.8.2`: passes.
- `go build -trimpath -o wishport ./cmd/wishport`: builds the native executable.
- `python3 scripts/terminal-smoke.py ./wishport`: passes using a real pseudoterminal and fake SSH executable. Exercises filtering, resize, child TTY ownership, child exit-code display, return to the directory, leaving the alternate screen, and restoration of terminal attributes.
- `python3 scripts/hosted-smoke.py ./wishport`: passes with the built binary, loopback OpenSSH, pinned temporary host keys, remote TUI/table, nested fake SSH with a real TTY, and clean exit.

The user subsequently expanded the Wishlist parity scope to hosted mode and network discovery. Tests now cover Wishlist YAML and hints, Tailscale API/OAuth, SRV/TXT, mDNS browsing/cancellation, background discovery isolation, source tabs, recurring refresh, direct targets, and authenticated hosted PTY/agent sessions. See [the feature audit](wishlist-parity.md) for explicit differences; no live LAN/tailnet discovery or public server deployment was performed.

The test suite includes local config/Include parsing, aliases and argument preservation, unsafe values, provider errors, deterministic inventory, Forge JSON:API pagination and relationship mapping, denied requests, rate-limit/cancellation behavior, redirects, incomplete snapshots, cache isolation/permissions/expiry, secret storage boundaries, remote quoting, public-key usability, probe classifications, consent and cancellation, duplicate prevention, polling, and UI message lifecycles.

An end-to-end HTTP fixture exercises the real Forge client and preparation service together: discover the site and server, verify the live target, request consent, accept an empty 202 response, wait for key metadata to appear, poll installation, retry authentication, and run again without a second upload. Changed live targets and unknown host keys never reach POST.

The independent security review identified two probe issues that were fixed before handoff: configured alternate identities could falsely satisfy a selected-key check, and remote command failure text could resemble authentication rejection. Probes now require OpenSSH authentication evidence, selected-key fingerprints, bounded diagnostic output, and explicit session settings. Regression tests cover those cases.

## Remaining release checks

Following a reported authentication failure, read-only live API checks with the user's saved credential confirmed HTTP 200 and exposed an undocumented empty `links` array. The parser was corrected and compared with Anvil's handling; a full refresh then loaded server and site inventory without warnings. A server-detail lookup also returned HTTP 200. No credential was printed or copied into source/configuration, and no Forge write was performed. End-to-end SSH connection/provisioning remains unverified against real servers.

Linux native operation is tested. Linux arm64 and macOS arm64 cross-builds both passed, but those platforms have not been runtime-tested on this host. CI includes Linux and macOS jobs; those remote jobs have not yet run. Native macOS Keychain storage is not implemented; session/environment credentials work there.

The public repository is https://github.com/athphane/wishport. No release tag is included in the initial publication. Release automation creates Linux archives and a draft release when the maintainer deliberately pushes a version tag. Existing user SSH files and keys were not changed.

## Scope choices

- Ambiguous key selection is resolved through `forge.public_key` or `--public-key`; there is no interactive filesystem browser.
- SSH-config search metadata is advisory. Full `Match`, identity, proxy and option resolution remains OpenSSH's job at selection time.
- A new Forge host is verified in an ordinary interactive SSH attempt. If authentication fails afterward, select it again to run safe key preparation.
- Unsupported or ambiguous SSH diagnostics stop preparation. The app does not infer missing authorization from arbitrary error text.
- CLI output schema is version 1. The executable is `wishport`; the Go module is `github.com/athphane/wishport`.
