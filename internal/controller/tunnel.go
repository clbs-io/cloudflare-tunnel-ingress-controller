package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"strings"

	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (c *IngressController) SetTunnelToken(token string) {
	c.cloudflaredDeploymentConfig.tunnelToken = token
}

func (c *IngressController) ensureCloudflareTunnelExists(ctx context.Context, logger logr.Logger) error {
	logger.Info("Ensuring Cloudflare Tunnel exists")
	err := c.tunnelClient.EnsureTunnelExists(ctx)
	if err != nil {
		logger.Error(err, "Failed to ensure Cloudflare Tunnel exists")
		return err
	}

	token, err := c.tunnelClient.GetTunnelToken(ctx)
	if err != nil {
		logger.Error(err, "Failed to get Cloudflare Tunnel token")
		return err
	}

	c.cloudflaredDeploymentConfig.tunnelToken = token
	return nil
}

func (c *IngressController) ensureCloudflareTunnelConfiguration(ctx context.Context, logger logr.Logger, tunnelConfig *tunnel.Config, ingress *networkingv1.Ingress) error {
	logger.Info("Ensuring Cloudflare Tunnel configuration")

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

				for _, port := range service.Spec.Ports {
					if port.Name == path.Backend.Service.Port.Name {
						portNumber = port.Port
						break
					}
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

			tunnelIng := tunnel.IngressConfig{
				Hostname: rule.Host,
				Path:     path.Path,
				Service:  tunnelService,
			}
			origin_config := &tunnel.IngressOriginConfig{}
			origin_config_set := false

			for k, v := range ingress.Annotations {
				if k == AnnotationOriginConnectTimeout {
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.ConnectTimeout = &t
					}
				} else if k == AnnotationOriginTlsTimeout {
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.TLSTimeout = &t
					}
				} else if k == AnnotationOriginTcpKeepalive {
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.TCPKeepAlive = &t
					}
				} else if k == AnnotationOriginNoHappyEyeballs {
					t, err := strconv.ParseBool(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.NoHappyEyeballs = &t
					}
				} else if k == AnnotationOriginKeepaliveConnections {
					t, err := strconv.Atoi(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.KeepAliveConnections = &t
					}
				} else if k == AnnotationOriginKeepaliveTimeout {
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.KeepAliveTimeout = &t
					}
				} else if k == AnnotationOriginHttpHostHeader {
					origin_config_set = true
					origin_config.HTTPHostHeader = &v
				} else if k == AnnotationOriginServerName {
					origin_config_set = true
					origin_config.OriginServerName = &v
				} else if k == AnnotationOriginNoTlsVerify {
					t, err := strconv.ParseBool(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.NoTLSVerify = &t
					}
				} else if k == AnnotationOriginDisableChunkedEncoding {
					t, err := strconv.ParseBool(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.DisableChunkedEncoding = &t
					}
				} else if k == AnnotationOriginBastionMode {
					t, err := strconv.ParseBool(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.BastionMode = &t
					}
				} else if k == AnnotationOriginProxyAddress {
					origin_config_set = true
					origin_config.ProxyAddress = &v
				} else if k == AnnotationOriginProxyPort {
					t, err := strconv.Atoi(v)
					if err != nil || t < 1 || t > 65535 {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						u := uint(t)
						origin_config_set = true
						origin_config.ProxyPort = &u
					}
				} else if k == AnnotationOriginProxyType {
					origin_config_set = true
					origin_config.ProxyType = &v
				} else if k == AnnotationOriginHttp2Origin {
					t, err := strconv.ParseBool(v)
					if err != nil {
						logger.Error(err, "Failed to parse duration", "annotation", k)
					} else {
						origin_config_set = true
						origin_config.Http2Origin = &t
					}
				}
			}

			if origin_config_set {
				tunnelIng.OriginConfig = origin_config
			}

			cfg = append(cfg, &tunnelIng)
		}
	}

	// Add a default 404 service if there is at least one ingress and the last one is not a 404
	if cnt := len(cfg); cnt > 0 {
		if cfg[cnt-1].Service != "http_status:404" {
			cfg = append(cfg, &tunnel.IngressConfig{Service: "http_status:404"})
		}
	}

	tunnelConfig.Ingresses[ingress.UID] = &cfg

	err := c.tunnelClient.EnsureTunnelConfiguration(ctx, logger, tunnelConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure Cloudflare Tunnel configuration")
		return err
	}

	return nil
}

func (c *IngressController) deleteTunnelConfigurationForIngress(ctx context.Context, logger logr.Logger, tunnelConfig *tunnel.Config, ingressUid types.UID) error {
	logger.Info("Deleting tunnel configuration for Ingress resource")

	ing := tunnelConfig.Ingresses[ingressUid]
	delete(tunnelConfig.Ingresses, ingressUid)
	err := c.tunnelClient.DeleteFromTunnelConfiguration(ctx, logger, ing)
	if err != nil {
		logger.Error(err, "Failed to delete from tunnel configuration")
		return err
	}

	return nil
}
