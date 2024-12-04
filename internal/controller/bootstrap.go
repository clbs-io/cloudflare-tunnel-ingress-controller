package controller

import (
	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
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

func RegisterIngressController(logger logr.Logger, mgr manager.Manager, options IngressControllerOptions) (*IngressController, error) {
	controller, err := NewIngressController(logger.WithName("ingress-controller"), mgr.GetClient(), mgr.GetConfig(), options.TunnelClient, options.IngressClassName, options.ControllerClassName, options.CloudflaredConfig)
	if err != nil {
		logger.WithName("register-controller").Error(err, "could not create ingress controller")
		return nil, err
	}

	err = builder.
		ControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Complete(controller)

	if err != nil {
		logger.WithName("register-controller").Error(err, "could not register ingress controller")
		return nil, err
	}

	return controller, nil
}
