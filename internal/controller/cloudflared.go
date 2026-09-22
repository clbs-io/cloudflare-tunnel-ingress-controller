package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// legacyCloudflaredDeployment is a controller-created cloudflared
	// Deployment that the chart's Deployment replaces.
	legacyCloudflaredDeployment = "cloudflare-tunnel-cloudflared"
	// legacyCloudflaredRequeue is how soon the reconcile runs again while the
	// legacy Deployment waits for the chart's Deployment to become available.
	legacyCloudflaredRequeue = time.Minute

	// managedByLabel and managedByValue mark the token Secret, and identify
	// the legacy Deployment as one the controller created.
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "cloudflare-tunnel-ingress-controller"
	// tokenHashAnnotation on the chart Deployment's pod template holds
	// tokenHash(token); a new value rolls the pods onto the new token.
	tokenHashAnnotation = "cloudflare-tunnel-ingress-controller.clbs.io/token-hash"
	// tunnelTokenKey is the Secret key the pods mount as their token file.
	tunnelTokenKey = "token"
)

// ensureCloudflared prepares the chart's cloudflared Deployment for the tunnel
// token and reports whether the legacy Deployment is still waiting to be
// replaced.
func (c *IngressController) ensureCloudflared(ctx context.Context, logger logr.Logger, token string) (bool, error) {
	if err := c.ensureTunnelTokenSecret(ctx, logger, token); err != nil {
		logger.Error(err, "Failed to ensure the tunnel token Secret", "name", c.tunnelTokenSecret)
		return false, err
	}

	found, available, err := c.ensureCloudflaredRollout(ctx, logger, token)
	if err != nil {
		logger.Error(err, "Failed to annotate the cloudflared Deployment", "name", c.cloudflaredDeployment)
		return false, err
	}
	if !found {
		return false, nil
	}

	// A chart Deployment named like the legacy one is the live cloudflared
	// Deployment, never the leftover to migrate away from.
	if c.cloudflaredDeployment == legacyCloudflaredDeployment {
		return false, nil
	}

	pending, err := c.cleanupLegacyCloudflared(ctx, logger, available)
	if err != nil {
		logger.Error(err, "Failed to delete the legacy cloudflared Deployment", "name", legacyCloudflaredDeployment)
		return false, err
	}
	return pending, nil
}

// ensureTunnelTokenSecret keeps the Secret the cloudflared pods mount equal to
// the tunnel token.
func (c *IngressController) ensureTunnelTokenSecret(ctx context.Context, logger logr.Logger, token string) error {
	key := types.NamespacedName{Namespace: Namespace(), Name: c.tunnelTokenSecret}
	secret := &corev1.Secret{}
	err := c.reader.Get(ctx, key, secret)
	if apierrors.IsNotFound(err) {
		logger.Info("Creating the tunnel token Secret", "name", key.Name)
		return c.client.Create(ctx, &corev1.Secret{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    map[string]string{managedByLabel: managedByValue},
			Type:      corev1.SecretTypeOpaque,
			Data:      map[string][]byte{tunnelTokenKey: []byte(token)},
		})
	}
	if err != nil {
		return err
	}

	if string(secret.Data[tunnelTokenKey]) == token {
		return nil
	}
	logger.Info("Updating the tunnel token Secret", "name", key.Name)
	secret.Data = map[string][]byte{tunnelTokenKey: []byte(token)}
	return c.client.Update(ctx, secret)
}

// ensureCloudflaredRollout keeps the token hash annotation on the pod template
// of the chart's cloudflared Deployment, so a token change rolls the pods. It
// reports whether the Deployment exists and has an available replica.
func (c *IngressController) ensureCloudflaredRollout(ctx context.Context, logger logr.Logger, token string) (bool, bool, error) {
	deployment := &appsv1.Deployment{}
	err := c.reader.Get(ctx, types.NamespacedName{Namespace: Namespace(), Name: c.cloudflaredDeployment}, deployment)
	if apierrors.IsNotFound(err) {
		logger.Info("The cloudflared Deployment does not exist", "name", c.cloudflaredDeployment)
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}

	hash := tokenHash(token)
	if deployment.Spec.Template.Annotations[tokenHashAnnotation] != hash {
		logger.Info("Rolling out cloudflared for the current tunnel token", "name", deployment.Name)
		patch := client.MergeFrom(deployment.DeepCopy())
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string, 1)
		}
		deployment.Spec.Template.Annotations[tokenHashAnnotation] = hash
		if err := c.client.Patch(ctx, deployment, patch); err != nil {
			return true, false, err
		}
	}

	return true, deployment.Status.AvailableReplicas > 0, nil
}

// cleanupLegacyCloudflared deletes the legacy cloudflared Deployment once the
// chart's Deployment is available, and reports whether it still waits for
// that. A Deployment of that name without our managed-by label is left alone.
func (c *IngressController) cleanupLegacyCloudflared(ctx context.Context, logger logr.Logger, available bool) (bool, error) {
	legacy := &appsv1.Deployment{}
	err := c.reader.Get(ctx, types.NamespacedName{Namespace: Namespace(), Name: legacyCloudflaredDeployment}, legacy)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if legacy.Labels[managedByLabel] != managedByValue {
		return false, nil
	}

	if !available {
		logger.Info("Keeping the legacy cloudflared Deployment until the chart's Deployment is available", "name", legacy.Name)
		return true, nil
	}

	logger.Info("Deleting the legacy cloudflared Deployment", "name", legacy.Name)
	err = c.client.Delete(ctx, legacy, client.PropagationPolicy(metav1.DeletePropagationBackground))
	return false, client.IgnoreNotFound(err)
}

// tokenHash identifies a token in the pod template without revealing it.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:16]
}
