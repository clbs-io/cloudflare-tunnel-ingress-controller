package controller

import (
	"context"

	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
)

func (c *IngressController) ensureStatus(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	host_add := make(map[string]struct{})
	for _, ingressRecords := range c.tunnelConfig.Ingresses {
		for _, ingress := range *ingressRecords {
			if ingress.Hostname != "" {
				host_add[ingress.Hostname] = struct{}{}
			}
		}
	}

	idx_remove := make([]int, 0, len(ing.Status.LoadBalancer.Ingress))
	for i, lbIngress := range ing.Status.LoadBalancer.Ingress {
		if _, ok := host_add[lbIngress.Hostname]; ok {
			delete(host_add, lbIngress.Hostname)
		} else {
			idx_remove = append(idx_remove, i)
		}
	}

	if len(host_add) == 0 && len(idx_remove) == 0 {
		return nil
	}

	logger.Info("Updating Ingress status")

	for i := len(idx_remove) - 1; i >= 0; i-- {
		ing.Status.LoadBalancer.Ingress = append(ing.Status.LoadBalancer.Ingress[:idx_remove[i]], ing.Status.LoadBalancer.Ingress[idx_remove[i]+1:]...)
	}

	for hostname := range host_add {
		ing.Status.LoadBalancer.Ingress = append(ing.Status.LoadBalancer.Ingress, networkingv1.IngressLoadBalancerIngress{
			Hostname: hostname,
		})
	}

	err := c.client.Update(ctx, ing)
	if err != nil {
		logger.Error(err, "Failed to update Ingress resource with status")
		return err
	}

	return nil
}
