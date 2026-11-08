package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKeyReadAndCreate(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing authorization")
		}
		if r.Method == http.MethodPost {
			posts++
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["name"] != "wishport-name" || body["user"] != "forge" || body["key"] != "ssh-ed25519 PUBLIC" {
				t.Errorf("unexpected body: %v", body)
			}
			w.WriteHeader(202)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/7") {
			fmt.Fprint(w, `{"data":{"type":"keys","id":"7","attributes":{"name":"wishport-name","user":"forge","status":"installed"}}}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"type":"keys","id":"7","attributes":{"name":"wishport-name","user":"forge","status":"installed"}}],"meta":{"next_cursor":null}}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "token", server.Client())
	keys, e := c.ListKeys(context.Background(), "org", "1")
	if e != nil || len(keys) != 1 || keys[0].Status != "installed" {
		t.Fatalf("%v %v", keys, e)
	}
	key, e := c.GetKey(context.Background(), "org", "1", "7")
	if e != nil || key.ID != "7" {
		t.Fatalf("%v %v", key, e)
	}
	if e = c.CreateKey(context.Background(), "org", "1", "wishport-name", "forge", "ssh-ed25519 PUBLIC"); e != nil || posts != 1 {
		t.Fatal(e)
	}
}
func TestCreateKeyNeverRetriesOrLeaksResponse(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
		fmt.Fprint(w, "SECRET public key token response")
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "token", server.Client())
	e := c.CreateKey(context.Background(), "org", "1", "name", "forge", "ssh-ed25519 PUBLIC")
	if e == nil || calls != 1 || strings.Contains(e.Error(), "SECRET") || !strings.Contains(e.Error(), "429") {
		t.Fatalf("calls=%d error=%v", calls, e)
	}
}
