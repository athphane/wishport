// Package discovery contains opt-in, read-only network inventory providers.
package discovery

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/athphane/wishport/internal/inventory"
	"net"
	"strings"
	"time"
)

const maxEntries = 10000

func bounded(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 || timeout > time.Minute {
		timeout = 5 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}
func validHost(host string) string {
	host = strings.TrimSuffix(host, ".")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	if len(host) == 0 || len(host) > 253 {
		return ""
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return ""
			}
		}
	}
	return strings.ToLower(host)
}
func connection(source, key, name, host string, port int) (inventory.Connection, bool) {
	host = validHost(host)
	if host == "" || port < 1 || port > 65535 {
		return inventory.Connection{}, false
	}
	name = inventory.DisplayText(name)
	if name == "" {
		name = host
	}
	if len(name) > 512 {
		name = host
	}
	sum := sha256.Sum256([]byte(key))
	return inventory.Connection{ID: fmt.Sprintf("%s:%x", source, sum[:16]), Source: inventory.Source(source), Kind: inventory.KindHost, Name: name, Host: host, Port: port}, true
}
