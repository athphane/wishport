package discovery

import (
	"context"
	"fmt"
	"github.com/athphane/wishport/internal/inventory"
	"net"
	"strings"
	"time"
)

type DNSResolver interface {
	LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error)
	LookupTXT(context.Context, string) ([]string, error)
}
type SRV struct {
	Domains  []string
	Timeout  time.Duration
	Resolver DNSResolver
}

func (*SRV) Name() string { return "srv" }
func (p *SRV) List(ctx context.Context, _ bool) ([]inventory.Connection, error) {
	ctx, cancel := bounded(ctx, p.Timeout)
	defer cancel()
	r := p.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
	var out []inventory.Connection
	seen := map[string]bool{}
	for _, domain := range p.Domains {
		domain = validHost(domain)
		if domain == "" {
			return out, fmt.Errorf("srv: invalid domain")
		}
		_, records, err := r.LookupSRV(ctx, "ssh", "tcp", domain)
		if err != nil {
			return out, fmt.Errorf("srv: lookup failed for %s", domain)
		}
		txts, _ := r.LookupTXT(ctx, domain)
		extra, _ := r.LookupTXT(ctx, "wishlist.name."+domain)
		txts = append(txts, extra...)
		for _, record := range records {
			if record == nil {
				continue
			}
			host := validHost(record.Target)
			address := net.JoinHostPort(host, fmt.Sprint(record.Port))
			name := host
			for _, txt := range txts {
				if !strings.HasPrefix(txt, "wishlist.name ") {
					continue
				}
				addr, label, ok := strings.Cut(strings.TrimPrefix(txt, "wishlist.name "), "=")
				if ok && addr == address {
					name = label
				}
			}
			c, ok := connection("srv", domain+"/"+address, name, host, int(record.Port))
			if ok && !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c)
				if len(out) >= maxEntries {
					inventory.Sort(out)
					return out, nil
				}
			}
		}
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	inventory.Sort(out)
	return out, nil
}
