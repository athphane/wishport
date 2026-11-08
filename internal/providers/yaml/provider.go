package yaml

import (
	"context"
	"github.com/athphane/wishport/internal/inventory"
)

type Provider struct{ Connections []inventory.Connection }

func (Provider) Name() string { return "yaml" }
func (p Provider) List(ctx context.Context, _ bool) ([]inventory.Connection, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return append([]inventory.Connection(nil), p.Connections...), nil
}
