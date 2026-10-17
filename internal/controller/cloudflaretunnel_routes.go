package controller

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	cfclient "github.com/unmango/cloudflare-operator/internal/client"
)

// tunnelRoutes is what reconcileRoutes found: how many of spec.routes
// Cloudflare holds, and why any others are missing.
type tunnelRoutes struct {
	ready    int32
	problems []string
}

func (t tunnelRoutes) problem() string {
	return strings.Join(t.problems, "; ")
}

// reconcileRoutes converges the private network routes Cloudflare holds for the
// tunnel on spec.routes. A route that points at the tunnel and that spec.routes
// omits is deleted, whoever created it. A route the API refuses, such as a
// network another tunnel already routes, is reported rather than retried, since
// asking again unchanged gets the same answer.
func (r *CloudflareTunnelReconciler) reconcileRoutes(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel, tunnelId string) (tunnelRoutes, error) {
	log := logf.FromContext(ctx)
	result := tunnelRoutes{}

	var desired []cfv1alpha1.CloudflareTunnelRoute
	for _, route := range tunnel.Spec.Routes {
		prefix, err := netip.ParsePrefix(route.Network)
		if err != nil {
			result.problems = append(result.problems, fmt.Sprintf("spec.routes network %q is not a CIDR", route.Network))
			continue
		}
		route.Network = prefix.Masked().String()
		desired = append(desired, route)
	}

	existing, err := r.listRoutes(ctx, tunnel, tunnelId)
	if err != nil {
		return result, fmt.Errorf("listing routes: %w", err)
	}

	matched := make([]bool, len(desired))
	for _, route := range existing {
		i := matchRoute(desired, matched, route)
		if i >= 0 {
			matched[i] = true
			result.ready++
			continue
		}

		log.Info("Deleting a route the tunnel no longer lists", "network", route.Network, "id", route.ID)
		_, err := r.Cloudflare.DeleteRoute(ctx, route.ID, zero_trust.NetworkRouteDeleteParams{
			AccountID: cloudflare.F(routeAccount(tunnel)),
		})
		if err := cfclient.IgnoreNotFound(err); err != nil {
			return result, fmt.Errorf("deleting route %s: %w", route.Network, err)
		}
	}

	for i, route := range desired {
		if matched[i] {
			continue
		}

		params := zero_trust.NetworkRouteNewParams{
			AccountID: cloudflare.F(routeAccount(tunnel)),
			Network:   cloudflare.F(route.Network),
			TunnelID:  cloudflare.F(tunnelId),
		}
		if route.Comment != "" {
			params.Comment = cloudflare.F(route.Comment)
		}
		if route.VirtualNetworkId != "" {
			params.VirtualNetworkID = cloudflare.F(route.VirtualNetworkId)
		}

		log.Info("Creating route", "network", route.Network)
		_, err := r.Cloudflare.CreateRoute(ctx, params)
		switch {
		case cfclient.IsClientError(err):
			result.problems = append(result.problems, fmt.Sprintf("route %s was refused: %s", route.Network, err))
		case err != nil:
			return result, fmt.Errorf("creating route %s: %w", route.Network, err)
		default:
			result.ready++
		}
	}

	return result, nil
}

// deleteRoutes deletes every route that points at the tunnel, so none outlives
// it.
func (r *CloudflareTunnelReconciler) deleteRoutes(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel, tunnelId string) error {
	existing, err := r.listRoutes(ctx, tunnel, tunnelId)
	if err != nil {
		return fmt.Errorf("listing routes: %w", err)
	}

	for _, route := range existing {
		_, err := r.Cloudflare.DeleteRoute(ctx, route.ID, zero_trust.NetworkRouteDeleteParams{
			AccountID: cloudflare.F(routeAccount(tunnel)),
		})
		if err := cfclient.IgnoreNotFound(err); err != nil {
			return fmt.Errorf("deleting route %s: %w", route.Network, err)
		}
	}

	return nil
}

// routeAccount is the account the tunnel's routes live in: the one Cloudflare
// reported for the tunnel, or the spec's before it has reported one.
func routeAccount(tunnel *cfv1alpha1.CloudflareTunnel) string {
	if tunnel.Status.AccountTag != "" {
		return tunnel.Status.AccountTag
	}

	return tunnel.Spec.AccountId
}

func (r *CloudflareTunnelReconciler) listRoutes(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel, tunnelId string) ([]zero_trust.Teamnet, error) {
	return r.Cloudflare.ListRoutes(ctx, zero_trust.NetworkRouteListParams{
		AccountID: cloudflare.F(routeAccount(tunnel)),
		TunnelID:  cloudflare.F(tunnelId),
		IsDeleted: cloudflare.F(false),
	})
}

// matchRoute finds the unmatched entry of desired that route satisfies. An
// entry without a virtual network matches any, since Cloudflare files it under
// the account's default.
func matchRoute(desired []cfv1alpha1.CloudflareTunnelRoute, matched []bool, route zero_trust.Teamnet) int {
	network := route.Network
	if prefix, err := netip.ParsePrefix(network); err == nil {
		network = prefix.Masked().String()
	}

	for i, d := range desired {
		if matched[i] || d.Network != network {
			continue
		}
		if d.VirtualNetworkId == "" || d.VirtualNetworkId == route.VirtualNetworkID {
			return i
		}
	}

	return -1
}
