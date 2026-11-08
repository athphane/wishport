package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://forge.laravel.com/api"
const maxPages = 1000

type Client struct {
	base      *url.URL
	token     string
	http      *http.Client
	partition string
}

func NewClient(base, token string, client *http.Client) (*Client, error) {
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid Forge base URL")
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("invalid Forge token")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	hc := http.Client{}
	if client != nil {
		hc = *client
	}
	if hc.Timeout == 0 {
		hc.Timeout = 20 * time.Second
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: u, token: token, http: &hc, partition: Partition(u.String(), token)}, nil
}
func (c *Client) endpoint(path string, q url.Values) *url.URL {
	u := *c.base
	u.Path += path
	u.RawQuery = q.Encode()
	return &u
}
func (c *Client) get(ctx context.Context, u *url.URL, dst any) error {
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return errors.New("invalid Forge request")
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.api+json")
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("forge request failed")
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			delay := retryDelay(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			if delay > 5*time.Second {
				return errors.New("forge retry delay exceeds retry budget")
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return apiError(resp.StatusCode)
		}
		decoder := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
		err = decoder.Decode(dst)
		if err == nil {
			var extra any
			if e := decoder.Decode(&extra); e != io.EOF {
				err = errors.New("trailing data")
			}
		}
		resp.Body.Close()
		if err != nil {
			return errors.New("invalid Forge API response")
		}
		return nil
	}
	return errors.New("forge rate limit exceeded")
}

func apiError(status int) error {
	hint := "request failed"
	switch status {
	case http.StatusUnauthorized:
		hint = "token is invalid or expired; log in again"
	case http.StatusForbidden:
		hint = "permission denied; check token scopes and organization access"
	case http.StatusNotFound:
		hint = "resource was not found or is no longer accessible; refresh inventory"
	case http.StatusUnprocessableEntity:
		hint = "request was rejected; verify the selected user and public key"
	case http.StatusTooManyRequests:
		hint = "rate limit reached; retry after the server's waiting period"
	case http.StatusServiceUnavailable:
		hint = "service is temporarily unavailable; cached inventory remains usable"
	}
	return fmt.Errorf("forge API returned HTTP %d: %s", status, hint)
}
func retryDelay(s string) time.Duration {
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		if n > 5 {
			return 6 * time.Second
		}
		return time.Duration(n) * time.Second
	}
	if s != "" && strings.Trim(s, "0123456789") == "" {
		return 6 * time.Second
	}
	if t, err := http.ParseTime(s); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return time.Second
}
func segment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (c *Client) pages(ctx context.Context, path string, include bool) ([]resource, []resource, error) {
	q := url.Values{}
	if include {
		q.Set("include", "server")
	}
	u := c.endpoint(path, q)
	expectedPath := u.Path
	seen := map[string]bool{}
	var all, included []resource
	for i := 0; i < maxPages; i++ {
		key := u.RawQuery
		if seen[key] {
			return all, included, errors.New("forge pagination cycle")
		}
		seen[key] = true
		var page pageResponse
		if err := c.get(ctx, u, &page); err != nil {
			return all, included, err
		}
		if page.Data == nil {
			return all, included, errors.New("invalid Forge API response")
		}
		var cursor *string
		if len(page.Meta.NextCursor) == 0 || json.Unmarshal(page.Meta.NextCursor, &cursor) != nil || (cursor != nil && *cursor == "") {
			return all, included, errors.New("invalid Forge pagination metadata")
		}
		all = append(all, page.Data...)
		included = append(included, page.Included...)
		if page.Links.Next != "" {
			next, err := url.Parse(page.Links.Next)
			if err != nil {
				return all, included, errors.New("invalid Forge pagination link")
			}
			next = u.ResolveReference(next)
			if next.Scheme != c.base.Scheme || next.Host != c.base.Host || next.User != nil || next.Fragment != "" || next.Path != expectedPath {
				return all, included, errors.New("invalid Forge pagination link")
			}
			nq := next.Query()
			if len(nq["page[cursor]"]) != 1 || cursor == nil || nq.Get("page[cursor]") != *cursor {
				return all, included, errors.New("invalid Forge pagination link")
			}
			for k := range nq {
				if k != "page[cursor]" && k != "page[size]" && k != "include" {
					return all, included, errors.New("invalid Forge pagination link")
				}
			}
			if include {
				nq.Set("include", "server")
			} else if nq.Has("include") {
				return all, included, errors.New("invalid Forge pagination link")
			}
			next.RawQuery = nq.Encode()
			u = next
			continue
		}
		if cursor != nil {
			q := u.Query()
			q.Set("page[cursor]", *cursor)
			if include {
				q.Set("include", "server")
			}
			v := *u
			v.RawQuery = q.Encode()
			u = &v
			continue
		}
		return all, included, nil
	}
	return all, included, errors.New("forge pagination limit exceeded")
}
