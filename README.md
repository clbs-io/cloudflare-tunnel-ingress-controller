# Cloudflare Tunnel Ingress Controller

As the name suggests, this is a Kubernetes Ingress Controller that uses Cloudflare Tunnel to expose services to the Internet. This controller is based on the [Kubernetes Ingress Controller for Cloudflare Argo Tunnel](https://github.com/cloudflare/cloudflare-ingress-controller) and the community made project [STRRL / cloudflare-tunnel-ingress-controller](https://github.com/STRRL/cloudflare-tunnel-ingress-controller).

## How it works

![How it works](assets/how-it-works.png)

1. The Ingress Controller creates a new Cloudflare Tunnel on startup or uses an existing one.
2. The Ingress Controller watches for Ingress resources in the Kubernetes cluster.
3. When a new Ingress resource is created, the Ingress Controller creates a new route in the Cloudflare Tunnel and creates a DNS CNAME record pointing to the Tunnel hostname.

## Usage

### Setup

Before installing, you need a Cloudflare API token. To create a token, go to [Cloudflare / Profile / API Tokens](https://dash.cloudflare.com/profile/api-tokens).

You must allow account-wide access to Cloudflare Tunnel and DNS:Edit for the zones that you want to manage (can be multiple or all that you have).

> [!IMPORTANT]
> Set up correct permissions for the API token:
> - Set a correct account for the token. Do not use the option *All accounts*, unless you have to!
> - Set a correct zone for the token. Do not use the option *All zones*, unless you have to!

![Screenshot from Cloudflare Dashboard, for options when creating new Cloudflare API Token](assets/create-cloudflare-api-token.png)

After obtaining an API token, create a Kubernetes Secret:

```shell
kubectl create secret generic --namespace cloudflare-tunnel-system cloudflare-api-token --from-literal=token=your-cloudflare-api-token
```

You will also need your Cloudflare Account ID, for DNS.

### Installation

The Helm chart is stored in the OCI format in our company Helm repository, so there is no need to add another Helm repository.

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
