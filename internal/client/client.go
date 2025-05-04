package client

import (
	"context"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
)

//go:generate go tool mockgen -destination ../testing/client.go -package testing . Client

type Client interface {
	CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*zero_trust.TunnelCloudflaredNewResponse, error)
	DeleteTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredDeleteParams) (*zero_trust.TunnelCloudflaredDeleteResponse, error)
	GetTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredGetParams) (*zero_trust.TunnelCloudflaredGetResponse, error)
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

// DeleteTunnel implements Client.
func (c *client) DeleteTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredDeleteParams) (*zero_trust.TunnelCloudflaredDeleteResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Delete(ctx, tunnelId, params)
}

// GetTunnel implements Client.
func (c *client) GetTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredGetParams) (*zero_trust.TunnelCloudflaredGetResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Get(ctx, tunnelId, params)
}
