# Cloudflare Tunnel Ingress Controller

As the name suggests, this a Kubernetes Ingress Controller that uses Cloudflare Tunnel to expose services to the internet. This controller is based on the [Kubernetes Ingress Controller for Cloudflare Argo Tunnel](https://github.com/cloudflare/cloudflare-ingress-controller) and community made project [STRRL / cloudflare-tunnel-ingress-controller](https://github.com/STRRL/cloudflare-tunnel-ingress-controller).

## How it works

![How it works](assets/how-it-works.png)

1. The Ingress Controller on startup creates a new Cloudflare Tunnel or uses existing one.
2. The Ingress Controller watches for Ingress resources in the Kubernetes cluster.
3. When a new Ingress resource is created, the Ingress Controller creates a new route in the Cloudflare Tunnel and creates a DNS CNAME record pointing to the Tunnel hostname.

## Usage

### Setup

Before installing, you need a Cloudflare API token, to create one, go to [Cloudflare / Profile / API Tokens](https://dash.cloudflare.com/profile/api-tokens).

You will need to allow account-wide access to Cloudflare Tunnel and DNS:Edit for the zone you want to manage (can be multiple or all, that you have).

> [!IMPORTANT]
> Setup correct permissions for the API token:
> - Set correct account for the token, do not use option *All accounts*, unless you have to!
> - Set correct zone for the token, do not use option *All zones*, unless you have to!

![Screenshot from Cloudflare Dashboard, for options when creating new Cloudflare API Token](assets/create-cloudflare-api-token.png)

After obtaining an API token, create a Kubernetes Secret:

```shell
kubectl create secret generic --namespace cloudflare-tunnel-system cloudflare-api-token --from-literal=token=your-cloudflare-api-token
```

You will also need your Cloudflare Account ID, for DNS.

### Installation

Helm chart is stored on our company Helm repository, in the OCI format, no need to add a Helm repository.

```shell
export CLOUDFLARE_ACCOUNT_ID=your-cloudflare-account-id

helm upgrade --install \
		--namespace cloudflare-tunnel-system --create-namespace \
		cloudflare-tunnel-ingress oci://registry.clbs.io/cloudflare-tunnel-ingress-controller/cloudflare-tunnel-ingress-controller \
		--set cloudflare.apiToken.existingSecret.name=cloudflare-api-token \
    --set cloudflare.accountID=$CLOUDFLARE_ACCOUNT_ID \
    --set cloudflare.tunnelName=tunnel-ingress-demo
```

### Uninstall

```shell
helm uninstall --namespace cloudflare-tunnel-system cloudflare-tunnel-ingress
```
