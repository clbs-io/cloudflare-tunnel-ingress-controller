package controller

import (
	"context"
	"fmt"
	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"strings"
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

func (c *IngressController) ensureCloudflareTunnelConfiguration(ctx context.Context, logger logr.Logger, ingress *networkingv1.Ingress) error {
	logger.Info("Ensuring Cloudflare Tunnel configuration")

	cfg := tunnel.Config{
		Ingresses: make([]tunnel.IngressConfig, 0),
	}

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
						if value == protocol {
							scheme = strings.ToLower(value)
							break
						}
					}
				}
			}

			tunnelService := fmt.Sprintf("%s://%s.%s:%d", scheme, path.Backend.Service.Name, ingress.Namespace, portNumber)

			tunnelIng := tunnel.IngressConfig{
				Hostname: rule.Host,
				Path:     path.Path,
				Service:  tunnelService,
			}

			cfg.Ingresses = append(cfg.Ingresses, tunnelIng)
		}
	}

	err := c.tunnelClient.EnsureTunnelConfiguration(ctx, logger, cfg)
	if err != nil {
		logger.Error(err, "Failed to ensure Cloudflare Tunnel configuration")
		return err
	}

	return nil
}
