# Network discovery

Enable one source for a session with `wishport --mdns`, `wishport --srv-domain example.com` (repeatable), or `wishport --tailscale-net example.com`. These flags can also accompany `list`, `check`, and `serve`. Persist settings in wishport YAML:

```yaml
discovery:
  mdns:
    enabled: true
    domain: local
    timeout: 2s
  srv:
    domains: [example.com]
    timeout: 5s
  tailscale:
    tailnet: example.com
    timeout: 15s
```

`--mdns-domain` and `--mdns-timeout` override those settings. Ctrl+R/R refreshes configured discovery sources as well as Forge. `--refresh-interval 5m` enables recurring refresh. Local sources appear before discovery starts. Successful results are kept in memory; a failed refresh shows a warning and preserves the last results. Discovery adds a Discovered tab without moving entries into Local or Forge.

Network sources are opt-in. Each inventory refresh queries configured sources again; discovered entries never change SSH configuration, host trust, DNS records, or tailnet membership. Source-qualified identities keep independently discovered entries separate from YAML, SSH config, and Forge entries. Discovery does not prove a device runs SSH or that a login will succeed.

Tailscale uses the [public devices API](https://tailscale.com/api#tag/devices/GET/tailnet/{tailnet}/devices), including devices not present in the local daemon's peer list. Set `TAILSCALE_KEY` to an API access key, or set both `TAILSCALE_CLIENT_ID` and `TAILSCALE_CLIENT_SECRET` for an [OAuth client](https://tailscale.com/docs/features/oauth-clients) permitted to read devices (`devices:read`). Credentials are fetched at request time; they are never included in inventory. API keys take precedence when both methods are supplied. The default tailnet `-` selects the authenticated tailnet. Discovery uses port 22, preferring an advertised IP and otherwise using the device's DNS name. OAuth exchanges and device requests share one bounded context. Redirects are rejected; errors expose HTTP status codes without response bodies or secrets. Response bodies are limited to 4 MiB.

DNS discovery queries `_ssh._tcp.<domain>` via the system resolver. Targets and advertised ports become inventory entries. For Wishlist compatibility, TXT records at the domain can supply names:

```dns
_ssh._tcp.example.com. SRV 0 0 2222 host.example.com.
example.com. TXT "wishlist.name host.example.com:2222=My server"
```

`wishlist.name.<domain>` is also checked for records using the same content syntax. TXT is optional: absent or failing TXT lookups leave the hostname as the display name. This follows the [Wishlist SRV format at the reference revision](https://github.com/charmbracelet/wishlist/blob/ce69eab9d599a727062bad19103c6f4a548c6d57/srv/srv.go), with the deliberate improvement that missing TXT does not discard valid SRV entries. DNS SRV priority and weight do not merge or suppress endpoints.

mDNS browses `_ssh._tcp` in `local.` by default using [libp2p/zeroconf v2](https://github.com/libp2p/zeroconf). Advertised hostnames preserve one endpoint across duplicate IPv4/IPv6 announcements; when no valid hostname exists, a usable advertised IP is used. Link-local IPv6 without an interface scope is not used as a fallback. The provider has a finite discovery window, a 64-entry receive buffer, and a 10,000-entry maximum. It only browses; it never registers a service. Firewalls, multicast filtering, and interface configuration can prevent discovery.

All providers support cancellation and a configurable timeout (default five seconds, maximum one minute). Invalid advertised hosts and ports are ignored. Display names have terminal controls removed. Usernames, private keys, working directories, and other connection hints belong to explicit user configuration, not unauthenticated network metadata.

The internal package exposes provider-compatible `Tailscale`, `SRV`, and `MDNS` structs. HTTP clients, credential getters, DNS resolvers, and mDNS browsers are injectable. Unit tests use those boundaries and never query the user's LAN, DNS, or Tailscale account.
