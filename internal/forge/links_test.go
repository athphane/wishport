package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPageLinksRepresentations(t *testing.T) {
	for _, tc := range []struct {
		body, next string
		valid      bool
	}{
		{`[]`, "", true}, {`[ ]`, "", true}, {`null`, "", true}, {`{}`, "", true}, {`{"next":null}`, "", true},
		{`{"next":"?page%5Bcursor%5D=a"}`, "?page%5Bcursor%5D=a", true},
		{`{"next":{"href":"?page%5Bcursor%5D=a","meta":{"note":"link metadata"}}}`, "?page%5Bcursor%5D=a", true},
		{`["https://foreign.example/"]`, "", false}, {`{"next":42}`, "", false}, {`{"next":{}}`, "", false}, {`{"next":{"href":null}}`, "", false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			var links pageLinks
			err := json.Unmarshal([]byte(tc.body), &links)
			if (err == nil) != tc.valid || err == nil && links.Next != tc.next {
				t.Fatal(links, err)
			}
		})
	}
}

func TestInventoryAcceptsLiveEmptyArrayLinks(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs":
			fmt.Fprint(w, `{"data":[{"id":"9","type":"organizations","attributes":{"slug":"team","name":"Team"}}],"links":[],"meta":{"next_cursor":null,"prev_cursor":null,"per_page":30}}`)
		case "/orgs/team/servers":
			fmt.Fprintf(w, `{"data":[%s],"links":[],"meta":{"next_cursor":null}}`, serverJSON)
		case "/orgs/team/sites":
			fmt.Fprintf(w, `{"data":[%s],"links":[],"meta":{"next_cursor":null}}`, siteJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, "fixture", s.Client())
	rows, err := c.Inventory(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("entries=%d error=%v", len(rows), err)
	}
}

func TestPaginationHrefAndCursorFallback(t *testing.T) {
	for _, object := range []bool{false, true} {
		t.Run(fmt.Sprint(object), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("include") != "server" {
					t.Error("include lost")
				}
				if calls == 1 {
					links := `[]`
					if object {
						links = `{"next":{"href":"?page%5Bcursor%5D=second"}}`
					}
					fmt.Fprintf(w, `{"data":[],"links":%s,"meta":{"next_cursor":"second"}}`, links)
				} else {
					if r.URL.Query().Get("page[cursor]") != "second" {
						t.Error("cursor lost")
					}
					fmt.Fprint(w, `{"data":[],"links":[],"meta":{"next_cursor":null}}`)
				}
			}))
			defer s.Close()
			c, _ := NewClient(s.URL, "fixture", s.Client())
			_, _, err := c.pages(context.Background(), "/sites", true)
			if err != nil || calls != 2 {
				t.Fatal(calls, err)
			}
		})
	}
}

func TestHrefStillRejectsForeignOrigin(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[],"links":{"next":{"href":"https://foreign.example/orgs?page%5Bcursor%5D=next"}},"meta":{"next_cursor":"next"}}`)
	}))
	defer s.Close()
	c, _ := NewClient(s.URL, "fixture", s.Client())
	if _, err := c.Inventory(context.Background()); err == nil {
		t.Fatal("foreign link accepted")
	}
}
