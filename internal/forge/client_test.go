package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const serverJSON = `{"id":"1","type":"servers","attributes":{"name":"box","ip_address":"192.0.2.1","ssh_port":22,"is_ready":true,"revoked":false}}`
const siteJSON = `{"id":"2","type":"sites","attributes":{"name":"site","user":"deploy","root_directory":"/home/deploy/app"},"relationships":{"server":{"data":{"type":"servers","id":"1"}}}}`

func TestInventoryAndVerify(t *testing.T) {
	var base string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authorization")
		}
		switch r.URL.Path {
		case "/api/orgs":
			fmt.Fprint(w, `{"data":[{"id":"9","type":"organizations","attributes":{"slug":"team","name":"Team"}}],"meta":{"next_cursor":null}}`)
		case "/api/orgs/team/servers":
			fmt.Fprintf(w, `{"data":[%s],"meta":{"next_cursor":null}}`, serverJSON)
		case "/api/orgs/team/sites":
			if r.URL.Query().Get("include") != "server" {
				t.Error("include absent")
			}
			if r.URL.Query().Get("page[cursor]") == "" {
				fmt.Fprintf(w, `{"data":[%s],"links":{"next":%q},"meta":{"next_cursor":"next"}}`, siteJSON, base+"/api/orgs/team/sites?page%5Bcursor%5D=next")
			} else {
				fmt.Fprint(w, `{"data":[],"meta":{"next_cursor":null}}`)
			}
		case "/api/orgs/team/servers/1":
			fmt.Fprintf(w, `{"data":%s}`, serverJSON)
		case "/api/orgs/team/sites/2":
			fmt.Fprintf(w, `{"data":%s}`, siteJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	base = ts.URL
	c, err := NewClient(base+"/api", "secret", ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := c.Inventory(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].User != "forge" || rows[1].User != "deploy" || rows[1].Directory != "/home/deploy/app" || rows[1].Unavailable != "" {
		t.Fatalf("%+v", rows)
	}
	if _, err = c.Verify(context.Background(), rows[1]); err != nil {
		t.Fatal(err)
	}
	rows[1].Host = "192.0.2.2"
	if _, err = c.Verify(context.Background(), rows[1]); err == nil {
		t.Fatal("changed target accepted")
	}
}
func TestClientFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"unauthorized", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret", http.StatusUnauthorized) }},
		{"forbidden", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret", http.StatusForbidden) }},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.com", http.StatusFound)
		}},
		{"bad-link", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":[],"links":{"next":"https://example.com/api/orgs?page%5Bcursor%5D=x"},"meta":{"next_cursor":"x"}}`)
		}},
		{"missing-data", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(tc.handler)
			defer s.Close()
			c, _ := NewClient(s.URL, "secret", s.Client())
			_, err := c.Inventory(context.Background())
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), s.URL) {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
}
func TestRetryAndCancellation(t *testing.T) {
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"data":[],"meta":{"next_cursor":null}}`)
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, "secret", s.Client())
	if _, err := c.Inventory(context.Background()); err != nil || count != 2 {
		t.Fatalf("count %d: %v", count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Inventory(ctx); err != context.Canceled && (err == nil || !strings.Contains(err.Error(), context.Canceled.Error())) {
		t.Fatalf("%v", err)
	}
	if retryDelay("999") <= 5*time.Second {
		t.Fatal("retry would happen early")
	}
}

func TestPartialOrganizationsAndIncompleteSite(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs":
			fmt.Fprint(w, `{"data":[{"type":"organizations","attributes":{"slug":"one"}},{"type":"organizations","attributes":{"slug":"two"}}],"meta":{"next_cursor":null}}`)
		case "/orgs/one/servers":
			fmt.Fprintf(w, `{"data":[%s],"meta":{"next_cursor":null}}`, serverJSON)
		case "/orgs/one/sites":
			fmt.Fprint(w, `{"data":[{"id":"3","type":"sites","attributes":{"name":"missing"}}],"meta":{"next_cursor":null}}`)
		case "/orgs/two/servers":
			http.Error(w, "denied", http.StatusForbidden)
		case "/orgs/two/sites":
			fmt.Fprint(w, `{"data":[],"meta":{"next_cursor":null}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, "secret", s.Client())
	rows, err := c.Inventory(context.Background())
	if err == nil || len(rows) != 2 || rows[1].Unavailable == "" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestRejectIncompleteAndTrailingData(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"data":[],"meta":{"next_cursor":null}} garbage`, `{"data":[],"meta":{"next_cursor":"x"},"links":{"next":"?page%5Bcursor%5D=y"}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c, _ := NewClient(s.URL, "secret", s.Client())
		_, err := c.Inventory(context.Background())
		s.Close()
		if err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestRefuseLongRetry(t *testing.T) {
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, "secret", s.Client())
	_, err := c.Inventory(context.Background())
	if err == nil || count != 1 {
		t.Fatalf("count %d error %v", count, err)
	}
}
func TestUnsafeTarget(t *testing.T) {
	c, _ := NewClient("https://example.com", "secret", nil)
	s := resource{ID: "1", Type: "servers"}
	ready := true
	revoked := false
	s.Attributes.IsReady = &ready
	s.Attributes.Revoked = &revoked
	s.Attributes.SSHPort = 22
	s.Attributes.IPAddress = "evil.example"
	if c.server("org", "", s).Unavailable == "" {
		t.Fatal("accepted non-IP")
	}
	s.Attributes.IPAddress = "192.0.2.1"
	v := resource{ID: "2", Type: "sites"}
	v.Attributes.User = "u"
	v.Attributes.RootDirectory = "relative"
	v.Relationships.Server.Data = &struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}{"1", "servers"}
	if c.site("org", "", v, map[string]resource{"1": s}).Unavailable == "" {
		t.Fatal("accepted relative root")
	}
}
