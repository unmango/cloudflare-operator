package client

import (
	"context"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/cloudflare/cloudflare-go/v7/zones"
)

//go:generate mockgen -destination ../testing/client.go -package testing . Client

type Client interface {
	CreateDnsRecord(ctx context.Context, params dns.RecordNewParams) (*dns.RecordResponse, error)
	CreateRoute(ctx context.Context, params zero_trust.NetworkRouteNewParams) (*zero_trust.Route, error)
	CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*shared.CloudflareTunnel, error)
	DeleteDnsRecord(ctx context.Context, recordId string, params dns.RecordDeleteParams) (*dns.RecordDeleteResponse, error)
	DeleteRoute(ctx context.Context, routeId string, params zero_trust.NetworkRouteDeleteParams) (*zero_trust.Route, error)
	DeleteTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredDeleteParams) (*shared.CloudflareTunnel, error)
	EditTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredEditParams) (*shared.CloudflareTunnel, error)
	GetDnsRecord(ctx context.Context, recordId string, params dns.RecordGetParams) (*dns.RecordResponse, error)
	GetTunnel(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredGetParams) (*shared.CloudflareTunnel, error)
	GetTunnelToken(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredTokenGetParams) (*string, error)
	GetZone(ctx context.Context, params zones.ZoneGetParams) (*zones.Zone, error)
	ListDnsRecords(ctx context.Context, params dns.RecordListParams) ([]dns.RecordResponse, error)
	ListRoutes(ctx context.Context, params zero_trust.NetworkRouteListParams) ([]zero_trust.Teamnet, error)
	ListTunnels(ctx context.Context, params zero_trust.TunnelCloudflaredListParams) ([]shared.CloudflareTunnel, error)
	ListVirtualNetworks(ctx context.Context, params zero_trust.NetworkVirtualNetworkListParams) ([]zero_trust.VirtualNetwork, error)
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

// CreateRoute implements Client.
func (c *client) CreateRoute(ctx context.Context, params zero_trust.NetworkRouteNewParams) (*zero_trust.Route, error) {
	return c.ZeroTrust.Networks.Routes.New(ctx, params)
}

// CreateTunnel implements Client.
func (c *client) CreateTunnel(ctx context.Context, params zero_trust.TunnelCloudflaredNewParams) (*shared.CloudflareTunnel, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.New(ctx, params)
}

// DeleteDnsRecord implements Client.
func (c *client) DeleteDnsRecord(ctx context.Context, recordId string, params dns.RecordDeleteParams) (*dns.RecordDeleteResponse, error) {
	return c.DNS.Records.Delete(ctx, recordId, params)
}

// DeleteRoute implements Client.
func (c *client) DeleteRoute(ctx context.Context, routeId string, params zero_trust.NetworkRouteDeleteParams) (*zero_trust.Route, error) {
	return c.ZeroTrust.Networks.Routes.Delete(ctx, routeId, params)
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

// GetZone implements Client.
func (c *client) GetZone(ctx context.Context, params zones.ZoneGetParams) (*zones.Zone, error) {
	return c.Zones.Get(ctx, params)
}

// ListDnsRecords implements Client.
func (c *client) ListDnsRecords(ctx context.Context, params dns.RecordListParams) ([]dns.RecordResponse, error) {
	var records []dns.RecordResponse
	iter := c.DNS.Records.ListAutoPaging(ctx, params)
	for iter.Next() {
		records = append(records, iter.Current())
	}

	return records, iter.Err()
}

// ListRoutes implements Client.
func (c *client) ListRoutes(ctx context.Context, params zero_trust.NetworkRouteListParams) ([]zero_trust.Teamnet, error) {
	var routes []zero_trust.Teamnet
	iter := c.ZeroTrust.Networks.Routes.ListAutoPaging(ctx, params)
	for iter.Next() {
		routes = append(routes, iter.Current())
	}

	return routes, iter.Err()
}

// ListTunnels implements Client.
func (c *client) ListTunnels(ctx context.Context, params zero_trust.TunnelCloudflaredListParams) ([]shared.CloudflareTunnel, error) {
	var tunnels []shared.CloudflareTunnel
	iter := c.ZeroTrust.Tunnels.Cloudflared.ListAutoPaging(ctx, params)
	for iter.Next() {
		tunnels = append(tunnels, iter.Current())
	}

	return tunnels, iter.Err()
}

// ListVirtualNetworks implements Client.
func (c *client) ListVirtualNetworks(ctx context.Context, params zero_trust.NetworkVirtualNetworkListParams) ([]zero_trust.VirtualNetwork, error) {
	var networks []zero_trust.VirtualNetwork
	iter := c.ZeroTrust.Networks.VirtualNetworks.ListAutoPaging(ctx, params)
	for iter.Next() {
		networks = append(networks, iter.Current())
	}

	return networks, iter.Err()
}

// UpdateConfiguration implements Client.
func (c *client) UpdateConfiguration(ctx context.Context, tunnelId string, params zero_trust.TunnelCloudflaredConfigurationUpdateParams) (*zero_trust.TunnelCloudflaredConfigurationUpdateResponse, error) {
	return c.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, tunnelId, params)
}

// UpdateDnsRecord implements Client.
func (c *client) UpdateDnsRecord(ctx context.Context, recordId string, params dns.RecordUpdateParams) (*dns.RecordResponse, error) {
	return c.DNS.Records.Update(ctx, recordId, params)
}
