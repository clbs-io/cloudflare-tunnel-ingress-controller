package controller

import (
	"context"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ingressTunnelFinalizer = "finalizer.cloudflare-tunnel-ingress-controller.clbs.io/tunnel"

func (c *IngressController) ensureFinalizers(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	containsFinalizer := false
	for _, finalizer := range ing.GetFinalizers() {
		if finalizer == ingressTunnelFinalizer {
			containsFinalizer = true
			break
		}
	}

	if !containsFinalizer {
		logger.Info("Adding Finalizer for the Ingress resource")
		patch := client.MergeFrom(ing.DeepCopy())
		ing.SetFinalizers(append(ing.GetFinalizers(), ingressTunnelFinalizer))

		err := c.client.Patch(ctx, ing, patch)
		if err != nil {
			logger.Error(err, "Failed to patch Ingress resource with a finalizer", "finalizer", ingressTunnelFinalizer)
			return err
		}
	}
	return nil
}

func (c *IngressController) finalizeIngress(ctx context.Context, logger logr.Logger, tunnelConfig *tunnel.Config, ing *networkingv1.Ingress) error {
	err := c.deleteTunnelConfigurationForIngress(ctx, logger, tunnelConfig, ing.UID)
	if err != nil {
		logger.Error(err, "Failed to delete tunnel configuration for Ingress")
		return err
	}

	patch := client.MergeFrom(ing.DeepCopy())
	ing.SetFinalizers(removeFinalizer(ing.GetFinalizers(), ingressTunnelFinalizer))

	err = c.client.Patch(ctx, ing, patch)
	if err != nil {
		logger.Error(err, "Failed to patch Ingress after removing finalizer")
		return err
	}

	return nil
}

func removeFinalizer(finalizers []string, finalizer string) []string {
	for i, f := range finalizers {
		if f == finalizer {
			return append(finalizers[:i], finalizers[i+1:]...)
		}
	}
	return finalizers
}
