package xds

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ComponentLabel marks the Service in front of the xDS server. The manager finds
// that Service by label rather than by name because kustomize and the Helm chart
// name it differently.
const (
	ComponentLabel = "app.kubernetes.io/component"
	ComponentValue = "xds"
)

// namespaceFile holds the namespace of the pod a service account token is
// mounted into.
const namespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// Address is where Envoy reaches the xDS server.
type Address struct {
	Host string
	Port uint32
}

func (a Address) String() string {
	return net.JoinHostPort(a.Host, strconv.FormatUint(uint64(a.Port), 10))
}

// ParseAddress parses host:port.
func ParseAddress(s string) (Address, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return Address{}, err
	}

	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return Address{}, fmt.Errorf("port %q: %w", port, err)
	}

	return Address{Host: host, Port: uint32(p)}, nil
}

// Resolver works out the xDS address Envoy is given in its bootstrap.
type Resolver struct {
	// Static, when set, is used as is.
	Static *Address

	// Reader lists Services in the manager's namespace.
	Reader client.Reader
}

// Resolve returns Static if set, and otherwise the cluster DNS name of the one
// Service in the manager's namespace that carries the xDS component label.
func (r *Resolver) Resolve(ctx context.Context) (Address, error) {
	if r.Static != nil {
		return *r.Static, nil
	}

	namespace, err := ownNamespace()
	if err != nil {
		return Address{}, err
	}

	services := &corev1.ServiceList{}
	if err := r.Reader.List(ctx, services,
		client.InNamespace(namespace),
		client.MatchingLabels{ComponentLabel: ComponentValue},
	); err != nil {
		return Address{}, fmt.Errorf("listing xDS Services: %w", err)
	}
	if len(services.Items) != 1 {
		return Address{}, fmt.Errorf("found %d Services labelled %s=%s in namespace %s, expected exactly one",
			len(services.Items), ComponentLabel, ComponentValue, namespace)
	}

	svc := services.Items[0]
	if len(svc.Spec.Ports) == 0 {
		return Address{}, fmt.Errorf("xDS Service %s/%s exposes no ports", svc.Namespace, svc.Name)
	}

	return Address{
		Host: svc.Name + "." + svc.Namespace + ".svc",
		Port: uint32(svc.Spec.Ports[0].Port),
	}, nil
}

// ownNamespace is the namespace the manager runs in.
func ownNamespace() (string, error) {
	if ns, ok := os.LookupEnv("POD_NAMESPACE"); ok && ns != "" {
		return ns, nil
	}

	b, err := os.ReadFile(namespaceFile)
	if err != nil {
		return "", fmt.Errorf("reading the manager's namespace: %w", err)
	}

	return strings.TrimSpace(string(b)), nil
}
