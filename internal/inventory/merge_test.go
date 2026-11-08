package inventory

import "testing"

func TestSortPreservesDistinctConnections(t *testing.T) {
	rows := []Connection{{ID: "yaml:two", Name: "same", Host: "192.0.2.1", User: "two"}, {ID: "ssh:same", Name: "same", Host: "192.0.2.1", User: "one"}, {ID: "forge:a:org:site:1", Name: "Same", Host: "192.0.2.1", User: "three"}}
	Sort(rows)
	if len(rows) != 3 || rows[0].ID != "forge:a:org:site:1" || rows[2].ID != "yaml:two" {
		t.Fatal(rows)
	}
}
