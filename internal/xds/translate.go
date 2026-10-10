package xds

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

// Snapshot translates the valid listeners of a Gateway into the configuration
// its Envoy serves. The version is a hash of the content, so translating an
// unchanged Gateway again yields a snapshot Envoy already has.
func Snapshot(listeners []gateway.Listener) (*cachev3.Snapshot, error) {
	resources, err := translate(listeners)
	if err != nil {
		return nil, err
	}

	version, err := hash(resources)
	if err != nil {
		return nil, err
	}

	snapshot, err := cachev3.NewSnapshot(version, resources)
	if err != nil {
		return nil, err
	}
	if err := snapshot.Consistent(); err != nil {
		return nil, fmt.Errorf("inconsistent snapshot: %w", err)
	}

	return snapshot, nil
}

// translate groups the valid HTTP listeners by port. Each port becomes one Envoy
// listener with one route configuration, and each Gateway listener on that port
// becomes a virtual host in it, matched on the listener's hostname.
func translate(listeners []gateway.Listener) (map[resourcev3.Type][]types.Resource, error) {
	byPort := map[gatewayv1.PortNumber][]gateway.Listener{}
	for _, l := range listeners {
		if !l.Valid || l.Protocol != gatewayv1.HTTPProtocolType {
			continue
		}
		byPort[l.Port] = append(byPort[l.Port], l)
	}

	ports := make([]gatewayv1.PortNumber, 0, len(byPort))
	for port := range byPort {
		ports = append(ports, port)
	}
	slices.Sort(ports)

	resources := map[resourcev3.Type][]types.Resource{
		resourcev3.ListenerType: {},
		resourcev3.RouteType:    {},
	}
	for _, port := range ports {
		name := fmt.Sprintf("http-%d", port)

		listener, err := httpListener(name, port)
		if err != nil {
			return nil, err
		}

		resources[resourcev3.ListenerType] = append(resources[resourcev3.ListenerType], listener)
		resources[resourcev3.RouteType] = append(resources[resourcev3.RouteType], routeConfiguration(name, byPort[port]))
	}

	return resources, nil
}

func httpListener(name string, port gatewayv1.PortNumber) (*listenerv3.Listener, error) {
	router, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return nil, err
	}

	manager, err := anypb.New(&hcmv3.HttpConnectionManager{
		StatPrefix: name,
		RouteSpecifier: &hcmv3.HttpConnectionManager_Rds{
			Rds: &hcmv3.Rds{
				RouteConfigName: name,
				ConfigSource: &corev3.ConfigSource{
					ResourceApiVersion:    corev3.ApiVersion_V3,
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
				},
			},
		},
		// Listener hostnames never carry a port, and a client may send one.
		StripPortMode: &hcmv3.HttpConnectionManager_StripAnyHostPort{StripAnyHostPort: true},
		HttpFilters: []*hcmv3.HttpFilter{{
			Name:       "envoy.filters.http.router",
			ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: router},
		}},
	})
	if err != nil {
		return nil, err
	}

	return &listenerv3.Listener{
		Name: name,
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address:       "0.0.0.0",
					PortSpecifier: &corev3.SocketAddress_PortValue{PortValue: uint32(gateway.ContainerPort(port))},
				},
			},
		},
		FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{{
				Name:       "envoy.filters.network.http_connection_manager",
				ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: manager},
			}},
		}},
	}, nil
}

// routeConfiguration holds one virtual host per Gateway listener on a port.
// Routes attach to them in a later pass; until then every request gets a 404.
func routeConfiguration(name string, listeners []gateway.Listener) *routev3.RouteConfiguration {
	config := &routev3.RouteConfiguration{Name: name}
	for _, l := range listeners {
		domain := "*"
		if l.Hostname != nil {
			domain = string(*l.Hostname)
		}

		config.VirtualHosts = append(config.VirtualHosts, &routev3.VirtualHost{
			Name:    string(l.Name),
			Domains: []string{domain},
		})
	}

	return config
}

// hash fingerprints a set of resources in a stable order.
func hash(resources map[resourcev3.Type][]types.Resource) (string, error) {
	typeURLs := make([]string, 0, len(resources))
	for typeURL := range resources {
		typeURLs = append(typeURLs, typeURL)
	}
	slices.Sort(typeURLs)

	h := sha256.New()
	opts := proto.MarshalOptions{Deterministic: true}
	for _, typeURL := range typeURLs {
		h.Write([]byte(typeURL))
		for _, r := range resources[typeURL] {
			b, err := opts.Marshal(r)
			if err != nil {
				return "", err
			}
			h.Write(b)
		}
	}

	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
