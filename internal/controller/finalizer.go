package controller

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ingressTunnelFinalizer keeps an Ingress until the tunnel no longer serves
// it. Every installation of this controller uses the same name;
// classifyIngresses decides whose finalizer it is from the IngressClass.
const ingressTunnelFinalizer = "finalizer.cloudflare-tunnel-ingress-controller.clbs.io/tunnel"

// ensureFinalizers adds our finalizer to ing unless it is already there. The
// merge patch replaces the whole finalizer list, so it carries an optimistic
// lock: a concurrent change by someone else fails the patch and the run is
// retried instead of that change being overwritten.
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
// Ingress, with the same optimistic lock as ensureFinalizers. An Ingress that
// is already gone counts as released.
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
