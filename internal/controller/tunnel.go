package controller

import (
	"context"
	"github.com/go-logr/logr"
)

func (c *IngressController) SetTunnelToken(token string) {
	c.cloudflaredDeploymentConfig.tunnelToken = token
}

func (c *IngressController) ensureCloudflareTunnelExists(ctx context.Context, logger logr.Logger) error {
	logger.Info("Ensuring Cloudflare Tunnel exists")
	err := c.tunnelClient.EnsureTunnelExists(ctx)
	if err != nil {
		logger.Error(err, "Failed to ensure Cloudflare Tunnel exists")
		return err
	}

	token, err := c.tunnelClient.GetTunnelToken(ctx)
	if err != nil {
		logger.Error(err, "Failed to get Cloudflare Tunnel token")
		return err
	}

	c.cloudflaredDeploymentConfig.tunnelToken = token
	return nil
}
