package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"strings"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
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

	c.cloudflaredDeploymentConfig.tunnelToken = token
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

			tunnelIng := &zero_trust.TunnelConfigurationGetResponseConfigIngress{
				Hostname: rule.Host,
				Path:     path.Path,
				Service:  tunnelService,
			}
			origin_config := &tunnelIng.OriginRequest

			for k, v := range ingress.Annotations {
				switch k {
				case AnnotationOriginConnectTimeout:
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse origin connect timeout", "annotation", k)
					} else {
						origin_config.ConnectTimeout = t.Nanoseconds()
					}
				case AnnotationOriginTlsTimeout:
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse origin tls timeout", "annotation", k)
					} else {
						origin_config.TLSTimeout = t.Nanoseconds()
					}
				case AnnotationOriginTcpKeepalive:
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse origin tcp keepalive", "annotation", k)
					} else {
						origin_config.TCPKeepAlive = t.Nanoseconds()
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
					t, err := time.ParseDuration(v)
					if err != nil {
						logger.Error(err, "Failed to parse origin keepalive timeout", "annotation", k)
					} else {
						origin_config.KeepAliveTimeout = t.Nanoseconds()
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

			cfg = append(cfg, tunnelIng)
		}
	}

	tunnelConfig.Ingresses[ingress.UID] = &cfg

	return nil
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
