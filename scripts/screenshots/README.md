# Documentation screenshots

These captures use Wishport's real Bubble Tea model and ANSI renderer with fictional fixture data. The fixture service cannot execute SSH and never constructs the application service or accesses a keyring, API, user config, or user cache.

- `fixtures/config.yaml`: hand-written local hosts, with automatic sources and Forge disabled.
- `fixtures/ssh_config` and `fixtures/wishlist`: explicit sample files loaded through the real local providers.
- `fixtures/forge.json`: synthetic Forge inventory; no token or network request is involved.

Domains use `example.com` / `example.org`; IPs are in the documentation-only `192.0.2.0/24`, `198.51.100.0/24`, and `203.0.113.0/24` ranges. The footer labels every image as demo inventory.

From the repository root, with Go, [Charm Freeze](https://github.com/charmbracelet/freeze), and the Source Code Pro font installed:

```sh
scripts/screenshots/capture.sh
```

This writes three PNGs to `assets/screenshots/`: the combined directory, the Forge table, and `server:` search. The helper embeds all fixtures, reads them through temporary explicit paths, drives the actual UI key messages, and emits ANSI for Freeze. It does not change production behavior or add a demo mode to the released executable.

To inspect a single view without creating an image:

```sh
go run ./scripts/screenshots --view forge
```
