package cli

import (
	"fmt"
	"io"
)

const completionWords = "list check command connect prepare login forget-token serve server completion man --config --wishlist-config --target --json --refresh --refresh-interval --public-key --yes --session --version --mdns --mdns-domain --mdns-timeout --srv-domain --tailscale-net --listen --host-key --authorized-keys --authorized-users --metrics-listen --help"

func referenceCommand(args []string, out, errout io.Writer) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	if args[0] == "man" {
		if len(args) != 1 {
			fmt.Fprintln(errout, "Usage: wishport man")
			return true, 2
		}
		fmt.Fprint(out, manual)
		return true, 0
	}
	if args[0] != "completion" {
		return false, 0
	}
	if len(args) != 2 {
		fmt.Fprintln(errout, "Usage: wishport completion bash|zsh|fish")
		return true, 2
	}
	switch args[1] {
	case "bash":
		fmt.Fprintf(out, "_wishport_complete() { COMPREPLY=( $(compgen -W '%s' -- \"${COMP_WORDS[COMP_CWORD]}\") ); }\ncomplete -F _wishport_complete wishport\n", completionWords)
	case "zsh":
		fmt.Fprintf(out, "#compdef wishport\n_wishport() { local -a choices; choices=(%s); _describe 'wishport' choices; }\ncompdef _wishport wishport\n", completionWords)
	case "fish":
		fmt.Fprintf(out, "complete -c wishport -a '%s'\n", completionWords)
	default:
		fmt.Fprintln(errout, "supported completion shells: bash, zsh, fish")
		return true, 2
	}
	return true, 0
}

const manual = `.TH WISHPORT 1
.SH NAME
wishport \- SSH directory with local, Forge and discovery inventories
.SH SYNOPSIS
.B wishport
[options] [name-or-id]
.br
.B wishport list|check
[--json] [--refresh]
.br
.B wishport connect|command|prepare
--target ID
.br
.B wishport serve
--authorized-keys PATH [--listen ADDRESS]
.SH DESCRIPTION
Browse and fuzzy-search a unified SSH inventory. Interactive connections use
the system OpenSSH executable. Read ~/.ssh/config, Wishlist configurations,
wishport YAML and optional Forge, Tailscale, DNS SRV and mDNS inventories.
.SH OPTIONS
.TP
.B --config PATH
Read wishport YAML from PATH.
.TP
.B --wishlist-config PATH
Read a Wishlist YAML or OpenSSH configuration.
.TP
.B --mdns, --srv-domain DOMAIN, --tailscale-net TAILNET
Enable opt-in discovery. SRV domains may be repeated.
.TP
.B --refresh-interval DURATION
Refresh inventory periodically; zero disables it.
.TP
.B --json
Versioned JSON for list, check, command and prepare.
.TP
.B --yes
Approve safe public-key installation with prepare; never bypasses validation.
.TP
.B --listen ADDRESS, --host-key PATH, --authorized-keys PATH
Hosted directory settings. Default listener: 127.0.0.1:2222.
.TP
.B --metrics-listen ADDRESS
Enable a loopback Prometheus endpoint for hosted mode.
.SH KEYS
Tab/Shift+Tab select inventory tabs; slash starts search; Enter or o connects;
y copies the address; t toggles table/list; Ctrl+R refreshes; q quits.
Prefix searches with server: for server/host names only.
.SH ENVIRONMENT
FORGE_API_TOKEN overrides the desktop keyring. TAILSCALE_KEY or
TAILSCALE_CLIENT_ID and TAILSCALE_CLIENT_SECRET authenticate Tailscale discovery.
Credentials are never accepted as command-line flags.
.SH SECURITY
Host keys are verified by OpenSSH. Key installation needs explicit consent.
Hosted mode requires authorized public keys; authorized clients share the
serving account's inventory and destination access.
.SH SEE ALSO
ssh(1), ssh_config(5)
`
