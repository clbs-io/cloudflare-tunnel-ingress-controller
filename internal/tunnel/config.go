package tunnel

import (
	"fmt"

	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	"k8s.io/apimachinery/pkg/types"
)

// Config represents the configuration for the Cloudflare Tunnel.
type Config struct {
	// Ingresses is a list of Ingress resources to configure the Cloudflare Tunnel with.
	// The key is the UID of the Ingress resource.
	Ingresses map[types.UID]*IngressRecords
	// Kubernetes API tunneling configuration
	KubernetesApiTunnelConfig KubernetesApiTunnelConfig
}

// List of records for single ingress resource.
type IngressRecords = []*zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress

type KubernetesApiTunnelConfig struct {
	// Enable Kubernetes API Tunnel
	Enabled bool
	// Kubernetes API Server
	Server string
	// Public domain where the Kubernetes API will be exposed
	Domain string
	// Related Cloudflare access application name
	CloudflareAccessAppName string
}

func (c KubernetesApiTunnelConfig) GetService() string {
	return fmt.Sprintf("tcp://%s", c.Server)
}
