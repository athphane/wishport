package discovery

import (
	"context"
	"errors"
	z "github.com/libp2p/zeroconf/v2"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestTailscaleAPIAndOAuth(t *testing.T) {
	for _, oauth := range []bool{false, true} {
		t.Run(map[bool]string{false: "key", true: "oauth"}[oauth], func(t *testing.T) {
			calls := 0
			p := Tailscale{Tailnet: "example.com", LookupEnv: func(k string) string {
				if oauth {
					if k == "TAILSCALE_CLIENT_ID" {
						return "client"
					}
					if k == "TAILSCALE_CLIENT_SECRET" {
						return "secret"
					}
					return ""
				}
				if k == "TAILSCALE_KEY" {
					return "key"
				}
				return ""
			}, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if strings.HasSuffix(r.URL.Path, "/oauth/token") {
					r.ParseForm()
					if r.Form.Get("scope") != "devices:read" || r.Form.Get("client_secret") != "secret" {
						t.Fatal(r.Form)
					}
					return response(`{"access_token":"oauth"}`), nil
				}
				want := "Bearer key"
				if oauth {
					want = "Bearer oauth"
				}
				if r.Header.Get("Authorization") != want {
					t.Fatal("auth missing")
				}
				return response(`{"devices":[{"id":"one","name":"hello.tail.ts.net","addresses":["100.64.0.1"]},{"id":"two","name":"world","addresses":["fd7a:115c:a1e0::2"]},{"id":"bad","name":"-oProxyCommand=evil"}]}`), nil
			})}}
			rows, err := p.List(context.Background(), false)
			if err != nil || len(rows) != 2 || rows[0].Name != "hello" {
				t.Fatal(rows, err)
			}
			if oauth && calls != 2 {
				t.Fatal(calls)
			}
		})
	}
}
func TestTailscaleSafeErrorsAndCancellation(t *testing.T) {
	p := Tailscale{Token: func(context.Context) (string, error) { return "secret", nil }, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret"))}, nil
	})}}
	_, err := p.List(context.Background(), false)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	p.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.List(ctx, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestTailscaleRejectsRedirect(t *testing.T) {
	p := Tailscale{Token: func(context.Context) (string, error) { return "key", nil }, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.tailscale.com" {
			t.Fatal("token leaked")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	_, err := p.List(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatal(err)
	}
}

type resolver struct{ noTXT bool }

func (resolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", []*net.SRV{{Target: "host.example.", Port: 2222}, {Target: "host.example.", Port: 2222}, {Target: "-evil.", Port: 22}}, nil
}
func (r resolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if r.noTXT {
		return nil, errors.New("not found")
	}
	if name == "example.com" {
		return []string{"wishlist.name host.example:2222=My\x1b server"}, nil
	}
	return nil, nil
}
func TestSRVNamesMissingTXTAndDedup(t *testing.T) {
	for _, missing := range []bool{false, true} {
		p := SRV{Domains: []string{"example.com"}, Resolver: resolver{missing}}
		rows, err := p.List(context.Background(), false)
		if err != nil || len(rows) != 1 || rows[0].Port != 2222 {
			t.Fatal(rows, err)
		}
		if !missing && rows[0].Name != "My server" {
			t.Fatal(rows)
		}
	}
}

type browserFunc func(context.Context, string, string, chan<- *z.ServiceEntry) error

func (f browserFunc) Browse(c context.Context, s, d string, ch chan<- *z.ServiceEntry) error {
	return f(c, s, d, ch)
}
func TestMDNSDedupAndFiniteBrowse(t *testing.T) {
	p := MDNS{Timeout: 20 * time.Millisecond, Browser: browserFunc(func(ctx context.Context, s, d string, ch chan<- *z.ServiceEntry) error {
		if s != "_ssh._tcp" || d != "local." {
			t.Fatal(s, d)
		}
		go func() {
			defer close(ch)
			for i := 0; i < 2; i++ {
				select {
				case ch <- &z.ServiceEntry{HostName: "host.local.", Port: 22}:
				case <-ctx.Done():
					return
				}
			}
			<-ctx.Done()
		}()
		return nil
	})}
	rows, err := p.List(context.Background(), false)
	if err != nil || len(rows) != 1 || rows[0].Host != "host.local" {
		t.Fatal(rows, err)
	}
}
func TestMDNSParentCancellation(t *testing.T) {
	p := MDNS{Browser: browserFunc(func(ctx context.Context, _, _ string, ch chan<- *z.ServiceEntry) error {
		go func() { <-ctx.Done(); close(ch) }()
		return nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.List(ctx, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestHostValidation(t *testing.T) {
	for _, s := range []string{"-oProxyCommand=x", "host\nname", "host:22", "[::1]", "a..b", "/tmp/socket"} {
		if validHost(s) != "" {
			t.Fatal(s)
		}
	}
	if validHost("::1") != "::1" {
		t.Fatal("IPv6")
	}
}

func TestMDNSBlockingBrowser(t *testing.T) {
	p := MDNS{Timeout: 20 * time.Millisecond, Browser: browserFunc(func(ctx context.Context, _, _ string, ch chan<- *z.ServiceEntry) error {
		defer close(ch)
		select {
		case ch <- &z.ServiceEntry{HostName: "blocking.local.", Port: 22}:
		case <-ctx.Done():
			return ctx.Err()
		}
		<-ctx.Done()
		return ctx.Err()
	})}
	rows, err := p.List(context.Background(), false)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}
