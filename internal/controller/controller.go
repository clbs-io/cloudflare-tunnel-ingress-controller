// Package controller turns the Ingresses of one IngressClass into the
// desired state of a Cloudflare Tunnel and drives it there.
//
// Every watched event maps to one reconcile request (see bootstrap.go), and
// each run rebuilds the complete desired state from the cache: render.go
// translates Ingresses into ordered cloudflared rules, package tunnel pushes
// them and the DNS records to Cloudflare, and cloudflared.go keeps the token
// Secret for the chart's cloudflared Deployment. Apart from the tunnel ID and
// token, nothing is remembered between runs, so a restart, a missed event or
// an edit made in the Cloudflare dashboard is corrected by the next run.
package controller

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// tunnelReconcileKey is the single request every watched event maps to.
const tunnelReconcileKey = "tunnel"

// IngressController reconciles the whole tunnel. Construct it with
// NewIngressController or RegisterIngressController.
type IngressController struct {
	logger logr.Logger

	// client reads through the manager's cache, backed by cluster-wide
	// informers for Ingresses, IngressClasses and Services.
	client       client.Client
	recorder     events.EventRecorder
	tunnelClient *tunnel.Client

	ingressClassName    string
	controllerClassName string
	resyncPeriod        time.Duration
	kubernetesApiTunnel KubernetesApiTunnelConfig

	// reader reads Secrets and Deployments directly from the API server, so
	// no informer caches them.
	reader client.Reader

	tunnelTokenSecret     string
	cloudflaredDeployment string
}

var (
	_namespaceOnce sync.Once
	_namespace     string
)

// NewIngressController builds the controller from options. The Kubernetes API
// tunnel settings come from the KUBERNETES_API_TUNNEL_* environment variables.
func NewIngressController(logger logr.Logger, client client.Client, reader client.Reader, recorder events.EventRecorder, options IngressControllerOptions) *IngressController {
	kubernetes_api_tunnel_enabled, _ := env.GetBool("KUBERNETES_API_TUNNEL_ENABLED", false)

	return &IngressController{
		logger:              logger,
		client:              client,
		reader:              reader,
		recorder:            recorder,
		tunnelClient:        options.TunnelClient,
		ingressClassName:    options.IngressClassName,
		controllerClassName: options.ControllerClassName,
		resyncPeriod:        options.ResyncPeriod,
		kubernetesApiTunnel: KubernetesApiTunnelConfig{
			Enabled:                 kubernetes_api_tunnel_enabled,
			Server:                  os.Getenv("KUBERNETES_API_TUNNEL_SERVER"),
			Domain:                  os.Getenv("KUBERNETES_API_TUNNEL_DOMAIN"),
			CloudflareAccessAppName: os.Getenv("KUBERNETES_API_TUNNEL_CF_ACCESS_APP_NAME"),
		},
		tunnelTokenSecret:     options.TunnelTokenSecret,
		cloudflaredDeployment: options.CloudflaredDeployment,
	}
}

