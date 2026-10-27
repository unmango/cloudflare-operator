package gateway

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
)

// ParametersGroup and ParametersKind are what GatewayClass.spec.parametersRef
// must name.
const (
	ParametersGroup = "cloudflare.unmango.dev"
	ParametersKind  = "CloudflareGatewayConfig"
)

// Errors returned by Resolve. Each maps to a condition reason on the class.
var (
	// ErrNoParameters reports an absent parametersRef. There is no default,
	// because a tunnel cannot be guessed.
	ErrNoParameters = errors.New("spec.parametersRef is required")

	// ErrWrongKind reports a parametersRef naming something other than a
	// CloudflareGatewayConfig.
	ErrWrongKind = errors.New("spec.parametersRef must name a " + ParametersGroup + "/" + ParametersKind)

	// ErrNoNamespace reports a parametersRef without a namespace. A GatewayClass
	// is cluster scoped, so there is nothing to default to.
	ErrNoNamespace = errors.New("spec.parametersRef.namespace is required")

	// ErrNotFound reports a parametersRef pointing at an object that is not there.
	ErrNotFound = errors.New("spec.parametersRef names an object that does not exist")

	// ErrNoTunnel reports a CloudflareGatewayConfig with neither a template nor
	// a tunnelRef. Its Gateways would be reachable only through Envoy's own
	// Service, which bypasses Cloudflare.
	ErrNoTunnel = errors.New("the CloudflareGatewayConfig sets neither template nor tunnelRef, " +
		"so no traffic would reach its Gateways through Cloudflare")
)

// Resolve reads the CloudflareGatewayConfig a GatewayClass points at.
func Resolve(ctx context.Context, reader client.Reader, class *gatewayv1.GatewayClass) (*cfv1alpha1.CloudflareGatewayConfig, error) {
	ref := class.Spec.ParametersRef
	if ref == nil {
		return nil, ErrNoParameters
	}
	if string(ref.Group) != ParametersGroup || string(ref.Kind) != ParametersKind {
		return nil, fmt.Errorf("%w, found %s/%s", ErrWrongKind, ref.Group, ref.Kind)
	}
	if ref.Namespace == nil || *ref.Namespace == "" {
		return nil, ErrNoNamespace
	}

	key := types.NamespacedName{Name: ref.Name, Namespace: string(*ref.Namespace)}
	config := &cfv1alpha1.CloudflareGatewayConfig{}
	if err := reader.Get(ctx, key, config); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
		}

		return nil, err
	}
	if config.Spec.Template == nil && config.Spec.TunnelRef == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoTunnel, key)
	}

	return config, nil
}

// WarpRouting reports whether config provisions tunnels that route WARP
// clients to Envoy, which carries listeners and routes a tunnel's public
// hostnames cannot.
func WarpRouting(config *cfv1alpha1.CloudflareGatewayConfig) bool {
	if config == nil || config.Spec.Template == nil || config.Spec.Template.Spec.Config == nil {
		return false
	}
	warp := config.Spec.Template.Spec.Config.WarpRouting

	return warp != nil && warp.Enable
}

// IsParametersError reports whether err describes a parametersRef the user has
// to fix, as opposed to a failure talking to the API server.
func IsParametersError(err error) bool {
	return errors.Is(err, ErrNoParameters) ||
		errors.Is(err, ErrWrongKind) ||
		errors.Is(err, ErrNoNamespace) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrNoTunnel)
}
