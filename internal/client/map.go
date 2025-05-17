package client

import (
	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
)

type (
	CloudflareTunnelConfig              cfv1alpha1.CloudflareTunnelConfig
	CloudflareTunnelConfigIngress       cfv1alpha1.CloudflareTunnelConfigIngress
	CloudflareTunnelOriginRequest       cfv1alpha1.CloudflareTunnelOriginRequest
	CloudflareTunnelOriginRequestAccess cfv1alpha1.CloudflareTunnelOriginRequestAccess
)

func (c CloudflareTunnelConfig) UpdateParams() zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig {
	return zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
		Ingress:       cloudflare.F(c.ingressUpdate()),
		OriginRequest: cloudflare.F(c.originRequest().update()),
	}
}

func (c CloudflareTunnelConfig) ingressUpdate() (params []zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress) {
	for _, x := range c.Ingress {
		i := CloudflareTunnelConfigIngress(x)
		params = append(params, i.update())
	}

	return params
}

func (c CloudflareTunnelConfig) originRequest() CloudflareTunnelOriginRequest {
	return CloudflareTunnelOriginRequest(c.OriginRequest)
}

func (c CloudflareTunnelConfigIngress) update() zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress {
	return zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
		Hostname:      cloudflare.F(c.Hostname),
		Service:       cloudflare.F(c.Service),
		OriginRequest: cloudflare.F(c.originRequest().ingressUpdate()),
		Path:          cloudflare.F(c.Path),
	}
}

func (c CloudflareTunnelConfigIngress) originRequest() CloudflareTunnelOriginRequest {
	return CloudflareTunnelOriginRequest(c.OriginRequest)
}

func (c CloudflareTunnelOriginRequest) update() zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequest {
	return zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequest{
		Access:                 cloudflare.F(c.access().update()),
		CAPool:                 cloudflare.F(c.CaPool),
		ConnectTimeout:         cloudflare.F(c.ConnectTimeout),
		DisableChunkedEncoding: cloudflare.F(c.DisableChunkedEncoding),
		HTTP2Origin:            cloudflare.F(c.Http2Origin),
		HTTPHostHeader:         cloudflare.F(c.HttpHostHeader),
		KeepAliveConnections:   cloudflare.F(c.KeepAliveConnections),
		KeepAliveTimeout:       cloudflare.F(c.KeepAliveTimeout),
		NoHappyEyeballs:        cloudflare.F(c.NoHappyEyeballs),
		NoTLSVerify:            cloudflare.F(c.NoTlsVerify),
		OriginServerName:       cloudflare.F(c.OriginServerName),
		ProxyType:              cloudflare.F(c.ProxyType),
		TCPKeepAlive:           cloudflare.F(c.TcpKeepAlive),
		TLSTimeout:             cloudflare.F(c.TlsTimeout),
	}
}

func (c CloudflareTunnelOriginRequest) ingressUpdate() zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequest {
	return zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequest{
		Access:                 cloudflare.F(c.access().ingressUpdate()),
		CAPool:                 cloudflare.F(c.CaPool),
		ConnectTimeout:         cloudflare.F(c.ConnectTimeout),
		DisableChunkedEncoding: cloudflare.F(c.DisableChunkedEncoding),
		HTTP2Origin:            cloudflare.F(c.Http2Origin),
		HTTPHostHeader:         cloudflare.F(c.HttpHostHeader),
		KeepAliveConnections:   cloudflare.F(c.KeepAliveConnections),
		KeepAliveTimeout:       cloudflare.F(c.KeepAliveTimeout),
		NoHappyEyeballs:        cloudflare.F(c.NoHappyEyeballs),
		NoTLSVerify:            cloudflare.F(c.NoTlsVerify),
		OriginServerName:       cloudflare.F(c.OriginServerName),
		ProxyType:              cloudflare.F(c.ProxyType),
		TCPKeepAlive:           cloudflare.F(c.TcpKeepAlive),
		TLSTimeout:             cloudflare.F(c.TlsTimeout),
	}
}

func (c CloudflareTunnelOriginRequest) access() CloudflareTunnelOriginRequestAccess {
	return CloudflareTunnelOriginRequestAccess(c.Access)
}

func (c CloudflareTunnelOriginRequestAccess) update() zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequestAccess {
	return zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequestAccess{
		AUDTag:   cloudflare.F(c.AudTag),
		TeamName: cloudflare.F(c.TeamName),
		Required: cloudflare.F(c.Required),
	}
}

func (c CloudflareTunnelOriginRequestAccess) ingressUpdate() zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequestAccess {
	return zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequestAccess{
		AUDTag:   cloudflare.F(c.AudTag),
		TeamName: cloudflare.F(c.TeamName),
		Required: cloudflare.F(c.Required),
	}
}
