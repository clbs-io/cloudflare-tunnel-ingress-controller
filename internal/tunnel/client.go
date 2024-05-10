package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/cloudflare/cloudflare-go"
	"github.com/go-logr/logr"
)

type Client struct {
	logger logr.Logger

	cloudflareAPI *cloudflare.API
	accountID     string
	tunnelName    string

	tunnelID    string
	tunnelToken string
}

func NewClient(cloudflareAPI *cloudflare.API, accountID, tunnelName string, logger logr.Logger) *Client {
	return &Client{
		logger:        logger,
		cloudflareAPI: cloudflareAPI,
		accountID:     accountID,
		tunnelName:    tunnelName,
	}
}

func (c *Client) GetTunnelToken(ctx context.Context) (string, error) {
	if c.tunnelToken == "" {
		return c.cloudflareAPI.GetTunnelToken(ctx, cloudflare.ResourceIdentifier(c.accountID), c.tunnelID)
	}

	return c.tunnelToken, nil
}

func (c *Client) EnsureTunnelExists(ctx context.Context) error {
	if c.tunnelID == "" {
		c.logger.Info("TunnelID not set, looking for an existing tunnel")

		tunnels, _, err := c.cloudflareAPI.ListTunnels(ctx, cloudflare.ResourceIdentifier(c.accountID), cloudflare.TunnelListParams{})
		if err != nil {
			c.logger.Error(err, "Failed to list tunnels")
			return err
		}

		for _, tunnel := range tunnels {
			if tunnel.Name == c.tunnelName {
				c.logger.Info("Cloudflare Tunnel found", "tunnelID", tunnel.ID)
				c.tunnelID = tunnel.ID
				return nil
			}
		}

		c.logger.Info("Cloudflare Tunnel not found, creating a new one")

		return c.createTunnel(ctx)
	}

	tunnel, err := c.cloudflareAPI.GetTunnel(ctx, cloudflare.ResourceIdentifier(c.accountID), c.tunnelID)
	if err != nil {
		c.logger.Error(err, "Failed to get the tunnel")
		return err
	}

	if tunnel.Name != c.tunnelName {
		// TODO: rename tunnel back to the original name

		//tunnel, err := c.cloudflareAPI.UpdateTunnelConfiguration(ctx, cloudflare.ResourceIdentifier(c.tunnelID), cloudflare.TunnelConfigurationParams{
		//	TunnelID: c.tunnelID,
		//	Config: cloudflare.TunnelConfiguration{
		//		Name: c.tunnelName,
		//	}
		//})
	}

	c.logger.Info("Tunnel exists")

	return nil
}

func (c *Client) createTunnel(ctx context.Context) error {
	secret := make([]byte, 64)
	_, err := rand.Read(secret)
	if err != nil {
		c.logger.Error(err, "Failed to generate a secret for the tunnel")
		return err
	}

	hexSecret := hex.EncodeToString(secret)
	tunnel, err := c.cloudflareAPI.CreateTunnel(ctx, cloudflare.ResourceIdentifier(c.accountID), cloudflare.TunnelCreateParams{
		Name:      c.tunnelName,
		Secret:    hexSecret,
		ConfigSrc: "cloudflare",
	})
	if err != nil {
		c.logger.Error(err, "Failed to create a tunnel")
		return err
	}

	c.logger.Info("Cloudflare Tunnel created", "tunnelID", tunnel.ID)
	c.tunnelID = tunnel.ID
	return nil
}
