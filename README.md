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

You must allow `Account:Cloudflare Tunnel:Edit` and `Zone:DNS:Edit` for the zones that you want to manage (can be multiple or all that you have).

> [!IMPORTANT]
> Set up correct permissions for the API token:
>
> - Set a correct account for the token. Do not use the option *All accounts*, unless you have to!
> - Set a correct zone for the token. Do not use the option *All zones*, unless you have to!

When creating a new API token, your screen should look like this:

![Screenshot from Cloudflare Dashboard, for options when creating new Cloudflare API Token](assets/create-cloudflare-api-token.png)

After obtaining an API token, create a Kubernetes Secret:

- Create secret with shell command:

  ```shell
  kubectl create secret generic --namespace cloudflare-tunnel-system cloudflare-api-token --from-literal=token=<your-cloudflare-api-token>
  ```

- Or create it from a YAML manifest:

  ```yaml
  # cloudflare-api-token.yaml
  apiVersion: v1
  kind: Secret
  metadata:
    name: cloudflare-api-token
    namespace: cloudflare-tunnel-system
  type: Opaque
  stringData:
    token: <your-cloudflare-api-token> # CHANGE ME !!!
  ```

  ```shell
  kubectl apply -f cloudflare-api-token.yaml
  ```

You will also need your Cloudflare Account ID, for DNS.

### Installation

The Helm chart is stored in the OCI format in our company Helm repository, so there is no need to add another Helm repository.

```shell
export CLOUDFLARE_ACCOUNT_ID=<your-cloudflare-account-id>

helm upgrade --install \
  --namespace cloudflare-tunnel-system --create-namespace \
  cloudflare-tunnel-ingress oci://registry.clbs.io/cloudflare-tunnel-ingress-controller/cloudflare-tunnel-ingress-controller \
  --set config.cloudflare.apiToken.existingSecret.name=cloudflare-api-token \
  --set config.cloudflare.accountID=$CLOUDFLARE_ACCOUNT_ID \
  --set config.cloudflare.tunnelName=tunnel-ingress-demo
```

### Uninstall

```shell
helm uninstall --namespace cloudflare-tunnel-system cloudflare-tunnel-ingress
```

## Using port-forward via Cloudflare Tunnel

The `kubectl port-forward` command utilizes an HTTP connection upgrade, which can fail if the connection is established via a Cloudflare (CF) Tunnel.
To resolve this issue, direct TCP access to the Kubernetes API server is required.

The clbs Cloudflare Tunnel Ingress Controller automatically creates access records that allow the use of the `cloudflared access tcp` command to directly forward to the Kubernetes API server.

### Command Usage

The `cloudflared access tcp` command requires two arguments:

* **hostname** - It shall correspond with the tunnel domain.
* **url** - The `host:port` value on which the tcp access shall locally listen on. Typically it should be `127.0.0.1:6443`.

Example command:

```sh
cloudflared access tcp --hostname k.example.com --url 127.0.0.1:6443
```

### Updating kubeconfig

You can then extend your kubeconfig by setting the proxy-url value to route traffic through the TCP tunnel.

```yaml
clusters:
- cluster:
    server: https://127.0.0.1:6443     # The value just has to be set.
    proxy-url: socks5://localhost:6443 # The same as the --url value in cloudflared access command.
```

This setup ensures that your kubectl commands will work correctly when using a Cloudflare Tunnel.
