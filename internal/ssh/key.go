package ssh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	cryptossh "golang.org/x/crypto/ssh"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Key struct{ Path, Public, Fingerprint, Name, Identity string }

func LoadKey(ctx context.Context, runner Runner, configured string) (Key, error) {
	var result Key
	if runner == nil {
		runner = OSRunner{}
	}
	if e := ctx.Err(); e != nil {
		return result, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return result, errors.New("cannot find SSH key directory")
	}
	path := configured
	if path == "" {
		var choices []string
		for _, name := range []string{"id_ed25519.pub", "id_rsa.pub"} {
			p := filepath.Join(home, ".ssh", name)
			if _, e := os.Stat(p); e == nil {
				choices = append(choices, p)
			} else if !os.IsNotExist(e) {
				return result, errors.New("cannot inspect default public SSH key")
			}
		}
		if len(choices) > 1 {
			return result, errors.New("multiple public SSH keys found; configure forge.public_key to choose one")
		}
		if len(choices) == 0 {
			return result, errors.New("no public SSH key found; configure forge.public_key with an existing .pub file")
		}
		path = choices[0]
	}
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	if filepath.Ext(path) != ".pub" || strings.ContainsAny(path, "\x00\r\n%$") {
		return result, errors.New("forge.public_key must name an existing .pub file")
	}
	path, e = filepath.Abs(path)
	if e != nil {
		return result, errors.New("invalid public SSH key path")
	}
	f, e := os.Open(path)
	if e != nil {
		return result, errors.New("cannot read public SSH key file")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 65536 {
		return result, errors.New("public SSH key must be a regular file smaller than 64 KiB")
	}
	data, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil || len(data) > 65536 {
		return result, errors.New("cannot read public SSH key file")
	}
	pub, _, options, rest, e := cryptossh.ParseAuthorizedKey(data)
	if e != nil || len(options) > 0 || len(bytes.TrimSpace(rest)) > 0 {
		return result, errors.New("public SSH key must contain exactly one public key without authorized_keys options")
	}
	// Keep the remote key identifier stable across the Wishport rename.
	sum := sha256.Sum256(pub.Marshal())
	result = Key{Path: path, Public: strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(pub))), Fingerprint: cryptossh.FingerprintSHA256(pub), Name: "sshdir-" + hex.EncodeToString(sum[:])}
	if agent, e := runner.Output(ctx, "ssh-add", "-L"); e == nil && agentHas(agent, pub.Marshal()) {
		result.Identity = path
		return result, nil
	}
	if e := ctx.Err(); e != nil {
		return Key{}, e
	}
	private := strings.TrimSuffix(path, ".pub")
	derived, e := runner.Output(ctx, "ssh-keygen", "-y", "-P", "", "-f", private)
	if e != nil {
		return Key{}, errors.New("SSH key is not usable noninteractively; unlock it with ssh-add or choose a usable public key")
	}
	actual, _, options, rest, e := cryptossh.ParseAuthorizedKey(derived)
	if e != nil || len(options) > 0 || len(bytes.TrimSpace(rest)) > 0 || !bytes.Equal(actual.Marshal(), pub.Marshal()) {
		return Key{}, errors.New("public SSH key does not match its private key; choose a matching .pub file")
	}
	result.Identity = private
	return result, nil
}
func agentHas(data, want []byte) bool {
	for len(bytes.TrimSpace(data)) > 0 {
		pub, _, options, rest, e := cryptossh.ParseAuthorizedKey(data)
		if e != nil {
			return false
		}
		if len(options) == 0 && bytes.Equal(pub.Marshal(), want) {
			return true
		}
		data = rest
	}
	return false
}
