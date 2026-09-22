package controller

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ingressTunnelFinalizer = "finalizer.cloudflare-tunnel-ingress-controller.clbs.io/tunnel"

func (c *IngressController) ensureFinalizers(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	if slices.Contains(ing.GetFinalizers(), ingressTunnelFinalizer) {
		return nil
	}

	logger.Info("Adding finalizer to Ingress", "namespace", ing.Namespace, "name", ing.Name)
	patch := client.MergeFromWithOptions(ing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	ing.SetFinalizers(append(ing.GetFinalizers(), ingressTunnelFinalizer))
	if err := c.client.Patch(ctx, ing, patch); err != nil {
		logger.Error(err, "Failed to add finalizer to Ingress", "namespace", ing.Namespace, "name", ing.Name)
		return err
	}
	return nil
}

// releaseIngress removes our finalizer once the tunnel no longer serves the
// Ingress. An Ingress that is already gone counts as released.
func (c *IngressController) releaseIngress(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	logger.Info("Removing finalizer from Ingress", "namespace", ing.Namespace, "name", ing.Name)
	patch := client.MergeFromWithOptions(ing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	ing.SetFinalizers(slices.DeleteFunc(ing.GetFinalizers(), func(f string) bool {
		return f == ingressTunnelFinalizer
	}))
	if err := client.IgnoreNotFound(c.client.Patch(ctx, ing, patch)); err != nil {
		logger.Error(err, "Failed to remove finalizer from Ingress", "namespace", ing.Namespace, "name", ing.Name)
		return err
	}
	return nil
}
