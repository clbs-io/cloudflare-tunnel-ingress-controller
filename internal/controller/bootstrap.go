package controller

import (
	"context"
	"slices"
	"time"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type IngressControllerOptions struct {
	IngressClassName      string
	ControllerClassName   string
	ResyncPeriod          time.Duration
	TunnelClient          *tunnel.Client
	TunnelTokenSecret     string
	CloudflaredDeployment string
}

func RegisterIngressController(logger logr.Logger, mgr manager.Manager, options IngressControllerOptions) (*IngressController, error) {
	ingressController := NewIngressController(logger.WithName("ingress-controller"), mgr.GetClient(), mgr.GetAPIReader(), mgr.GetEventRecorder("cloudflare-tunnel-ingress-controller"), options)

	toTunnel := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{Name: tunnelReconcileKey}}
	})

	// Fires the first reconcile, so it writes the token Secret and prepares
	// the chart's cloudflared Deployment without any Ingress
	startup := make(chan event.GenericEvent, 1)
	startup <- event.GenericEvent{Object: &networkingv1.Ingress{}}

	servicePortsChanged := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			old_service, old_ok := e.ObjectOld.(*corev1.Service)
			new_service, new_ok := e.ObjectNew.(*corev1.Service)
			return !old_ok || !new_ok || !equality.Semantic.DeepEqual(old_service.Spec.Ports, new_service.Spec.Ports)
		},
	}

	err := builder.ControllerManagedBy(mgr).
		Named("tunnel").
		Watches(&networkingv1.Ingress{}, toTunnel, builder.WithPredicates(ingressController.ingressEventFilter())).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(ingressController.mapServiceToTunnel),
			builder.WithPredicates(servicePortsChanged)).
		WatchesRawSource(source.Channel(startup, toTunnel)).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(ingressController)
	if err != nil {
		logger.WithName("register-controller").Error(err, "could not register ingress controller")
		return nil, err
	}

	return ingressController, nil
}

// ingressEventFilter passes Ingress events that change the generation or the
// annotations of an Ingress relevant to the tunnel.
func (c *IngressController) ingressEventFilter() predicate.Predicate {
	relevant := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return c.isTunnelIngress(e.Object)
		},
		// An Ingress moved to another class is relevant through its old object
		UpdateFunc: func(e event.UpdateEvent) bool {
			return c.isTunnelIngress(e.ObjectOld) || c.isTunnelIngress(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return c.isTunnelIngress(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return c.isTunnelIngress(e.Object)
		},
	}
	return predicate.And(relevant, predicate.Or(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}))
}

// isTunnelIngress reports whether obj is an Ingress of our class or one that
// carries our finalizer.
func (c *IngressController) isTunnelIngress(obj client.Object) bool {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return false
	}
	return c.hasOurClass(ingress) || slices.Contains(ingress.Finalizers, ingressTunnelFinalizer)
}

// mapServiceToTunnel enqueues the tunnel when an Ingress of our class in the
// Service's namespace uses the Service through a named port, the only case in
// which the rendered rules depend on the Service. A failed Ingress list
// enqueues the tunnel.
func (c *IngressController) mapServiceToTunnel(ctx context.Context, obj client.Object) []reconcile.Request {
	enqueue := []reconcile.Request{{Name: tunnelReconcileKey}}

	ingresses := &networkingv1.IngressList{}
	if err := c.client.List(ctx, ingresses, client.InNamespace(obj.GetNamespace())); err != nil {
		c.logger.Error(err, "Failed to list Ingress resources for a Service event", "namespace", obj.GetNamespace(), "name", obj.GetName())
		return enqueue
	}
	for i := range ingresses.Items {
		ingress := &ingresses.Items[i]
		if !c.hasOurClass(ingress) {
			continue
		}
		for _, rule := range ingress.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				backend := path.Backend.Service
				if backend != nil && backend.Name == obj.GetName() && len(backend.Port.Name) > 0 {
					return enqueue
				}
			}
		}
	}
	return nil
}
