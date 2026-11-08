# Forge API contract

Verified 8 November 2026 against the published [OpenAPI document](https://forge.laravel.com/api/docs.openapi), [introduction](https://laravel.com/forge/docs/api-reference/introduction), and [pagination documentation](https://laravel.com/forge/docs/api-reference/pagination). The documentation index's `https://laravel.com/api/docs.openapi` link returned 404; the Forge-hosted document is available.

Base URL: `https://forge.laravel.com/api`. Authentication uses a bearer token. This application does not use the deprecated `/api/v1` API.

| Operation | Path | Required scope |
| --- | --- | --- |
| Organizations | GET `/orgs` | `organization:view` |
| Servers | GET `/orgs/{organization}/servers` | `server:view` |
| Sites | GET `/orgs/{organization}/sites?include=server` | `server:view` |
| Server detail | GET `/orgs/{organization}/servers/{server}` | `server:view` |
| Site detail | GET `/orgs/{organization}/sites/{site}` | `server:view` |
| List keys | GET `/orgs/{organization}/servers/{server}/ssh-keys` | `server:view` |
| Install key | POST `/orgs/{organization}/servers/{server}/ssh-keys` | `server:create-keys` |
| Key status | GET `/orgs/{organization}/servers/{server}/ssh-keys/{key}` | `server:view` |

The scopes above come from each operation's `security` and `x-permissions` definitions, not the deprecated API's permission model. The token must also have access to the relevant organizations and resources.

Resources use JSON:API envelopes: `data` contains resources with string `id`, `type`, `attributes`, and optional `relationships`. Server connection fields are `ip_address`, `ssh_port`, `is_ready`, and `revoked`. The server schema has no login-user override, so server connections use `forge`. Site attributes supply `user` and nullable `root_directory`; `relationships.server.data` supplies the server ID. A missing relationship or directory is not guessed.

Pagination uses `page[cursor]`, `meta.next_cursor`, and `links.next`. `include=server` must survive every site page. Successful list responses require complete pagination metadata; a partial or failed refresh cannot overwrite a good cache snapshot.

Live inventory verification also confirmed that Forge emits `links: []` when no links are present. The decoder accepts this empty collection, null, and link objects; `links.next` can be a URL string or a JSON:API `{href: ...}` object. Cursor pagination still falls back to `meta.next_cursor`, and any supplied URL must pass origin/path validation. This behavior was cross-checked against [Anvil's inventory reader](https://github.com/athphane/anvil/blob/master/src/backend.cpp#L308); wishport implements its own decoder with regression fixtures for these shapes.

Key creation takes JSON `{name, key, user}`. The published success response is **202 Accepted with no specified body**, so installation must discover the new resource by deterministic name and user, then poll its status. Key metadata includes name, user and status, but not public-key bytes. Never depend on an immediate created-key body or a `username` request field.

Fixtures test these contracts without an account or live write operations. Read-only inventory and a server-detail lookup were also verified against the live service using the user's existing saved credential. Live SSH-key installation still needs a consenting user's smoke test before a production release.
