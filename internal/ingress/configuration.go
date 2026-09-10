package ingress

const (
	DefaultClassName = "cloudflare"
	ControllerName   = "cloudflare.unmango.dev/ingress-controller"
)

type Configuration[T any] struct {
	AccountId    T
	ConfigSource T
	Cloudflared  T
	Name         T
	TunnelSecret T
}
