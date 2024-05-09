package controller

import (
	"context"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type IngressController struct {
	logger logr.Logger
}

func NewIngressController(logger logr.Logger, client client.Client, ingressClassName, controllerClassName string) *IngressController {
	return &IngressController{logger: logger}
}

func (c *IngressController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// TODO
	// - ensure cloudflared deployment exists
	// - watch ingress resources with the correct class name
	// - ensure ingress resource finalizers are set
	// - update tunnel configuration based on ingress resources
	// - update ingress resource status

	//err := c.ensureCloudflaredDeploymentExists()
	//if err != nil {
	//	return reconcile.Result{}, err
	//}

	return reconcile.Result{}, nil
}
