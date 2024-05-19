package controller

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/cybroslabs/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type IngressController struct {
	logger logr.Logger

	client       client.Client
	tunnelClient *tunnel.Client

	ingressClassName    string
	controllerClassName string

	cloudflaredDeploymentConfig cloudflaredDeploymentConfig

	tunnelConfigLck sync.Mutex
	tunnelConfig    *tunnel.Config
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
		tunnelConfigLck: sync.Mutex{},
		tunnelConfig: &tunnel.Config{
			Ingresses: make(map[types.UID]*tunnel.IngressRecords),
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

	ingress := &networkingv1.Ingress{}
	err = c.client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, ingress)
	if apierrors.IsNotFound(err) {
		reqLogger.Info("Ingress resource not found")
		return ctrl.Result{}, nil
	}
	if err != nil {
		reqLogger.Error(err, "failed to get ingress resource")
		return ctrl.Result{}, err
	}

	if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName != c.ingressClassName {
		// This has an ingress class that we don't care about
		return ctrl.Result{}, nil
	}

	if ingress.GetDeletionTimestamp() != nil {
		c.tunnelConfigLck.Lock()
		defer c.tunnelConfigLck.Unlock()
		err = c.finalizeIngress(ctx, reqLogger, c.tunnelConfig, ingress)
		return ctrl.Result{}, err
	}

	err = c.ensureFinalizers(ctx, reqLogger, ingress)
	if err != nil {
		reqLogger.Error(err, "failed to ensure finalizers on ingress resource")
		return ctrl.Result{}, err
	}

	c.tunnelConfigLck.Lock()
	defer c.tunnelConfigLck.Unlock()
	err = c.ensureCloudflareTunnelConfiguration(ctx, reqLogger, c.tunnelConfig, ingress)
	if err != nil {
		reqLogger.Error(err, "failed to ensure tunnel configuration")
		return ctrl.Result{}, err
	}

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
