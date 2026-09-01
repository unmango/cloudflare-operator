package annotation

import (
	"github.com/unmango/cloudflare-operator/internal/annotation"
	"github.com/unmango/cloudflare-operator/internal/ingress"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type (
	Configuration ingress.Configuration[annotation.Annotation]
	Values        ingress.Configuration[annotation.Value]
)

var Prefix = annotation.Prefix("ingress.cloudflare.unmango.dev/")

var Definitions = Configuration{
	AccountId:    Prefix.Annotation("accountId"),
	ConfigSource: Prefix.Annotation("configSource"),
	Cloudflared:  Prefix.Annotation("cloudflared"),
	Name:         Prefix.Annotation("name"),
	TunnelSecret: Prefix.Annotation("tunnelSecret"),
}

func Parse(obj client.Object) (a Values) {
	a.AccountId = Definitions.AccountId.Get(obj)
	a.Cloudflared = Definitions.Cloudflared.Get(obj)
	a.ConfigSource = Definitions.ConfigSource.Get(obj)
	a.Name = Definitions.Name.Get(obj)
	a.TunnelSecret = Definitions.TunnelSecret.Get(obj)

	return a
}

// Re-export for convenience
var (
	Lookup     = annotation.Lookup
	Kubernetes = annotation.Kubernetes
)
