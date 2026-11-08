# Hosted SSH directory

```sh
wishport serve --authorized-keys /path/to/authorized_keys
# Optional: --listen 127.0.0.1:2222 --host-key /path/to/server_ed25519
# Optional: --refresh-interval 5m --metrics-listen 127.0.0.1:9090
```

The CLI creates the host-key parent directory with owner-only permissions when missing. Default server files are under `$XDG_CONFIG_HOME/wishport` or `~/.config/wishport`: `server_ed25519` and `authorized_keys`. The public authorized-keys file must already exist. `serve`, `server`, and `s` select hosted mode; no ordinary inventory command starts a listener.

Hosted mode is opt-in. Start it under the account whose directory configuration and OpenSSH access should be used. By default it listens on `127.0.0.1:2222`; binding a non-loopback address makes it reachable from the network and should be done deliberately. Supply an authorized-keys file containing one or more public keys and a path for the server's private host key. The standalone server package requires an existing host-key parent directory; the CLI creates it when missing. On startup, a missing host key is generated once as an owner-only file; an existing key must be a regular file with no group or other permissions. Preserve this key across restarts and verify its fingerprint with clients through a trusted channel.

Only listed public keys can authenticate. Password and anonymous authentication are disabled. The authorized-keys file is read at startup; restart the server after changing it. OpenSSH authorized-key options are rejected because their restrictions are not implemented. Use a dedicated account and a carefully restricted key list: every authorized client operates the server account's directory, configuration, credentials, and any destination SSH identities that account can access. This is a shared-account trust boundary: authentication can restrict usernames, but it does not give each user a different inventory or operating-system identity.

A client can connect with a terminal using `ssh -t -p 2222 USER@HOST`. A normal shell request or the command `menu` opens the directory. `ssh -t -p 2222 USER@HOST connect --target ID` opens a selected stable inventory ID or unambiguous name; a single safe target token also works. Arbitrary commands, non-PTY sessions, TCP forwarding, and other SSH channels are rejected. The directory launches the local executable directly without a shell; terminal input, output, resizing, and exit status pass through the SSH session.

SSH agent forwarding is available only when the client explicitly requests it (for example, `ssh -A -t ...`). The forwarded agent socket is private to that session and removed afterward. A forwarded agent can sign authentication requests while the session is active; only forward it to an account and host you trust. The server does not pass its own inherited `SSH_AUTH_SOCK` to remote sessions.

An optional Prometheus-compatible `/metrics` endpoint can be enabled on a loopback address. Its aggregate connection, public-key authentication, and session counters do not include usernames, keys, or tokens. It is disabled unless explicitly configured. Protect access to the machine's loopback interface as appropriate.

## Named users

Instead of `--authorized-keys`, pass `--authorized-users /path/to/wishlist.yaml` to enforce the Wishlist-style user/key mapping:

```yaml
users:
  - name: alice
    public-keys:
      - ssh-ed25519 AAAA... alice
```

Each username requires one or more raw public keys; duplicate usernames, certificates, and key options are rejected. This is explicit opt-in: importing endpoints with `--wishlist-config` never configures server authentication. If both authorization flags are provided, the plain authorized-keys file grants its keys access regardless of username, while the users file grants named access.
