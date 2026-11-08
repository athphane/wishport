package config

import (
	"strings"
	"testing"
	"time"
)

func TestDiscoveryConfiguration(t *testing.T) {
	c, err := loadText(t, "sources:\n  wishlist_config: /tmp/wishlist.yaml\ndiscovery:\n  mdns:\n    enabled: true\n    domain: local\n    timeout: 3s\n  srv:\n    domains: [example.com]\n  tailscale:\n    tailnet: example.com\n")
	if err != nil || !c.Discovery.MDNS.Enabled || c.Discovery.MDNS.Timeout != 3*time.Second || c.Discovery.Tailscale.Tailnet != "example.com" || len(c.Discovery.SRV.Domains) != 1 || c.Sources.WishlistConfig != "/tmp/wishlist.yaml" {
		t.Fatal(c, err)
	}
	for _, s := range []string{"mdns: {enabled: yes}", "mdns: {timeout: 2m}", "srv: {domains: [bad/host]}", "tailscale: {token: sensitive-token}", "mdns: {domain: -bad}"} {
		_, err := loadText(t, "discovery:\n  "+s+"\n")
		if err == nil || strings.Contains(err.Error(), "sensitive-token") {
			t.Fatal("unsafe discovery config accepted or leaked", err)
		}
	}
}
