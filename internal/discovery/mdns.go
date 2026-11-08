package discovery

import (
	"context"
	"fmt"
	"github.com/athphane/wishport/internal/inventory"
	z "github.com/libp2p/zeroconf/v2"
	"net"
	"time"
)

// Browser runs until cancellation, closing entries when finished.
type Browser interface {
	Browse(context.Context, string, string, chan<- *z.ServiceEntry) error
}
type defaultBrowser struct{}

func (defaultBrowser) Browse(ctx context.Context, s, d string, ch chan<- *z.ServiceEntry) error {
	return z.Browse(ctx, s, d, ch)
}

type MDNS struct {
	Domain  string
	Timeout time.Duration
	Browser Browser
}

func (*MDNS) Name() string { return "mdns" }
func (p *MDNS) List(parent context.Context, _ bool) ([]inventory.Connection, error) {
	ctx, cancel := bounded(parent, p.Timeout)
	defer cancel()
	domain := p.Domain
	if domain == "" {
		domain = "local"
	}
	domain = validHost(domain)
	if domain == "" {
		return nil, fmt.Errorf("mdns: invalid domain")
	}
	browser := p.Browser
	if browser == nil {
		browser = defaultBrowser{}
	}
	entries := make(chan *z.ServiceEntry, 64)
	browseErrors := make(chan error, 1)
	go func() { browseErrors <- browser.Browse(ctx, "_ssh._tcp", domain+".", entries) }()
	out := []inventory.Connection{}
	seen := map[string]bool{}
	for {
		select {
		case err := <-browseErrors:
			// Some browser implementations return after launching a background browse.
			// A nil result leaves channel completion/cancellation in control.
			if err != nil && ctx.Err() == nil {
				return out, fmt.Errorf("mdns: browse failed")
			}
			browseErrors = nil
		case <-ctx.Done():
			inventory.Sort(out)
			if parent.Err() != nil {
				return out, parent.Err()
			}
			return out, nil
		case e, ok := <-entries:
			if !ok {
				if parent.Err() != nil {
					return out, parent.Err()
				}
				inventory.Sort(out)
				return out, nil
			}
			if e == nil {
				continue
			}
			// Prefer the advertised hostname so dual stack advertisements remain one endpoint.
			host := validHost(e.HostName)
			if host == "" {
				for _, ip := range append(append([]net.IP{}, e.AddrIPv4...), e.AddrIPv6...) {
					if ip != nil && !ip.IsLinkLocalUnicast() {
						host = ip.String()
						break
					}
				}
			}
			c, valid := connection("mdns", domain+"/"+host+fmt.Sprint(":", e.Port), e.HostName, host, e.Port)
			if valid && !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c)
				if len(out) >= maxEntries {
					inventory.Sort(out)
					return out, nil
				}
			}
		}
	}
}
