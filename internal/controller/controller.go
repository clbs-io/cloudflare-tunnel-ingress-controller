package controller

import (
	"context"
	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

type IngressController struct {
	logger logr.Logger

	client       client.Client
	tunnelClient *tunnel.Client

	ingressClassName    string
	controllerClassName string

	cloudflaredDeploymentConfig cloudflaredDeploymentConfig
}

type CloudflaredConfig struct {
	CloudflaredImage           string
	CloudflaredImagePullPolicy string
}

func NewIngressController(logger logr.Logger, client client.Client, tunnelClient *tunnel.Client, ingressClassName, controllerClassName string, cloudflaredConfig CloudflaredConfig) *IngressController {
	return &IngressController{
		logger:              logger,
		client:              client,
		tunnelClient:        tunnelClient,
		ingressClassName:    ingressClassName,
		controllerClassName: controllerClassName,
		cloudflaredDeploymentConfig: cloudflaredDeploymentConfig{
			cloudflaredImage:           cloudflaredConfig.CloudflaredImage,
			cloudflaredImagePullPolicy: cloudflaredConfig.CloudflaredImagePullPolicy,
		},
	}
}

func (c *IngressController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.FromContext(ctx)

	var err error

	err = c.ensureCloudflareTunnelExists(ctx, reqLogger)
	if err != nil {
		reqLogger.Error(err, "failed to ensure cloudflare tunnel exists")
		return ctrl.Result{}, err
	}

	err = c.EnsureCloudflaredDeploymentExists(ctx, reqLogger)
	if err != nil {
		reqLogger.Error(err, "failed to ensure cloudflared deployment exists")
		return ctrl.Result{}, err
	}

	ing := &networkingv1.Ingress{}
	err = c.client.Get(ctx, req.NamespacedName, ing)
	if apierrors.IsNotFound(err) {
		reqLogger.Info("Ingress resource not found")
		return ctrl.Result{}, nil
	}

	if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName != c.ingressClassName {
		reqLogger.Info("Ingress resource does not have the correct class name")
		return ctrl.Result{}, nil
	}

	if ing.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, c.finalizeIngress(ctx, reqLogger, ing)
	}

	err = c.ensureFinalizers(ctx, reqLogger, ing)
	if err != nil {
		reqLogger.Error(err, "failed to ensure finalizers on ingress resource")
		return ctrl.Result{}, err
	}

	err = c.ensureCloudflareTunnelConfiguration(ctx, reqLogger, ing)
	if err != nil {
		reqLogger.Error(err, "failed to ensure tunnel configuration")
		return ctrl.Result{}, err

	}

	// TODO
	// - update tunnel configuration based on ingress resources
	// - emit Kubernetes events based on the status of the tunnel

	return ctrl.Result{}, nil
}

func namespace() string {
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns
		}
	}
	return "default"
}
