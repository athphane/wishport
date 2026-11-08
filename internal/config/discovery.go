package config

import (
	"fmt"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

func loadDiscovery(n *yaml.Node, d *Discovery) error {
	fields, err := mapping(n, map[string]bool{"mdns": true, "srv": true, "tailscale": true})
	if err != nil {
		return err
	}
	for name, node := range fields {
		allowed := map[string]bool{"timeout": true}
		switch name {
		case "mdns":
			allowed["enabled"], allowed["domain"] = true, true
		case "srv":
			allowed["domains"] = true
		case "tailscale":
			allowed["tailnet"] = true
		}
		m, err := mapping(node, allowed)
		if err != nil {
			return err
		}
		var timeout *time.Duration
		switch name {
		case "mdns":
			timeout = &d.MDNS.Timeout
			if v := m["enabled"]; v != nil {
				if v.Tag != "!!bool" || v.Decode(&d.MDNS.Enabled) != nil {
					return fmt.Errorf("discovery.mdns.enabled must be a boolean")
				}
			}
			if v := m["domain"]; v != nil {
				if stringValue(v, &d.MDNS.Domain) != nil || !domain(d.MDNS.Domain) {
					return fmt.Errorf("discovery.mdns.domain must be a DNS domain")
				}
			}
		case "srv":
			timeout = &d.SRV.Timeout
			if v := m["domains"]; v != nil {
				if v.Kind != yaml.SequenceNode || len(v.Content) > 32 {
					return fmt.Errorf("discovery.srv.domains must be a list of at most 32 domains")
				}
				for _, item := range v.Content {
					var value string
					if stringValue(item, &value) != nil || !domain(value) {
						return fmt.Errorf("discovery.srv.domains must contain DNS domains")
					}
					d.SRV.Domains = append(d.SRV.Domains, value)
				}
			}
		case "tailscale":
			timeout = &d.Tailscale.Timeout
			if v := m["tailnet"]; v != nil {
				if stringValue(v, &d.Tailscale.Tailnet) != nil || badText(d.Tailscale.Tailnet) || strings.TrimSpace(d.Tailscale.Tailnet) == "" {
					return fmt.Errorf("discovery.tailscale.tailnet must be a nonempty name")
				}
			}
		}
		if v := m["timeout"]; v != nil {
			value, err := time.ParseDuration(v.Value)
			if v.Tag != "!!str" || err != nil || value <= 0 || value > time.Minute {
				return fmt.Errorf("discovery.%s.timeout must be a duration between 0 and 1m", name)
			}
			*timeout = value
		}
	}
	return nil
}

func domain(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if len(part) == 0 || len(part) > 63 || strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
