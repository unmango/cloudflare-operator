package xds

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	httpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

// Cluster names in the bootstrap.
const (
	xdsClusterName   = "xds"
	adminClusterName = "admin"
	readinessName    = "readiness"
)

// Bootstrap is the Envoy bootstrap for the proxy with the given node id, as
// JSON for Envoy's --config-yaml flag. It points Envoy at the xDS server for
// everything else, so it changes only when the server moves.
func Bootstrap(nodeID, xdsHost string, xdsPort uint32) (string, error) {
	http2, err := anypb.New(&httpv3.HttpProtocolOptions{
		UpstreamProtocolOptions: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_{
			ExplicitHttpConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
					Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
				},
			},
		},
	})
	if err != nil {
		return "", err
	}

	readiness, err := readinessListener()
	if err != nil {
		return "", err
	}

	ads := &corev3.ConfigSource{
		ResourceApiVersion:    corev3.ApiVersion_V3,
		ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
	}

	bootstrap := &bootstrapv3.Bootstrap{
		Node: &corev3.Node{Id: nodeID, Cluster: nodeID},
		Admin: &bootstrapv3.Admin{
			Address: socketAddress("127.0.0.1", gateway.EnvoyAdminPort),
		},
		DynamicResources: &bootstrapv3.Bootstrap_DynamicResources{
			AdsConfig: &corev3.ApiConfigSource{
				ApiType:             corev3.ApiConfigSource_GRPC,
				TransportApiVersion: corev3.ApiVersion_V3,
				GrpcServices: []*corev3.GrpcService{{
					TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: xdsClusterName},
					},
				}},
				SetNodeOnFirstMessageOnly: true,
			},
			LdsConfig: ads,
			CdsConfig: ads,
		},
		StaticResources: &bootstrapv3.Bootstrap_StaticResources{
			Listeners: []*listenerv3.Listener{readiness},
			Clusters: []*clusterv3.Cluster{
				{
					Name:                 xdsClusterName,
					ConnectTimeout:       durationpb.New(5 * time.Second),
					ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STRICT_DNS},
					LoadAssignment:       loadAssignment(xdsClusterName, xdsHost, xdsPort),
					TypedExtensionProtocolOptions: map[string]*anypb.Any{
						"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": http2,
					},
				},
				{
					Name:                 adminClusterName,
					ConnectTimeout:       durationpb.New(time.Second),
					ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STATIC},
					LoadAssignment:       loadAssignment(adminClusterName, "127.0.0.1", gateway.EnvoyAdminPort),
				},
			},
		},
	}

	if err := bootstrap.ValidateAll(); err != nil {
		return "", fmt.Errorf("invalid bootstrap: %w", err)
	}

	b, err := protojson.Marshal(bootstrap)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// readinessListener answers GET /ready by asking the admin interface, which
// reports ready once Envoy has received its initial configuration. Exposing the
// admin listener itself would expose every admin endpoint.
func readinessListener() (*listenerv3.Listener, error) {
	router, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return nil, err
	}

	manager, err := anypb.New(&hcmv3.HttpConnectionManager{
		StatPrefix: readinessName,
		RouteSpecifier: &hcmv3.HttpConnectionManager_RouteConfig{
			RouteConfig: &routev3.RouteConfiguration{
				Name: readinessName,
				VirtualHosts: []*routev3.VirtualHost{{
					Name:    readinessName,
					Domains: []string{"*"},
					Routes: []*routev3.Route{{
						Match: &routev3.RouteMatch{
							PathSpecifier: &routev3.RouteMatch_Path{Path: "/ready"},
						},
						Action: &routev3.Route_Route{
							Route: &routev3.RouteAction{
								ClusterSpecifier: &routev3.RouteAction_Cluster{Cluster: adminClusterName},
							},
						},
					}},
				}},
			},
		},
		HttpFilters: []*hcmv3.HttpFilter{{
			Name:       "envoy.filters.http.router",
			ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: router},
		}},
	})
	if err != nil {
		return nil, err
	}

	return &listenerv3.Listener{
		Name:    readinessName,
		Address: socketAddress("0.0.0.0", gateway.EnvoyReadinessPort),
		FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{{
				Name:       "envoy.filters.network.http_connection_manager",
				ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: manager},
			}},
		}},
	}, nil
}

func socketAddress(address string, port uint32) *corev3.Address {
	return &corev3.Address{
		Address: &corev3.Address_SocketAddress{
			SocketAddress: &corev3.SocketAddress{
				Address:       address,
				PortSpecifier: &corev3.SocketAddress_PortValue{PortValue: port},
			},
		},
	}
}

func loadAssignment(cluster, address string, port uint32) *endpointv3.ClusterLoadAssignment {
	return &endpointv3.ClusterLoadAssignment{
		ClusterName: cluster,
		Endpoints: []*endpointv3.LocalityLbEndpoints{{
			LbEndpoints: []*endpointv3.LbEndpoint{{
				HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
					Endpoint: &endpointv3.Endpoint{Address: socketAddress(address, port)},
				},
			}},
		}},
	}
}
