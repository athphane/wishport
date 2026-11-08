package forge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"

	"github.com/athphane/wishport/internal/inventory"
)

func (c *Client) server(org, orgName string, r resource) inventory.Connection {
	v := inventory.Connection{ID: fmt.Sprintf("forge:%s:%s:server:%s", c.partition, org, r.ID), Source: inventory.SourceForge, Kind: inventory.KindForgeServer, Name: r.Attributes.Name, Organization: org, OrganizationName: orgName, ServerID: r.ID, ServerName: r.Attributes.Name, Host: r.Attributes.IPAddress, Port: r.Attributes.SSHPort, User: "forge", SearchTerms: []string{orgName}}
	switch {
	case r.Type != "servers" || !segment(r.ID):
		v.Unavailable = "invalid server identity"
	case r.Attributes.IsReady == nil || !*r.Attributes.IsReady:
		v.Unavailable = "server is not ready"
	case r.Attributes.Revoked == nil || *r.Attributes.Revoked:
		v.Unavailable = "server is revoked or status is unknown"
	case net.ParseIP(v.Host) == nil:
		v.Unavailable = "server address is missing or invalid"
	case v.Port < 1 || v.Port > 65535:
		v.Unavailable = "server SSH port is missing or invalid"
	}
	return v
}
func (c *Client) site(org, orgName string, r resource, servers map[string]resource) inventory.Connection {
	v := inventory.Connection{ID: fmt.Sprintf("forge:%s:%s:site:%s", c.partition, org, r.ID), Source: inventory.SourceForge, Kind: inventory.KindForgeSite, Name: r.Attributes.Name, Organization: org, OrganizationName: orgName, SiteID: r.ID, User: r.Attributes.User, Directory: r.Attributes.RootDirectory, SearchTerms: []string{orgName}}
	if r.Type != "sites" || !segment(r.ID) {
		v.Unavailable = "invalid site identity"
		return v
	}
	rel := r.Relationships.Server.Data
	if rel == nil || rel.Type != "servers" || !segment(rel.ID) {
		v.Unavailable = "site server relationship is missing"
		return v
	}
	v.ServerID = rel.ID
	s, ok := servers[rel.ID]
	if !ok {
		v.Unavailable = "site server is unavailable"
		return v
	}
	sv := c.server(org, orgName, s)
	v.Host = sv.Host
	v.Port = sv.Port
	v.ServerName = sv.Name
	switch {
	case sv.Unavailable != "":
		v.Unavailable = sv.Unavailable
	case v.User == "":
		v.Unavailable = "site user is missing"
	case v.Directory == "" || !path.IsAbs(v.Directory):
		v.Unavailable = "site root directory is missing or invalid"
	}
	return v
}
func (c *Client) Inventory(ctx context.Context) ([]inventory.Connection, error) {
	orgs, _, orgErr := c.pages(ctx, "/orgs", false)
	var result []inventory.Connection
	var errs []error
	if orgErr != nil {
		errs = append(errs, orgErr)
	}
	seenOrgs := map[string]bool{}
	for _, org := range orgs {
		slug := org.Attributes.Slug
		if org.Type != "organizations" || !segment(slug) || seenOrgs[slug] {
			errs = append(errs, errors.New("invalid Forge organization"))
			continue
		}
		seenOrgs[slug] = true
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		servers, _, serverErr := c.pages(ctx, "/orgs/"+slug+"/servers", false)
		if serverErr != nil {
			errs = append(errs, fmt.Errorf("servers for organization %s: %w", slug, serverErr))
		}
		byID := map[string]resource{}
		for _, s := range servers {
			if s.Type != "servers" || !segment(s.ID) {
				errs = append(errs, errors.New("invalid Forge server identity"))
				continue
			}
			if _, exists := byID[s.ID]; exists {
				errs = append(errs, errors.New("duplicate Forge server identity"))
				continue
			}
			if s.Type == "servers" {
				byID[s.ID] = s
			}
			result = append(result, c.server(slug, org.Attributes.Name, s))
		}
		sites, included, siteErr := c.pages(ctx, "/orgs/"+slug+"/sites", true)
		if siteErr != nil {
			errs = append(errs, fmt.Errorf("sites for organization %s: %w", slug, siteErr))
		}
		for _, s := range included {
			if s.Type == "servers" {
				byID[s.ID] = s
			}
		}
		seenSites := map[string]bool{}
		for _, site := range sites {
			if site.Type != "sites" || !segment(site.ID) || seenSites[site.ID] {
				errs = append(errs, errors.New("invalid or duplicate Forge site identity"))
				continue
			}
			seenSites[site.ID] = true
			result = append(result, c.site(slug, org.Attributes.Name, site, byID))
		}
	}
	return result, errors.Join(errs...)
}

// Verify re-reads the exact site and server and rejects any change to the SSH target.
func (c *Client) Verify(ctx context.Context, original inventory.Connection) (inventory.Connection, error) {
	org := original.Organization
	if original.Source != inventory.SourceForge || !segment(org) || !segment(original.ServerID) {
		return original, errors.New("invalid Forge target")
	}
	var current inventory.Connection
	if original.Kind == inventory.KindForgeSite {
		if !segment(original.SiteID) || original.ID != fmt.Sprintf("forge:%s:%s:site:%s", c.partition, org, original.SiteID) {
			return original, errors.New("invalid Forge target")
		}
		var site oneResponse
		if err := c.get(ctx, c.endpoint("/orgs/"+org+"/sites/"+original.SiteID, url.Values{"include": {"server"}}), &site); err != nil {
			return original, err
		}
		if site.Data.ID != original.SiteID || site.Data.Relationships.Server.Data == nil || site.Data.Relationships.Server.Data.Type != "servers" || site.Data.Relationships.Server.Data.ID != original.ServerID {
			return original, errors.New("forge target changed")
		}
		var server oneResponse
		if err := c.get(ctx, c.endpoint("/orgs/"+org+"/servers/"+original.ServerID, nil), &server); err != nil {
			return original, err
		}
		if server.Data.ID != original.ServerID {
			return original, errors.New("forge target changed")
		}
		current = c.site(org, "", site.Data, map[string]resource{server.Data.ID: server.Data})
	} else if original.Kind == inventory.KindForgeServer {
		if original.ID != fmt.Sprintf("forge:%s:%s:server:%s", c.partition, org, original.ServerID) {
			return original, errors.New("invalid Forge target")
		}
		var server oneResponse
		if err := c.get(ctx, c.endpoint("/orgs/"+org+"/servers/"+original.ServerID, nil), &server); err != nil {
			return original, err
		}
		if server.Data.ID != original.ServerID {
			return original, errors.New("forge target changed")
		}
		current = c.server(org, "", server.Data)
	} else {
		return original, errors.New("invalid Forge target")
	}
	if current.Unavailable != "" {
		return original, fmt.Errorf("forge target unavailable: %s", current.Unavailable)
	}
	if current.Host != original.Host || current.Port != original.Port || current.User != original.User || current.Directory != original.Directory || current.ServerID != original.ServerID || current.SiteID != original.SiteID {
		return original, errors.New("forge target changed")
	}
	return current, nil
}
