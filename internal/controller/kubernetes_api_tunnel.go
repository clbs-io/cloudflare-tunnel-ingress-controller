package controller

import "fmt"

// socksProxyType makes cloudflared run a SOCKS proxy for the route.
const socksProxyType = "socks"

// KubernetesApiTunnelConfig exposes the Kubernetes API through the tunnel as
// a SOCKS proxy route protected by a Cloudflare Access application.
type KubernetesApiTunnelConfig struct {
	Enabled bool
	// Kubernetes API server address, host:port
	Server string
	// Public hostname of the route
	Domain string
	// Name of the Access application created for Domain
	CloudflareAccessAppName string
}

func (c KubernetesApiTunnelConfig) GetService() string {
	return fmt.Sprintf("tcp://%s", c.Server)
}
