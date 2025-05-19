package client

import (
	"context"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
)

//go:generate go tool mockgen -destination ../testing/client.go -package testing . Client

type Client interface {
	CreateDnsRecord(ctx context.Context, params dns.RecordNewParams) (*dns.RecordResponse, error)
	CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*zero_trust.TunnelCloudflaredNewResponse, error)
	DeleteDnsRecord(ctx context.Context, recordId string, params dns.RecordDeleteParams) (*dns.RecordDeleteResponse, error)
	DeleteTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredDeleteParams) (*zero_trust.TunnelCloudflaredDeleteResponse, error)
	EditTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredEditParams) (*zero_trust.TunnelCloudflaredEditResponse, error)
	GetDnsRecord(ctx context.Context, recordId string, params dns.RecordGetParams) (*dns.RecordResponse, error)
	GetTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredGetParams) (*zero_trust.TunnelCloudflaredGetResponse, error)
	GetTunnelToken(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredTokenGetParams) (*string, error)
	UpdateConfiguration(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredConfigurationUpdateParams) (*zero_trust.TunnelCloudflaredConfigurationUpdateResponse, error)
	UpdateDnsRecord(ctx context.Context, recordId string, params dns.RecordUpdateParams) (*dns.RecordResponse, error)
}

type client struct {
	*cloudflare.Client
}

func New() Client {
	return &client{cloudflare.NewClient()}
}

// CreateDnsRecord implements Client.
func (c *client) CreateDnsRecord(ctx context.Context, params dns.RecordNewParams) (*dns.RecordResponse, error) {
	return c.DNS.Records.New(ctx, params)
}

// CreateTunnel implements Client.
func (c *client) CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*zero_trust.TunnelCloudflaredNewResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.New(ctx, params)
}

// DeleteDnsRecord implements Client.
func (c *client) DeleteDnsRecord(ctx context.Context, recordId string, params dns.RecordDeleteParams) (*dns.RecordDeleteResponse, error) {
	return c.DNS.Records.Delete(ctx, recordId, params)
}

// DeleteTunnel implements Client.
func (c *client) DeleteTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredDeleteParams) (*zero_trust.TunnelCloudflaredDeleteResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Delete(ctx, tunnelId, params)
}

// EditTunnel implements Client.
func (c *client) EditTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredEditParams) (*zero_trust.TunnelCloudflaredEditResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Edit(ctx, tunnelId, params)
}

// GetDnsRecord implements Client.
func (c *client) GetDnsRecord(ctx context.Context, recordId string, params dns.RecordGetParams) (*dns.RecordResponse, error) {
	return c.DNS.Records.Get(ctx, recordId, params)
}

// GetTunnel implements Client.
func (c *client) GetTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredGetParams) (*zero_trust.TunnelCloudflaredGetResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Get(ctx, tunnelId, params)
}

// GetTunnelToken implements Client.
func (c *client) GetTunnelToken(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredTokenGetParams) (*string, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Token.Get(ctx, tunnelId, params)
}

// UpdateConfiguration implements Client.
func (c *client) UpdateConfiguration(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredConfigurationUpdateParams) (*zero_trust.TunnelCloudflaredConfigurationUpdateResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, tunnelId, params)
}

// UpdateDnsRecord implements Client.
func (c *client) UpdateDnsRecord(ctx context.Context, recordId string, params dns.RecordUpdateParams) (*dns.RecordResponse, error) {
	return c.DNS.Records.Update(ctx, recordId, params)
}
