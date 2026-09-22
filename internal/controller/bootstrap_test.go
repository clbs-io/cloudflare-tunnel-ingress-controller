package controller

import (
	"context"
	"errors"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestIsTunnelIngress(t *testing.T) {
	other := tunnelIngress("other", "other.example.com")
	other.Spec.IngressClassName = new("other")
	claimed := other.DeepCopy()
	claimed.Finalizers = []string{ingressTunnelFinalizer}
	f := newFixture(t)

	tests := []struct {
		name    string
		ingress *networkingv1.Ingress
		want    bool
	}{
		{"our class", tunnelIngress("app", "app.example.com"), true},
		{"other class", other, false},
		{"other class with our finalizer", claimed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.controller.isTunnelIngress(tt.ingress); got != tt.want {
				t.Errorf("isTunnelIngress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIngressEventFilter(t *testing.T) {
	ours := tunnelIngress("app", "app.example.com")
	ours.Generation = 1
	status_only := ours.DeepCopy()
	status_only.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{{Hostname: "app.example.com"}}
	moved := ours.DeepCopy()
	moved.Spec.IngressClassName = new("other")
	moved.Generation = 2
	other_changed := moved.DeepCopy()
	other_changed.Generation = 3
	f := newFixture(t)
	filter := f.controller.ingressEventFilter()

	tests := []struct {
		name     string
		old, new *networkingv1.Ingress
		want     bool
	}{
		{"moved from our class to another", ours, moved, true},
		{"status-only update of our Ingress", ours, status_only, false},
		{"spec change of an Ingress of another class", moved, other_changed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filter.Update(event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}); got != tt.want {
				t.Errorf("Update() = %v, want %v", got, tt.want)
			}
		})
	}
	if !filter.Create(event.CreateEvent{Object: ours}) {
		t.Error("expected the creation of our Ingress to pass")
	}
	if filter.Create(event.CreateEvent{Object: moved}) {
		t.Error("expected the creation of an Ingress of another class to be filtered out")
	}
}

// namedPortIngress is an Ingress of our class in namespace "ns" whose backend
// is Service service through its port named "http".
func namedPortIngress(name, service string) *networkingv1.Ingress {
	ingress := tunnelIngress(name, name+".example.com")
	ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service = &networkingv1.IngressServiceBackend{
		Name: service, Port: networkingv1.ServiceBackendPort{Name: "http"},
	}
	return ingress
}

func TestMapServiceToTunnel(t *testing.T) {
	other_class := namedPortIngress("app", "web")
	other_class.Spec.IngressClassName = new("other")

	tests := []struct {
		name      string
		ingress   *networkingv1.Ingress
		namespace string
		want      bool
	}{
		{"referenced by named port", namedPortIngress("app", "web"), "ns", true},
		{"referenced by port number", tunnelIngress("web", "web.example.com"), "ns", false},
		{"Service in another namespace", namedPortIngress("app", "web"), "elsewhere", false},
		{"referenced by an Ingress of another class", other_class, "ns", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.ingress)
			service := &corev1.Service{Name: "web", Namespace: tt.namespace}

			got := f.controller.mapServiceToTunnel(t.Context(), service)

			if want := tunnelRequests(tt.want); !slices.Equal(got, want) {
				t.Errorf("mapServiceToTunnel() = %v, want %v", got, want)
			}
		})
	}
}

func TestMapServiceToTunnel_ListErrorEnqueues(t *testing.T) {
	f := newFixture(t)
	f.controller.client = interceptor.NewClient(f.k8s.(client.WithWatch), interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return errors.New("cache unavailable")
		},
	})

	got := f.controller.mapServiceToTunnel(t.Context(), &corev1.Service{Name: "web", Namespace: "ns"})

	if want := tunnelRequests(true); !slices.Equal(got, want) {
		t.Errorf("mapServiceToTunnel() = %v, want %v", got, want)
	}
}

func tunnelRequests(enqueued bool) []reconcile.Request {
	if !enqueued {
		return nil
	}
	return []reconcile.Request{{Name: tunnelReconcileKey}}
}
