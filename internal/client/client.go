package client

import (
	"context"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
)

//go:generate mockgen -destination ../testing/client.go -package testing . Client

type Client interface {
	CreateDnsRecord(ctx context.Context, params dns.RecordNewParams) (*dns.RecordResponse, error)
	CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*shared.CloudflareTunnel, error)
	DeleteDnsRecord(ctx context.Context, recordId string, params dns.RecordDeleteParams) (*dns.RecordDeleteResponse, error)
	DeleteTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredDeleteParams) (*shared.CloudflareTunnel, error)
	EditTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredEditParams) (*shared.CloudflareTunnel, error)
	GetDnsRecord(ctx context.Context, recordId string, params dns.RecordGetParams) (*dns.RecordResponse, error)
	GetTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredGetParams) (*shared.CloudflareTunnel, error)
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
func (c *client) CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*shared.CloudflareTunnel, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.New(ctx, params)
}

// DeleteDnsRecord implements Client.
func (c *client) DeleteDnsRecord(ctx context.Context, recordId string, params dns.RecordDeleteParams) (*dns.RecordDeleteResponse, error) {
	return c.DNS.Records.Delete(ctx, recordId, params)
}

// DeleteTunnel implements Client.
func (c *client) DeleteTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredDeleteParams) (*shared.CloudflareTunnel, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Delete(ctx, tunnelId, params)
}

// EditTunnel implements Client.
func (c *client) EditTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredEditParams) (*shared.CloudflareTunnel, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Edit(ctx, tunnelId, params)
}

// GetDnsRecord implements Client.
func (c *client) GetDnsRecord(ctx context.Context, recordId string, params dns.RecordGetParams) (*dns.RecordResponse, error) {
	return c.DNS.Records.Get(ctx, recordId, params)
}

// GetTunnel implements Client.
func (c *client) GetTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredGetParams) (*shared.CloudflareTunnel, error) {
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
