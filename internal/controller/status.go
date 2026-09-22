package controller

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ensureStatus sets the Ingress load balancer status to the published
// hostnames.
func (c *IngressController) ensureStatus(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress, hostnames []string) error {
	current := make([]string, 0, len(ing.Status.LoadBalancer.Ingress))
	for _, lb := range ing.Status.LoadBalancer.Ingress {
		current = append(current, lb.Hostname)
	}
	if slices.Equal(current, hostnames) {
		return nil
	}

	logger.Info("Updating Ingress status", "namespace", ing.Namespace, "name", ing.Name, "hostnames", hostnames)
	patch := client.MergeFrom(ing.DeepCopy())
	ing.Status.LoadBalancer.Ingress = make([]networkingv1.IngressLoadBalancerIngress, 0, len(hostnames))
	for _, hostname := range hostnames {
		ing.Status.LoadBalancer.Ingress = append(ing.Status.LoadBalancer.Ingress, networkingv1.IngressLoadBalancerIngress{Hostname: hostname})
	}
	if err := c.client.Status().Patch(ctx, ing, patch); err != nil {
		logger.Error(err, "Failed to update Ingress status", "namespace", ing.Namespace, "name", ing.Name)
		return err
	}
	return nil
}
