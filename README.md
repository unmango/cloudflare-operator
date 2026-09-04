# cloudflare-operator

A Kubernetes operator for Cloudflare.

It manages Cloudflare tunnels, the `cloudflared` daemons that run them, and DNS records, as Kubernetes resources.
An Ingress controller creates a tunnel for any Ingress in the `cloudflare` class.

> This project is `v1alpha1` and the API may change.

## Related projects

You probably want one of these instead:

| Project | Description |
| --- | --- |
| [adyanth/cloudflare-operator](https://github.com/adyanth/cloudflare-operator) | Creates and manages Cloudflare Tunnels and DNS records for HTTP/TCP/UDP Service resources |
| [STRRL/cloudflare-tunnel-ingress-controller](https://github.com/STRRL/cloudflare-tunnel-ingress-controller) | A Kubernetes Ingress controller built on Cloudflare Tunnel |

Both are more mature and cover most of what people come here looking for.

This operator exists because I wanted the experience of writing one, and because I wanted my favourite parts of both projects in a single place.

The three model the problem differently.
adyanth's `Tunnel` and `ClusterTunnel` bundle the tunnel with the `cloudflared` Deployment that serves it, and a `TunnelBinding` attaches Services to a tunnel and creates their DNS records.
STRRL's controller has no CRDs at all: it owns one tunnel for the cluster and derives its configuration from `Ingress` objects.
Here every Cloudflare object gets its own resource.
A `CloudflareTunnel`, the `Cloudflared` that runs it, and a `DnsRecord` are each reconciled on their own and refer to one another by name.

## Resources

All resources live in the `cloudflare.unmango.dev/v1alpha1` API group.

| Kind | What it does |
| --- | --- |
| `CloudflareTunnel` | Creates and reconciles a tunnel through the Cloudflare API, and can create the `Cloudflared` that runs it |
| `Cloudflared` | Runs the `cloudflared` daemon for a tunnel as a DaemonSet or a Deployment |
| `DnsRecord` | Manages a single DNS record in a zone |

The Ingress controller watches core `Ingress` objects and creates a `CloudflareTunnel` for any whose `ingressClassName` is `cloudflare`.
It reads its configuration from `ingress.cloudflare.unmango.dev/` annotations.

## Installing

Install the CRDs and deploy the manager:

```sh
make install
make deploy IMG=<registry>/cloudflare-operator:<tag>
```

The manager reads its Cloudflare API token from the `CLOUDFLARE_API_TOKEN` environment variable.
`config/manager/manager.yaml` does not set it, so supply it yourself, for example from a Secret:

```sh
kubectl create secret generic cloudflare-credentials \
  --namespace cloudflare-operator-system \
  --from-literal=CLOUDFLARE_API_TOKEN=<token>

kubectl set env deployment/cloudflare-operator-controller-manager \
  --namespace cloudflare-operator-system \
  --from=secret/cloudflare-credentials
```

Remove everything again:

```sh
make undeploy
make uninstall
```

## Example

```yaml
apiVersion: cloudflare.unmango.dev/v1alpha1
kind: CloudflareTunnel
metadata:
  name: example
spec:
  accountId: <cloudflare-account-id>
  configSource: cloudflare
---
apiVersion: cloudflare.unmango.dev/v1alpha1
kind: Cloudflared
metadata:
  name: example
spec:
  config:
    tunnelRef:
      name: example
```

More examples are in [`config/samples`](config/samples).

## Development

The repository is Nix first.
The dev shell supplies Go, `controller-gen`, `kustomize`, `golangci-lint`, `kubebuilder`, `kind` and the envtest binaries, so nothing is downloaded at build or test time.

```sh
direnv allow     # or prefix commands with `nix develop -c`
make test        # unit and envtest suites
make lint
make build       # nix build .#
make check       # nix flake check
```

The container image is built by Nix rather than a Dockerfile:

```sh
nix build .#image   # produces a script that streams the image to stdout
make kind-load      # stream it straight into a kind cluster
```

See [AGENTS.md](AGENTS.md) for the architecture and the reasoning behind the less obvious choices.

## License

Apache 2.0. See [LICENSE](LICENSE).
