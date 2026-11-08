package ssh

import (
	"fmt"
	"github.com/athphane/wishport/internal/inventory"
	"github.com/gobwas/glob"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode"
)

func validateOptions(c inventory.Connection) error {
	o := c.SSHOptions
	if o == nil {
		return nil
	}
	if !trustedOptionsSource(c.Source) {
		return fmt.Errorf("SSH options require trusted local Wishlist configuration")
	}
	if c.Directory != "" && o.RemoteCommand != "" {
		return fmt.Errorf("remote directory and remote command cannot both be set")
	}
	if strings.ContainsRune(o.RemoteCommand, 0) {
		return fmt.Errorf("invalid remote command")
	}
	if o.ConnectTimeout < 0 {
		return fmt.Errorf("invalid connection timeout")
	}
	for _, v := range o.IdentityFiles {
		if v == "" || strings.ContainsAny(v, "\x00\r\n%$") {
			return fmt.Errorf("invalid identity path")
		}
	}
	if o.ProxyJump != "" && o.ProxyJump != "none" {
		for _, jump := range strings.Split(o.ProxyJump, ",") {
			host := jump
			if at := strings.IndexByte(host, '@'); at >= 0 {
				if !validName(host[:at]) {
					return fmt.Errorf("invalid proxy jump")
				}
				host = host[at+1:]
			}
			if strings.HasPrefix(host, "[") || strings.Count(host, ":") == 1 {
				h, p, e := net.SplitHostPort(host)
				if e != nil {
					return fmt.Errorf("invalid proxy jump")
				}
				port, e := strconv.Atoi(p)
				if e != nil || port < 1 || port > 65535 {
					return fmt.Errorf("invalid proxy jump")
				}
				host = h
			}
			if !validTarget(host) {
				return fmt.Errorf("invalid proxy jump")
			}
		}
	}
	for _, v := range o.SendEnv {
		if !validEnvPattern(v) {
			return fmt.Errorf("invalid environment pattern")
		}
	}
	for _, v := range o.SetEnv {
		k, value, ok := strings.Cut(v, "=")
		if !ok || !validEnvName(k) || strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.Contains(value, "${") {
			return fmt.Errorf("invalid environment assignment")
		}
	}
	for _, v := range o.PreferredAuthentications {
		switch v {
		case "publickey", "password", "keyboard-interactive", "hostbased", "gssapi-with-mic", "none":
		default:
			return fmt.Errorf("invalid authentication method")
		}
	}
	return nil
}
func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
func validEnvPattern(s string) bool {
	if s == "" || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	_, err := glob.Compile(s)
	return err == nil
}

func optionsArgs(o *inventory.SSHOptions) []string {
	args := []string{}
	add := func(k, v string) { args = append(args, "-o", k+"="+v) }
	if o.ForwardAgent != nil {
		if *o.ForwardAgent {
			add("ForwardAgent", "yes")
		} else {
			add("ForwardAgent", "no")
		}
	}
	tty := o.RemoteCommand == ""
	if o.RequestTTY != nil {
		tty = *o.RequestTTY
	}
	if tty {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	if o.ProxyJump != "" {
		add("ProxyJump", o.ProxyJump)
	}
	patterns := append([]string{"LC_*", "LANG"}, o.SendEnv...)
	send := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !validEnvName(name) {
			continue
		}
		for _, pattern := range patterns {
			matcher, err := glob.Compile(pattern)
			if err == nil && matcher.Match(name) {
				send = append(send, name)
				break
			}
		}
	}
	add("SendEnv", "-*")
	if len(send) > 0 {
		add("SendEnv", strings.Join(send, " "))
	}
	if len(o.SetEnv) > 0 {
		values := []string{}
		last := map[string]int{}
		for i, v := range o.SetEnv {
			key, _, _ := strings.Cut(v, "=")
			last[key] = i
		}
		for i, v := range o.SetEnv {
			key, _, _ := strings.Cut(v, "=")
			if last[key] != i {
				continue
			}
			escaped := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(v, "%", "%%"), "\\", "\\\\"), "\"", "\\\"")
			values = append(values, "\""+escaped+"\"")
		}
		add("SetEnv", strings.Join(values, " "))
	}
	auth := o.PreferredAuthentications
	if len(auth) == 0 {
		auth = []string{"publickey", "keyboard-interactive"}
	}
	add("PreferredAuthentications", strings.Join(auth, ","))
	for _, v := range o.IdentityFiles {
		args = append(args, "-i", v)
	}
	if o.ConnectTimeout > 0 {
		seconds := int64(o.ConnectTimeout / 1000000000)
		if o.ConnectTimeout%1000000000 != 0 {
			seconds++
		}
		add("ConnectTimeout", strconv.FormatInt(seconds, 10))
	}
	return args
}

func trustedOptionsSource(source inventory.Source) bool {
	switch source {
	case inventory.SourceWishlist, inventory.SourceTailscale, inventory.SourceMDNS, inventory.SourceSRV:
		return true
	}
	return false
}
