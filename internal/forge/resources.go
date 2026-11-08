package forge

import (
	"bytes"
	"encoding/json"
	"errors"
)

type resource struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name          string `json:"name"`
		Status        string `json:"status"`
		Slug          string `json:"slug"`
		IPAddress     string `json:"ip_address"`
		SSHPort       int    `json:"ssh_port"`
		IsReady       *bool  `json:"is_ready"`
		Revoked       *bool  `json:"revoked"`
		User          string `json:"user"`
		RootDirectory string `json:"root_directory"`
	} `json:"attributes"`
	Relationships struct {
		Server struct {
			Data *struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
		} `json:"server"`
	} `json:"relationships"`
}
type pageResponse struct {
	Data     []resource `json:"data"`
	Included []resource `json:"included"`
	Links    pageLinks  `json:"links"`
	Meta     struct {
		NextCursor json.RawMessage `json:"next_cursor"`
	} `json:"meta"`
}

// Forge emits an empty PHP array for links when there are no page links.
// Nonempty links use JSON:API's string or {href: ...} representation. Keep the
// URL validation in pages: tolerating an empty collection must not relax it.
type pageLinks struct{ Next string }

func (l *pageLinks) UnmarshalJSON(data []byte) error {
	l.Next = ""
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '[' {
		var links []json.RawMessage
		if json.Unmarshal(data, &links) != nil || len(links) != 0 {
			return errors.New("invalid Forge pagination links")
		}
		return nil
	}
	var links map[string]json.RawMessage
	if json.Unmarshal(data, &links) != nil {
		return errors.New("invalid Forge pagination links")
	}
	next, ok := links["next"]
	if !ok || bytes.Equal(bytes.TrimSpace(next), []byte("null")) {
		return nil
	}
	if json.Unmarshal(next, &l.Next) == nil {
		return nil
	}
	var link struct {
		Href *string `json:"href"`
	}
	if json.Unmarshal(next, &link) != nil || link.Href == nil || *link.Href == "" {
		return errors.New("invalid Forge next-page link")
	}
	l.Next = *link.Href
	return nil
}

type oneResponse struct {
	Data     resource   `json:"data"`
	Included []resource `json:"included"`
}
