package controller

import (
	"context"
	"os"
	"sync"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
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

	tunnelConfigLck         sync.Mutex
	tunnelConfigInitialized bool
	tunnelConfig            *tunnel.Config
}

type CloudflaredConfig struct {
	CloudflaredImage           string
	CloudflaredImagePullPolicy string
}

var (
	_namespace = ""
)

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
		tunnelConfigLck:         sync.Mutex{},
		tunnelConfigInitialized: false,
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

	c.tunnelConfigLck.Lock()
	defer c.tunnelConfigLck.Unlock()

	// Load all ingress resources on the first reconcile
	if !c.tunnelConfigInitialized {
		c.tunnelConfigInitialized = true
		ingress_list := &networkingv1.IngressList{}
		err = c.client.List(ctx, ingress_list)
		if err != nil {
			reqLogger.Error(err, "failed to list ingress resources")
			return ctrl.Result{}, err
		} else {
			for _, ing := range ingress_list.Items {
				if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName != c.ingressClassName {
					continue
				}
				if ing.GetDeletionTimestamp() != nil {
					continue
				}
				err = c.harvestRules(ctx, reqLogger, c.tunnelConfig, &ing)
				if err != nil {
					reqLogger.Error(err, "failed to harvest rules")
					return ctrl.Result{}, err
				}
			}
		}
	}

	if ingress.GetDeletionTimestamp() != nil {
		err = c.finalizeIngress(ctx, reqLogger, c.tunnelConfig, ingress)
		return ctrl.Result{}, err
	}

	err = c.ensureFinalizers(ctx, reqLogger, ingress)
	if err != nil {
		reqLogger.Error(err, "failed to ensure finalizers on ingress resource")
		return ctrl.Result{}, err
	}

	err = c.ensureCloudflareTunnelConfiguration(ctx, reqLogger, c.tunnelConfig, ingress)
	if err != nil {
		reqLogger.Error(err, "failed to ensure tunnel configuration")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func namespace() string {
	if _namespace == "" {
		_namespace = "default"

		ns := os.Getenv("NAMESPACE")
		if len(ns) > 0 {
			_namespace = ns
		}
	}

	return _namespace
}
