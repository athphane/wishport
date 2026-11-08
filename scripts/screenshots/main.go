// Command screenshots renders the real TUI from embedded fictional fixtures.
// It never constructs the application service, reads user config or credentials,
// contacts Forge, or starts SSH. Run from the repository root; see README.md here.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/athphane/wishport/internal/config"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/athphane/wishport/internal/providers/sshconfig"
	"github.com/athphane/wishport/internal/providers/wishlist"
	yamlprovider "github.com/athphane/wishport/internal/providers/yaml"
	"github.com/athphane/wishport/internal/tui"
)

//go:embed fixtures/*
var fixtures embed.FS

type demoService struct{ snapshot inventory.Snapshot }

func (s demoService) Load(context.Context, bool) inventory.Snapshot { return s.snapshot }
func (demoService) Command(context.Context, inventory.Connection) (*exec.Cmd, error) {
	return nil, errors.New("connections are disabled in the screenshot fixture")
}

func run() error {
	view := flag.String("view", "directory", "directory, forge, or search")
	flag.Parse()
	if *view != "directory" && *view != "forge" && *view != "search" {
		return errors.New("unknown screenshot view")
	}
	dir, err := os.MkdirTemp("", "wishport-screenshots-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"config.yaml", "ssh_config", "wishlist"} {
		data, err := fixtures.ReadFile("fixtures/" + name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	cfg, err := config.Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return err
	}
	ctx := context.Background()
	snapshot := inventory.Snapshot{Status: "Demo inventory · fictional hosts and Forge sites"}
	// Explicit paths only: no automatic discovery of personal configuration.
	for _, provider := range []inventory.Provider{
		sshconfig.Provider{Path: filepath.Join(dir, "ssh_config")},
		&wishlist.Provider{Path: filepath.Join(dir, "wishlist")},
		yamlprovider.Provider{Connections: cfg.Connections},
	} {
		rows, err := provider.List(ctx, false)
		if err != nil {
			return err
		}
		snapshot.Connections = append(snapshot.Connections, rows...)
	}
	data, err := fixtures.ReadFile("fixtures/forge.json")
	if err != nil {
		return err
	}
	var rows []inventory.Connection
	if err := json.Unmarshal(data, &rows); err != nil {
		return err
	}
	snapshot.Connections = append(snapshot.Connections, rows...)
	inventory.Sort(snapshot.Connections)
	model := tui.New(ctx, demoService{snapshot: snapshot})
	model.Update(model.Init()())
	width, height := 104, 30
	if *view == "forge" {
		width, height = 130, 28
	}
	if *view == "search" {
		height = 26
	}
	model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	key := func(r rune) { model.Update(tea.KeyPressMsg{Code: r, Text: string(r)}) }
	if *view == "forge" {
		model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		key('t')
	}
	if *view == "search" {
		key('/')
		for _, r := range "server:production" {
			key(r)
		}
	}
	_, err = fmt.Fprintln(os.Stdout, model.View().Content)
	return err
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
