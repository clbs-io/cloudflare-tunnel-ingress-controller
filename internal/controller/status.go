package controller

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *IngressController) ensureStatus(ctx context.Context, logger logr.Logger, ing *networkingv1.Ingress) error {
	host_add := make(map[string]struct{})
	if ingressRecords, ok := c.tunnelConfig.Ingresses[ing.UID]; ok {
		for _, ingress := range *ingressRecords {
			if len(ingress.Hostname) > 0 {
				host_add[ingress.Hostname] = struct{}{}
			}
		}
	}

	has_stale := false
	for _, lbIngress := range ing.Status.LoadBalancer.Ingress {
		if _, ok := host_add[lbIngress.Hostname]; ok {
			delete(host_add, lbIngress.Hostname)
		} else {
			has_stale = true
		}
	}

	if len(host_add) == 0 && !has_stale {
		return nil
	}

	logger.Info("Updating Ingress status")

	ing = ing.DeepCopy()

	if has_stale {
		valid_hosts := make(map[string]struct{})
		if ingressRecords, ok := c.tunnelConfig.Ingresses[ing.UID]; ok {
			for _, ingress := range *ingressRecords {
				if len(ingress.Hostname) > 0 {
					valid_hosts[ingress.Hostname] = struct{}{}
				}
			}
		}
		ing.Status.LoadBalancer.Ingress = slices.DeleteFunc(ing.Status.LoadBalancer.Ingress, func(lbIngress networkingv1.IngressLoadBalancerIngress) bool {
			_, ok := valid_hosts[lbIngress.Hostname]
			return !ok
		})
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
