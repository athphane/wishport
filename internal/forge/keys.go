package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type Key struct{ ID, Name, User, Status string }

func keyPath(org, server string) (string, error) {
	if !segment(org) || !segment(server) {
		return "", errors.New("invalid Forge organization or server identifier")
	}
	return "/orgs/" + org + "/servers/" + server + "/ssh-keys", nil
}
func convertKey(r resource) (Key, error) {
	if r.Type != "keys" || !segment(r.ID) || r.Attributes.Name == "" || r.Attributes.User == "" || r.Attributes.Status == "" {
		return Key{}, errors.New("invalid Forge SSH key response")
	}
	return Key{ID: r.ID, Name: r.Attributes.Name, User: r.Attributes.User, Status: r.Attributes.Status}, nil
}
func (c *Client) ListKeys(ctx context.Context, org, server string) ([]Key, error) {
	path, e := keyPath(org, server)
	if e != nil {
		return nil, e
	}
	resources, _, e := c.pages(ctx, path, false)
	if e != nil {
		return nil, e
	}
	keys := make([]Key, 0, len(resources))
	for _, r := range resources {
		k, e := convertKey(r)
		if e != nil {
			return nil, e
		}
		keys = append(keys, k)
	}
	return keys, nil
}
func (c *Client) GetKey(ctx context.Context, org, server, id string) (Key, error) {
	path, e := keyPath(org, server)
	if e != nil {
		return Key{}, e
	}
	if !segment(id) {
		return Key{}, errors.New("invalid Forge SSH key identifier")
	}
	var response oneResponse
	if e = c.get(ctx, c.endpoint(path+"/"+id, nil), &response); e != nil {
		return Key{}, e
	}
	key, e := convertKey(response.Data)
	if e != nil || key.ID != id {
		return Key{}, errors.New("invalid Forge SSH key response")
	}
	return key, nil
}

// CreateKey deliberately makes one attempt: replaying a write can install duplicates.
func (c *Client) CreateKey(ctx context.Context, org, server, name, user, public string) error {
	path, e := keyPath(org, server)
	if e != nil {
		return e
	}
	if name == "" || user == "" || public == "" || strings.ContainsAny(name+user+public, "\x00\r\n") {
		return errors.New("invalid Forge SSH key installation request")
	}
	body, e := json.Marshal(struct {
		Name string `json:"name"`
		Key  string `json:"key"`
		User string `json:"user"`
	}{name, public, user})
	if e != nil {
		return errors.New("invalid Forge SSH key installation request")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path, nil).String(), bytes.NewReader(body))
	if e != nil {
		return errors.New("invalid Forge SSH key installation request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("forge SSH key installation request failed; verify status before retrying")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("forge SSH key installation returned HTTP %d", resp.StatusCode)
	}
	return nil
}
