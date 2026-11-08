package forge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/athphane/wishport/internal/inventory"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const DefaultCacheTTL = 168 * time.Hour

func Partition(base, token string) string {
	sum := sha256.Sum256([]byte(base + "\x00" + token))
	return hex.EncodeToString(sum[:])
}

type Cache struct {
	Partition string
	Dir       string
	Now       func() time.Time
}
type cachedConnection struct {
	ID               string                   `json:"id"`
	Kind             inventory.ConnectionKind `json:"kind"`
	Name             string                   `json:"name"`
	Host             string                   `json:"host,omitempty"`
	Port             int                      `json:"port,omitempty"`
	User             string                   `json:"user,omitempty"`
	Directory        string                   `json:"directory,omitempty"`
	Organization     string                   `json:"organization,omitempty"`
	OrganizationName string                   `json:"organization_name,omitempty"`
	Unavailable      string                   `json:"unavailable,omitempty"`
	ServerName       string                   `json:"server_name,omitempty"`
	ServerID         string                   `json:"server_id,omitempty"`
	SiteID           string                   `json:"site_id,omitempty"`
}
type cacheDocument struct {
	Version     int                `json:"version"`
	SavedAt     time.Time          `json:"saved_at"`
	Connections []cachedConnection `json:"connections"`
}

var partitionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func NewCache(partition string) (*Cache, error) {
	c := &Cache{Partition: partition}
	_, e := c.path()
	return c, e
}
func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
func (c *Cache) path() (string, error) {
	if !partitionPattern.MatchString(c.Partition) {
		return "", fmt.Errorf("invalid Forge cache partition")
	}
	dir := c.Dir
	if dir == "" {
		d := os.Getenv("XDG_CACHE_HOME")
		if d == "" || !filepath.IsAbs(d) {
			home, e := os.UserHomeDir()
			if e != nil {
				return "", fmt.Errorf("find cache directory")
			}
			d = filepath.Join(home, ".cache")
		}
		dir = filepath.Join(d, "wishport")
	}
	return filepath.Join(dir, c.Partition+".json"), nil
}
func (c *Cache) Save(rows []inventory.Connection) error {
	path, e := c.path()
	if e != nil {
		return e
	}
	dir := filepath.Dir(path)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return fmt.Errorf("create Forge cache directory")
	}
	if e = os.Chmod(dir, 0700); e != nil {
		return fmt.Errorf("secure Forge cache directory")
	}
	doc := cacheDocument{Version: 1, SavedAt: c.now().UTC(), Connections: make([]cachedConnection, 0, len(rows))}
	for _, r := range rows {
		doc.Connections = append(doc.Connections, cachedConnection{ID: r.ID, Kind: r.Kind, Name: r.Name, Host: r.Host, Port: r.Port, User: r.User, Directory: r.Directory, Organization: r.Organization, OrganizationName: r.OrganizationName, Unavailable: r.Unavailable, ServerName: r.ServerName, ServerID: r.ServerID, SiteID: r.SiteID})
	}
	f, e := os.CreateTemp(dir, ".forge-*")
	if e != nil {
		return fmt.Errorf("create Forge cache file")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		e = json.NewEncoder(f).Encode(doc)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return fmt.Errorf("write Forge cache")
	}
	if e = os.Rename(tmp, path); e != nil {
		return fmt.Errorf("replace Forge cache")
	}
	return nil
}
func (c *Cache) Load(ttl time.Duration) ([]inventory.Connection, time.Time, bool, error) {
	path, e := c.path()
	if e != nil {
		return nil, time.Time{}, false, e
	}
	f, e := os.Open(path)
	if os.IsNotExist(e) && c.Dir == "" {
		// Read old snapshots in place; all new writes use Wishport's namespace.
		f, e = os.Open(filepath.Join(filepath.Dir(filepath.Dir(path)), "sshdir", filepath.Base(path)))
	}
	if e != nil {
		return nil, time.Time{}, false, fmt.Errorf("read Forge cache: %w", e)
	}
	defer f.Close()
	var d cacheDocument
	dec := json.NewDecoder(io.LimitReader(f, 16<<20))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil || d.Version != 1 || d.SavedAt.IsZero() || d.SavedAt.After(c.now().Add(time.Minute)) {
		return nil, time.Time{}, false, errors.New("invalid Forge cache; refresh to replace it")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, time.Time{}, false, errors.New("invalid Forge cache; refresh to replace it")
	}
	rows := make([]inventory.Connection, 0, len(d.Connections))
	for _, r := range d.Connections {
		rows = append(rows, inventory.Connection{ID: r.ID, Source: inventory.SourceForge, Kind: r.Kind, Name: r.Name, Host: r.Host, Port: r.Port, User: r.User, Directory: r.Directory, Organization: r.Organization, OrganizationName: r.OrganizationName, Unavailable: r.Unavailable, ServerName: r.ServerName, ServerID: r.ServerID, SiteID: r.SiteID})
	}
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	stale := c.now().Sub(d.SavedAt) > ttl
	return rows, d.SavedAt, stale, nil
}
func (c *Cache) Forget() error {
	path, e := c.path()
	if e != nil {
		return e
	}
	paths := []string{path}
	if c.Dir == "" {
		paths = append(paths, filepath.Join(filepath.Dir(filepath.Dir(path)), "sshdir", filepath.Base(path)))
	}
	for _, path := range paths {
		if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			return fmt.Errorf("remove Forge cache")
		}
	}
	return nil
}
