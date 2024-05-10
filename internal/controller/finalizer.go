package controller

import (
	"context"
	"errors"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
)

const ingressFinalizer = "finalizer.cloudflare-tunnel-ingress-controller.clbs.io"

func (c *IngressController) ensureFinalizers(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	containsFinalizer := false
	for _, finalizer := range ing.GetFinalizers() {
		if finalizer == ingressFinalizer {
			containsFinalizer = true
			break
		}
	}

	if !containsFinalizer {
		logger.Info("Adding Finalizer for the Ingress resource")
		ing.SetFinalizers(append(ing.GetFinalizers(), ingressFinalizer))

		err := c.client.Update(ctx, ing)
		if err != nil {
			logger.Error(err, "Failed to update Ingress resource with a finalizer", "finalizer", ingressFinalizer)
			return err
		}
	}
	return nil
}

func (c *IngressController) finalizeIngress(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	return errors.New("not implemented")
}
