package controller

import (
	"context"
	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type IngressControllerOptions struct {
	IngressClassName    string
	ControllerClassName string
	TunnelClient        *tunnel.Client
	CloudflaredConfig   CloudflaredConfig
}

func RegisterIngressController(logger logr.Logger, mgr manager.Manager, options IngressControllerOptions) error {
	controller := NewIngressController(logger.WithName("ingress-controller"), mgr.GetClient(), options.TunnelClient, options.IngressClassName, options.ControllerClassName, options.CloudflaredConfig)
	err := builder.
		ControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Complete(controller)

	if err != nil {
		logger.WithName("register-controller").Error(err, "could not register ingress controller")
		return err
	}

	err = controller.ensureCloudflareTunnelExists(context.Background(), logger)
	if err != nil {
		logger.Error(err, "failed to ensure cloudflare tunnel exists")
		return err
	}

	err = controller.ensureCloudflaredDeploymentExists(context.Background(), logger)
	if err != nil {
		logger.Error(err, "failed to ensure cloudflared deployment exists")
		return err
	}

	return nil
}
