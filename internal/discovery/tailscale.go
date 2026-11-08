package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/athphane/wishport/internal/inventory"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Tailscale obtains secrets at request time; no credential is stored in inventory.
type Tailscale struct {
	Tailnet   string
	Timeout   time.Duration
	Client    *http.Client
	Token     func(context.Context) (string, error)
	LookupEnv func(string) string
}

func (*Tailscale) Name() string { return "tailscale" }
func (p *Tailscale) client() *http.Client {
	c := http.Client{Timeout: 15 * time.Second}
	if p.Client != nil {
		c = *p.Client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}
func requestJSON(ctx context.Context, client *http.Client, req *http.Request, dst any) error {
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("tailscale: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tailscale: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil {
		return errors.New("tailscale: response read failed")
	}
	if len(body) > 4*1024*1024 {
		return errors.New("tailscale: response too large")
	}
	if json.Unmarshal(body, dst) != nil {
		return errors.New("tailscale: invalid response")
	}
	return nil
}
func (p *Tailscale) token(ctx context.Context, c *http.Client) (string, error) {
	if p.Token != nil {
		token, err := p.Token(ctx)
		if err != nil {
			return "", errors.New("tailscale: credential lookup failed")
		}
		return token, nil
	}
	env := p.LookupEnv
	if env == nil {
		env = os.Getenv
	}
	if token := env("TAILSCALE_KEY"); token != "" {
		return token, nil
	}
	id, secret := env("TAILSCALE_CLIENT_ID"), env("TAILSCALE_CLIENT_SECRET")
	if id == "" || secret == "" {
		return "", errors.New("tailscale: missing API key or OAuth credentials")
	}
	values := url.Values{"grant_type": {"client_credentials"}, "client_id": {id}, "client_secret": {secret}, "scope": {"devices:read"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tailscale.com/api/v2/oauth/token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", errors.New("tailscale: invalid request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := requestJSON(ctx, c, req, &response); err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", errors.New("tailscale: missing access token")
	}
	return response.AccessToken, nil
}
func (p *Tailscale) List(parent context.Context, _ bool) ([]inventory.Connection, error) {
	ctx, cancel := bounded(parent, p.Timeout)
	defer cancel()
	tailnet := p.Tailnet
	if tailnet == "" {
		tailnet = "-"
	}
	if len(tailnet) > 253 || strings.ContainsAny(tailnet, "/\\?#\r\n") {
		return nil, errors.New("tailscale: invalid tailnet")
	}
	client := p.client()
	token, err := p.token(ctx, client)
	if err != nil {
		return nil, err
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("tailscale: invalid credential")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.tailscale.com/api/v2/tailnet/"+url.PathEscape(tailnet)+"/devices", nil)
	if err != nil {
		return nil, errors.New("tailscale: invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	var response struct {
		Devices []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Hostname  string   `json:"hostname"`
			Addresses []string `json:"addresses"`
		} `json:"devices"`
	}
	if err := requestJSON(ctx, client, req, &response); err != nil {
		return nil, err
	}
	out := []inventory.Connection{}
	seen := map[string]bool{}
	for _, d := range response.Devices {
		host := ""
		for _, addr := range d.Addresses {
			if ip := net.ParseIP(addr); ip != nil {
				host = ip.String()
				break
			}
		}
		if host == "" {
			host = validHost(d.Name)
		}
		name := strings.Split(d.Name, ".")[0]
		if name == "" {
			name = d.Hostname
		}
		key := d.ID
		if key == "" {
			key = host
		}
		c, ok := connection("tailscale", tailnet+"/"+key, name, host, 22)
		if ok && !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
			if len(out) >= maxEntries {
				break
			}
		}
	}
	inventory.Sort(out)
	return out, nil
}
