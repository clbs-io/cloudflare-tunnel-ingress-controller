package controller

import (
	"context"
	"errors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"strings"
)

const appName = "cloudflare-tunnel-cloudflared"

type cloudflaredDeploymentConfig struct {
	cloudflaredImage           string
	cloudflaredImagePullPolicy string
	tunnelToken                string
}

func (c *IngressController) EnsureCloudflaredDeploymentExists(ctx context.Context, logger logr.Logger) error {
	logger.Info("Ensuring Cloudflared Deployment exists")

	foundDeployment := &appsv1.Deployment{}
	ns := namespace()

	err := c.client.Get(ctx, types.NamespacedName{Name: appName, Namespace: ns}, foundDeployment)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating a new Cloudflared Deployment resource")

		err = c.createAndDeployCloudflaredDeployment(ctx, logger)
		if err != nil {
			logger.Error(err, "Failed to create a new Cloudflared Deployment resource")
			return err
		}

		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get Cloudflared Deployment resource")
		return err
	}

	return err
}

func (c *IngressController) createAndDeployCloudflaredDeployment(ctx context.Context, logger logr.Logger) error {
	logger.Info("Creating Cloudflared Deployment resource")

	deployment, err := c.newCloudflaredDeployment(ctx, logger)
	if err != nil {
		logger.Error(err, "Failed to create Cloudflared Deployment resource")
		return err
	}

	err = c.client.Create(ctx, deployment)
	if err != nil {
		logger.Error(err, "Failed to create Cloudflared Deployment resource")
		return err
	}

	logger.Info("Created Cloudflared Deployment resource")

	return nil
}

func (c *IngressController) newCloudflaredDeployment(ctx context.Context, logger logr.Logger) (*appsv1.Deployment, error) {
	replicas := int32(1)
	ns := namespace()

	cloudflaredVersion := strings.Split(c.cloudflaredDeploymentConfig.cloudflaredImage, ":")[1]
	if cloudflaredVersion == "" || cloudflaredVersion == "latest" {
		return nil, errors.New("cloudflared image version is required, latest is not allowed")
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       appName,
		"app.kubernetes.io/managed-by": "cloudflare-tunnel-ingress-controller",
		"app.kubernetes.io/component":  "cloudflared",
		"app.kubernetes.io/part-of":    "cloudflare-tunnel-ingress-controller",
		"app.kubernetes.io/version":    cloudflaredVersion,
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   appName,
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            appName,
							Image:           c.cloudflaredDeploymentConfig.cloudflaredImage,
							ImagePullPolicy: corev1.PullPolicy(c.cloudflaredDeploymentConfig.cloudflaredImagePullPolicy),
							Command: []string{
								"cloudflared",
								"--no-autoupdate",
								"tunnel",
								"--metrics",
								"0.0.0.0:9090",
								"run",
								"--token",
								c.cloudflaredDeploymentConfig.tunnelToken,
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}

	return deployment, nil
}

func (c *IngressController) updateCloudflaredDeploymentIfNeeded() error {
	// TODO
	return errors.New("not implemented")
}