// Reconcile renders the whole tunnel from all Ingresses and synchronizes it.
// Every event maps to the same request, so the request itself is unused and
// runs never overlap (MaxConcurrentReconciles is 1); that is why the
// controller needs no locks.
//
// The order of the steps carries the guarantees:
//   - an Ingress gets the finalizer before its routes are published, so it
//     cannot disappear without the controller removing its routes;
//   - finalizers of released Ingresses are removed only after a successful
//     sync, so a failed Cloudflare update is retried while they still exist;
//   - Access applications come last, so an Access failure retries the run
//     without holding back releases and status.
//
// Errors go back to controller-runtime for retry with backoff. Content that
// cannot be published never fails the run; render reports it as Warning
// Events on its Ingress.
func (c *IngressController) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	token, err := c.ensureCloudflareTunnelExists(ctx, logger)
	if err != nil {
		return ctrl.Result{}, err
	}
	legacy_pending, err := c.ensureCloudflared(ctx, logger, token)
	if err != nil {
		return ctrl.Result{}, err
	}

	ingresses := &networkingv1.IngressList{}
	if err := c.client.List(ctx, ingresses); err != nil {
		logger.Error(err, "Failed to list Ingress resources")
		return ctrl.Result{}, err
	}
	active, releasing, err := c.classifyIngresses(ctx, ingresses.Items)
	if err != nil {
		logger.Error(err, "Failed to classify Ingress resources")
		return ctrl.Result{}, err
	}

	for i := range active {
		if err := c.ensureFinalizers(ctx, logger, &active[i]); err != nil {
			return ctrl.Result{}, err
		}
	}

	rendered := render(active, c.kubernetesApiTunnel, c.servicePortLookup(ctx))
	config := &tunnel.Config{Rules: rendered.Rules, AccessAppRequests: rendered.AccessAppRequests}

	synced, err := c.tunnelClient.Sync(ctx, logger, config)
	if err != nil {
		return ctrl.Result{}, err
	}

	for i := range releasing {
		if err := c.releaseIngress(ctx, logger, &releasing[i]); err != nil {
			return ctrl.Result{}, err
		}
	}

	for i := range active {
		ingress := &active[i]
		result := rendered.Results[ingress.UID]
		hostnames := make([]string, 0, len(result.Hostnames))
		for _, hostname := range result.Hostnames {
			if slices.Contains(synced.DNSConflicts, hostname) {
				result.warn(ReasonDNSConflict, "DNS name %q is held by a record that does not point to the tunnel", hostname)
				continue
			}
			hostnames = append(hostnames, hostname)
		}
		if err := c.ensureStatus(ctx, logger, ingress, hostnames); err != nil {
			return ctrl.Result{}, err
		}
		for _, w := range result.Warnings {
			c.recorder.Eventf(ingress, nil, corev1.EventTypeWarning, w.Reason, "Reconcile", "%s", w.Message)
		}
	}

	// Access applications apply to a hostname whatever serves it, so none is
	// created for a name held by a record that does not point to the tunnel
	maps.DeleteFunc(config.AccessAppRequests, func(hostname, _ string) bool {
		return slices.Contains(synced.DNSConflicts, hostname)
	})
	if err := c.tunnelClient.EnsureAccessApplications(ctx, logger, config); err != nil {
		return ctrl.Result{}, err
	}

	requeue := c.resyncPeriod
	if legacy_pending {
		requeue = legacyCloudflaredRequeue
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// classifyIngresses splits Ingresses into active ones of our class and
// releasing ones that carry our finalizer but are being deleted, or have moved
// to a class no controller with our controller class serves. Ingresses of
// another installation of this controller keep their finalizer.
func (c *IngressController) classifyIngresses(ctx context.Context, ingresses []networkingv1.Ingress) ([]networkingv1.Ingress, []networkingv1.Ingress, error) {
	var active, releasing []networkingv1.Ingress
	for _, ingress := range ingresses {
		switch {
		case c.hasOurClass(&ingress) && ingress.DeletionTimestamp == nil:
			active = append(active, ingress)
		case !slices.Contains(ingress.Finalizers, ingressTunnelFinalizer):
			// not ours, or already released
		case c.hasOurClass(&ingress):
			releasing = append(releasing, ingress)
		default:
			served, err := c.classServedByUs(ctx, ingress.Spec.IngressClassName)
			if err != nil {
				return nil, nil, err
			}
			if !served {
				releasing = append(releasing, ingress)
			}
		}
	}
	return active, releasing, nil
}

func (c *IngressController) hasOurClass(ingress *networkingv1.Ingress) bool {
	return ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == c.ingressClassName
}

// classServedByUs reports whether the IngressClass exists and names our
// controller class.
func (c *IngressController) classServedByUs(ctx context.Context, className *string) (bool, error) {
	if className == nil {
		return false, nil
	}
	class := &networkingv1.IngressClass{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: *className}, class); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get IngressClass %q: %w", *className, err)
	}
	return class.Spec.Controller == c.controllerClassName, nil
}

// servicePortLookup resolves named Service ports for render from the cache.
// A missing Service or port only skips the rule that references it.
func (c *IngressController) servicePortLookup(ctx context.Context) portLookup {
	return func(namespace, name, port string) (int32, error) {
		service := &corev1.Service{}
		if err := c.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, service); err != nil {
			return 0, err
		}
		for _, p := range service.Spec.Ports {
			if p.Name == port {
				return p.Port, nil
			}
		}
		return 0, fmt.Errorf("service %s/%s has no port named %q", namespace, name, port)
	}
}

// Namespace is the namespace of the controller, its leader-election lease, the
// token Secret and the cloudflared Deployment: NAMESPACE, which the chart sets
// from the pod's own namespace, or "default".
func Namespace() string {
	_namespaceOnce.Do(func() {
		_namespace = "default"
		if ns := os.Getenv("NAMESPACE"); len(ns) > 0 {
			_namespace = ns
		}
	})
	return _namespace
}
