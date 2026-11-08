package server

import (
	"errors"
	"io"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/crypto/ssh"
)

func authorizedUsers(path string) (map[string]map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read authorized users file")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return nil, errors.New("authorized users file exceeds limit or cannot be read")
	}
	var document struct {
		Users []struct {
			Name       string   `yaml:"name"`
			PublicKeys []string `yaml:"public-keys"`
		} `yaml:"users"`
	}
	if yaml.Unmarshal(b, &document) != nil {
		return nil, errors.New("invalid authorized users YAML")
	}
	users := map[string]map[string]bool{}
	for _, user := range document.Users {
		if user.Name == "" || strings.ContainsAny(user.Name, "\x00\r\n\t ") || users[user.Name] != nil || len(user.PublicKeys) == 0 {
			return nil, errors.New("authorized users require unique names and public keys")
		}
		users[user.Name] = map[string]bool{}
		for _, text := range user.PublicKeys {
			key, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(text))
			if err != nil || len(options) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
				return nil, errors.New("authorized user keys must be raw public keys without options")
			}
			if _, cert := key.(*ssh.Certificate); cert {
				return nil, errors.New("authorized user certificates are unsupported")
			}
			users[user.Name][string(key.Marshal())] = true
		}
	}
	if len(users) == 0 {
		return nil, errors.New("authorized users file contains no users")
	}
	return users, nil
}
