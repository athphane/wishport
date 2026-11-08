package inventory

import (
	"sort"
	"strings"
)

// Sort never coalesces entries: provenance and user are part of their identity.
func Sort(connections []Connection) {
	sort.SliceStable(connections, func(i, j int) bool {
		a, b := strings.ToLower(connections[i].Name), strings.ToLower(connections[j].Name)
		if a != b {
			return a < b
		}
		return connections[i].ID < connections[j].ID
	})
}

func SearchText(c Connection) string {
	return strings.Join(append([]string{c.Name, c.Description, c.Host, c.SSHAlias, c.User, c.Organization, c.OrganizationName, c.ServerName, string(c.Source), string(c.Kind)}, c.SearchTerms...), " ")
}
