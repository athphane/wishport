// Package interaction contains UI-independent consent and progress messages.
package interaction

import (
	"context"
	"github.com/athphane/wishport/internal/inventory"
)

type Consent struct {
	Target        inventory.Connection `json:"target"`
	PublicKeyPath string               `json:"public_key_path"`
	Fingerprint   string               `json:"fingerprint"`
	Message       string               `json:"message"`
}
type Confirm func(context.Context, Consent) (bool, error)
type Progress func(string)
