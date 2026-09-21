package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (c *IngressController) SetTunnelToken(token string) {
	c.cloudflaredDeploymentConfig.tunnelTokenLck.Lock()
	defer c.cloudflaredDeploymentConfig.tunnelTokenLck.Unlock()
	c.cloudflaredDeploymentConfig.tunnelToken = token
}

func (c *IngressController) ensureCloudflareTunnelExists(ctx context.Context, logger logr.Logger) error {
	logger.Info("Ensuring Cloudflare Tunnel exists")
	err := c.tunnelClient.EnsureTunnelExists(ctx, logger)
	if err != nil {
		logger.Error(err, "Failed to ensure Cloudflare Tunnel exists")
		return err
	}

	token, err := c.tunnelClient.GetTunnelToken(ctx)
	if err != nil {
		logger.Error(err, "Failed to get Cloudflare Tunnel token")
		return err
	}

	c.cloudflaredDeploymentConfig.tunnelTokenLck.Lock()
	c.cloudflaredDeploymentConfig.tunnelToken = token
	c.cloudflaredDeploymentConfig.tunnelTokenLck.Unlock()
	return nil
}

func (c *IngressController) harvestRules(ctx context.Context, logger logr.Logger, tunnelConfig *tunnel.Config, ingress *networkingv1.Ingress) error {
	cfg := tunnel.IngressRecords{}

	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}

		for _, path := range rule.HTTP.Paths {
			if path.PathType == nil {
				continue
			}

			// We do not support pathType=Exact
			// pathType=Prefix and pathType=ImplementationSpecific are supported
			// and behave the same way
			if *path.PathType == networkingv1.PathTypeExact {
				continue
			}

			portNumber := path.Backend.Service.Port.Number
			if path.Backend.Service.Port.Name != "" {
				service := &corev1.Service{}

				err := c.client.Get(ctx, types.NamespacedName{Name: path.Backend.Service.Name, Namespace: ingress.Namespace}, service)
				if err != nil {
					logger.Error(err, "Failed to get Service")
					return err
				}

				portFound := false
				for _, port := range service.Spec.Ports {
					if port.Name == path.Backend.Service.Port.Name {
						portNumber = port.Port
						portFound = true
						break
					}
				}
				if !portFound {
					logger.Error(nil, "Named port not found in Service, skipping path", "service", path.Backend.Service.Name, "portName", path.Backend.Service.Port.Name)
					continue
				}
			}

			scheme := "http"
			for annotation, value := range ingress.Annotations {
				// find right annotation
				if annotation == AnnotationBackendProtocol {
					// check if annotation value (backend protocol) is supported
					for _, protocol := range SupportedBackendProtocols {
						if strings.EqualFold(value, protocol) {
							scheme = strings.ToLower(value)
							break
						}
					}
					break
				}
			}

			tunnelService := fmt.Sprintf("%s://%s.%s:%d", scheme, path.Backend.Service.Name, ingress.Namespace, portNumber)

			tunnelIng := &zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress{
				Hostname: rule.Host,
				Path:     path.Path,
				Service:  tunnelService,
			}
			applyOriginRequestAnnotations(logger, &tunnelIng.OriginRequest, ingress.Annotations)

			cfg = append(cfg, tunnelIng)
		}
	}

	tunnelConfig.Ingresses[ingress.UID] = &cfg

	// Track hostnames that need a Cloudflare Access application auto-created
	if app_name, ok := ingress.Annotations[AnnotationAccessAppName]; ok && app_name != "" {
		for _, rule := range ingress.Spec.Rules {
			if rule.Host != "" {
				tunnelConfig.AccessAppRequests[rule.Host] = app_name
			}
		}
	}

	return nil
}

