package client

import (
	"context"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
)

//go:generate go tool mockgen -destination ../testing/client_test.go -package testing . Client

type Client interface {
	CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*zero_trust.TunnelCloudflaredNewResponse, error)
}

type client struct {
	*cloudflare.Client
}

func New() Client {
	return &client{cloudflare.NewClient()}
}

// CreateTunnel implements Client.
func (c *client) CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*zero_trust.TunnelCloudflaredNewResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.New(ctx, params)
}
