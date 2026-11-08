package inventory

import (
	"context"
	"time"
)

type Source string

const (
	SourceTailscale Source = "tailscale"
	SourceMDNS      Source = "mdns"
	SourceSRV       Source = "srv"
	SourceSSHConfig Source = "ssh_config"
	SourceWishlist  Source = "wishlist"
	SourceYAML      Source = "yaml"
	SourceForge     Source = "forge"
)

type ConnectionKind string

const (
	KindHost        ConnectionKind = "host"
	KindForgeServer ConnectionKind = "forge_server"
	KindForgeSite   ConnectionKind = "forge_site"
)

type Link struct {
	Name string `json:"name,omitempty" yaml:"name"`
	URL  string `json:"url" yaml:"url"`
}

type SSHOptions struct {
	ForwardAgent             *bool         `json:"forward_agent,omitempty" yaml:"forward_agent"`
	RequestTTY               *bool         `json:"request_tty,omitempty" yaml:"request_tty"`
	RemoteCommand            string        `json:"remote_command,omitempty" yaml:"remote_command"`
	ProxyJump                string        `json:"proxy_jump,omitempty" yaml:"proxy_jump"`
	SendEnv                  []string      `json:"send_env,omitempty" yaml:"send_env"`
	SetEnv                   []string      `json:"set_env,omitempty" yaml:"set_env"`
	PreferredAuthentications []string      `json:"preferred_authentications,omitempty" yaml:"preferred_authentications"`
	IdentityFiles            []string      `json:"identity_files,omitempty" yaml:"identity_files"`
	ConnectTimeout           time.Duration `json:"connect_timeout,omitempty" yaml:"connect_timeout"`
}

type Connection struct {
	SSHOptions       *SSHOptions    `json:"ssh_options,omitempty" yaml:"-"`
	Link             *Link          `json:"link,omitempty" yaml:"-"`
	ID               string         `json:"id" yaml:"-"`
	Source           Source         `json:"source" yaml:"-"`
	Kind             ConnectionKind `json:"kind" yaml:"-"`
	Name             string         `json:"name" yaml:"name"`
	Description      string         `json:"description,omitempty" yaml:"description"`
	Host             string         `json:"host,omitempty" yaml:"host"`
	SSHAlias         string         `json:"ssh_alias,omitempty" yaml:"ssh_alias"`
	SSHConfigFile    string         `json:"ssh_config_file,omitempty" yaml:"-"`
	Port             int            `json:"port,omitempty" yaml:"port"`
	User             string         `json:"user,omitempty" yaml:"user"`
	Identity         string         `json:"identity,omitempty" yaml:"identity"`
	Directory        string         `json:"directory,omitempty" yaml:"directory"`
	Organization     string         `json:"organization,omitempty" yaml:"-"`
	OrganizationName string         `json:"organization_name,omitempty" yaml:"-"`
	ServerName       string         `json:"server_name,omitempty" yaml:"-"`
	ServerID         string         `json:"server_id,omitempty" yaml:"-"`
	SiteID           string         `json:"site_id,omitempty" yaml:"-"`
	SearchTerms      []string       `json:"search_terms,omitempty" yaml:"-"`
	Unavailable      string         `json:"unavailable,omitempty" yaml:"-"`
}
type Provider interface {
	Name() string
	List(context.Context, bool) ([]Connection, error)
}
type Warning struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}
type Snapshot struct {
	NeedsRefresh bool         `json:"-"`
	Connections  []Connection `json:"connections"`
	Warnings     []Warning    `json:"warnings"`
	Status       string       `json:"status,omitempty"`
}