func applyOriginRequestAnnotations(logger logr.Logger, origin_config *zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest, annotations map[string]string) {
	for k, v := range annotations {
		switch k {
		case AnnotationAccessRequired:
			t, err := strconv.ParseBool(v)
			if err != nil {
				logger.Error(err, "Failed to parse access required", "annotation", k)
			} else {
				origin_config.Access.Required = t
			}
		case AnnotationAccessTeamName:
			origin_config.Access.TeamName = v
		case AnnotationAccessAudTag:
			origin_config.Access.AUDTag = strings.Split(v, ",")
		case AnnotationOriginConnectTimeout:
			t, err := parseSeconds(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin connect timeout", "annotation", k)
			} else {
				origin_config.ConnectTimeout = t
			}
		case AnnotationOriginTlsTimeout:
			t, err := parseSeconds(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin tls timeout", "annotation", k)
			} else {
				origin_config.TLSTimeout = t
			}
		case AnnotationOriginTcpKeepalive:
			t, err := parseSeconds(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin tcp keepalive", "annotation", k)
			} else {
				origin_config.TCPKeepAlive = t
			}
		case AnnotationOriginNoHappyEyeballs:
			t, err := strconv.ParseBool(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin no happy eyeballs", "annotation", k)
			} else {
				origin_config.NoHappyEyeballs = t
			}
		case AnnotationOriginKeepaliveConnections:
			t, err := strconv.Atoi(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin keepalive connections", "annotation", k)
			} else {
				origin_config.KeepAliveConnections = int64(t)
			}
		case AnnotationOriginKeepaliveTimeout:
			t, err := parseSeconds(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin keepalive timeout", "annotation", k)
			} else {
				origin_config.KeepAliveTimeout = t
			}
		case AnnotationOriginHttpHostHeader:
			origin_config.HTTPHostHeader = v
		case AnnotationOriginServerName:
			origin_config.OriginServerName = v
		case AnnotationOriginNoTlsVerify:
			t, err := strconv.ParseBool(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin no tls verify", "annotation", k)
			} else {
				origin_config.NoTLSVerify = t
			}
		case AnnotationOriginDisableChunkedEncoding:
			t, err := strconv.ParseBool(v)
			if err != nil {
				logger.Error(err, "Failed to parse origin disable chunked encoding", "annotation", k)
			} else {
				origin_config.DisableChunkedEncoding = t
			}
		case AnnotationOriginProxyType:
			origin_config.ProxyType = v
		case AnnotationOriginHttp2Origin:
			t, err := strconv.ParseBool(v)
			if err != nil {
				logger.Error(err, "Failed to parse duration", "annotation", k)
			} else {
				origin_config.HTTP2Origin = t
			}
		}
	}
}

func (c *IngressController) ensureCloudflareTunnelConfiguration(ctx context.Context, logger logr.Logger, tunnelConfig *tunnel.Config, ingress *networkingv1.Ingress) error {
	err := c.harvestRules(ctx, logger, tunnelConfig, ingress)
	if err != nil {
		return err
	}

	err = c.tunnelClient.EnsureTunnelConfiguration(ctx, logger, tunnelConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure Cloudflare Tunnel configuration")
		return err
	}

	return nil
}

func (c *IngressController) deleteTunnelConfigurationForIngress(ctx context.Context, logger logr.Logger, tunnelConfig *tunnel.Config, ingress *networkingv1.Ingress) error {
	logger.Info("Deleting tunnel configuration for Ingress resource")

	records, has_records := tunnelConfig.Ingresses[ingress.UID]
	access_app_requests := maps.Clone(tunnelConfig.AccessAppRequests)

	hostnames := make([]string, 0, len(ingress.Spec.Rules))
	for _, rule := range ingress.Spec.Rules {
		if len(rule.Host) > 0 {
			hostnames = append(hostnames, rule.Host)
		}
	}

	// Remove any Access app requests for hostnames belonging to this ingress
	if records != nil {
		for _, record := range *records {
			delete(tunnelConfig.AccessAppRequests, record.Hostname)
			hostnames = append(hostnames, record.Hostname)
		}
	}
	slices.Sort(hostnames)
	hostnames = slices.Compact(hostnames)

	delete(tunnelConfig.Ingresses, ingress.UID)
	err := c.tunnelClient.DeleteFromTunnelConfiguration(ctx, logger, tunnelConfig, hostnames)
	if err != nil {
		// Keep the ingress state so the finalizer retry cleans it up again
		if has_records {
			tunnelConfig.Ingresses[ingress.UID] = records
		}
		tunnelConfig.AccessAppRequests = access_app_requests
		logger.Error(err, "Failed to delete from tunnel configuration")
		return err
	}

	return nil
}

// parseSeconds parses a duration that is a whole, positive number of seconds,
// the unit of origin timeouts in the tunnel configuration.
func parseSeconds(value string) (int64, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d < time.Second || d%time.Second != 0 {
		return 0, fmt.Errorf("duration %q must be a whole number of seconds, at least 1s", value)
	}
	return int64(d / time.Second), nil
}
