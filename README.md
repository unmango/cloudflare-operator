<!-- markdownlint-disable MD033 -->

# Cloudflare Operator

A kubernetes operator for all things Cloudflare!

## Description

*First, take a look at the [disclaimer](#disclaimer).*

Manage your [Cloudflare](https://cloudflare.com) infrastructure using the [operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

- [Cloudflare Operator](#cloudflare-operator)
  - [Description](#description)
    - [Features](#features)
  - [Installation](#installation)
    - [Manifest](#manifest)
    - [Kustomize](#kustomize)
    - [Helm](#helm)
  - [Resources](#resources)
    - [Cloudflared](#cloudflared)
    - [CloudflareTunnel](#cloudflaretunnel)
    - [DnsRecord](#dnsrecord)
  - [Getting Started](#getting-started)
    - [Prerequisites](#prerequisites)
    - [To Deploy on the cluster](#to-deploy-on-the-cluster)
    - [To Uninstall](#to-uninstall)
  - [Contributing](#contributing)
  - [Disclaimer](#disclaimer)
    - [Rationale](#rationale)
  - [License](#license)

### Features

- Ingress controller for [Cloudflare Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)
- [cloudflared](https://github.com/cloudflare/cloudflared) deployment
- Managed [DNS records](https://developers.cloudflare.com/api/resources/dns/subresources/records/)

## Installation

A few options are available for installation:

- [Manifest](#manifest)
- [Kustomize](#kustomize)
- [Helm](#helm)

The controller pod needs to be configured with a `CLOUDFLARE_API_TOKEN` in order to perform any actions that involve sending requests to the cloudflare API.

The operator can be run without providing this token but will only be able to manage resources within the cluster.
Resources that support running wihout a token include:

- `Cloudflared`

### Manifest

Use `kubectl apply` to apply the installation manifest:

```sh
kubectl apply -f https://raw.githubusercontent.com/unmango/cloudflare-operator/main/dist/install.yaml
```

This will install from the `main` branch. Replace `main` in the link above to use a specific tag or branch.

### Kustomize

```shell
kubectl apply --server-side --kustomize='https://github.com/unmango/cloudflare-operator.git/config/default?ref=main&submodules=false'
```

Replace `ref=main` with the branch or tag to use, more info [in the kustomize docs](https://github.com/kubernetes-sigs/kustomize/blob/master/examples/remoteBuild.md).

**Note** the `submodules=false` in the above URL, this prevents kustomize from cloning `./upstream` which is a *very* hefty reference to <https://github.com/cloudflare/api-schemas.git>.

### Helm

The helm chart is not currently hosted on any repositories.
To use it, you'll need to clone this repository and install from there.

```shell
git clone https://github.com/unmango/cloudflare-operator
```

Once cloned you can use:

```shell
helm install my-release ./dist/chart --create-namespace --namespace cloudflare-operator-system
```

## Resources

- [Cloudflared](./config/crd/bases/cloudflare.unmango.dev_cloudflareds.yaml)
- [CloudflareTunnel](./config/crd/bases/cloudflare.unmango.dev_cloudflaretunnels.yaml)
- [DnsRecord](./config/crd/bases/cloudflare.unmango.dev_dnsrecords.yaml)

### Cloudflared

With no `spec`, the cloudflared resource will create a DaemonSet running the `--hello-world` tunnel.

```yaml
apiVersion: cloudflare.unmango.dev/v1alpha1
kind: Cloudflared
metadata:
  name: cloudflared-sample
```

### CloudflareTunnel

A minimal manifest will create a remote-managed tunnel with the same name as the resource.

**Note** the tunnel will only be created in the Cloudflare API,
No other resources (i.e. `cloudflared`) will be created to run the tunnel unless explicitly specified.

```yaml
apiVersion: cloudflare.unmango.dev/v1alpha1
kind: CloudflareTunnel
metadata:
  name: cloudflaretunnel-sample
spec:
  accountId: <cloudflare account-id>
  configSource: cloudflare
```

### DnsRecord

```yaml
apiVersion: cloudflare.unmango.dev/v1alpha1
kind: DnsRecord
metadata:
  name: dnsrecord-sample
spec:
  zoneId: $CLOUDFLARE_ZONE_ID
  record:
    aRecord:
      name: cloudflare-operator-sample
      content: 1.1.1.1
```

## Getting Started

### Prerequisites

- go version v1.24.2+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/cloudflare-operator:tag
```

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/cloudflare-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Contributing

// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Disclaimer

<h1 align="center">🚧 Warning! 🚧</h1>

Hi, thank you for coming to my repo!
You probably meant to go to one of these projects instead:

| Project | Description |
|:--------|:------------|
|[adyanth/cloudflare-operator](https://github.com/adyanth/cloudflare-operator)|A Kubernetes Operator to create and manage Cloudflare Tunnels and DNS records for (HTTP/TCP/UDP*) Service Resources|
|[STRRL/cloudflare-tunnel-ingress-controller](https://github.com/STRRL/cloudflare-tunnel-ingress-controller/)|The Kuberntes Ingress Controller based on Cloudflare Tunnel.|

Both projects are much more mature and can probably do what you're looking for!

### Rationale

*There are two great projects bringing cloudflare to kubernetes, what is this project trying to add?*

My primary motivation is to gain experience writing kubernetes operators.
Additionally, I love parts of both projects above and I wanted to take my favorite bits and put them in one place.

*Will anything be upstreamed?*

Cloudflare has developed a strong foundational platform which our projects have all been able to build on top of.
This allows for a clean separation of concerns where Cloudflare provides the core functionality and the operators focus on Kubernetes integration.
Currently, each project takes a very different approach to the resource architecture on Kubernetes which reduces the overlap in functionality.

Where applicable, I will happily contribute any changes that bring shared benefit!

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  <http://www.apache.org/licenses/LICENSE-2.0>

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
