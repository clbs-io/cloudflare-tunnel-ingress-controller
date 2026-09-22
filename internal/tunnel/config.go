package tunnel

import (
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
)

// Config is the desired state of the tunnel.
type Config struct {
	// Rules are the tunnel ingress rules in match order, without the
	// catch-all rule.
	Rules IngressRecords
	// AccessAppRequests maps hostnames to the name of the Access application
	// to create when none exists for the hostname.
	AccessAppRequests map[string]string
}

// IngressRecords is an ordered list of tunnel ingress rules.
type IngressRecords = []*zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress
