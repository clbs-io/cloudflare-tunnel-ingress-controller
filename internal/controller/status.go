package controller

import (
	"context"

	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *IngressController) ensureStatus(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	host_add := make(map[string]struct{})
	for _, ingressRecords := range c.tunnelConfig.Ingresses {
		for _, ingress := range *ingressRecords {
			if len(ingress.Hostname) > 0 {
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

	ing = ing.DeepCopy()

	for i := len(idx_remove) - 1; i >= 0; i-- {
		remove_idx := idx_remove[i]
		ing.Status.LoadBalancer.Ingress = append(ing.Status.LoadBalancer.Ingress[:remove_idx], ing.Status.LoadBalancer.Ingress[remove_idx+1:]...)
	}

	for hostname := range host_add {
		ing.Status.LoadBalancer.Ingress = append(ing.Status.LoadBalancer.Ingress, networkingv1.IngressLoadBalancerIngress{
			Hostname: hostname,
		})
	}

	_, err := c.clientset.NetworkingV1().Ingresses(ing.Namespace).UpdateStatus(ctx, ing, v1.UpdateOptions{})
	if err != nil {
		logger.Error(err, "Failed to update Ingress resource with status")
		return err
	}

	return nil
}
