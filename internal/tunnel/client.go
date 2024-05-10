package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/cloudflare/cloudflare-go"
	"github.com/go-logr/logr"
	"strings"
)

const tunnelDomain = "cfargotunnel.com"

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
		return c.cloudflareAPI.GetTunnelToken(ctx, cloudflare.AccountIdentifier(c.accountID), c.tunnelID)
	}

	return c.tunnelToken, nil
}

func (c *Client) EnsureTunnelExists(ctx context.Context) error {
	if c.tunnelID == "" {
		c.logger.Info("TunnelID not set, looking for an existing tunnel")

		tunnels, _, err := c.cloudflareAPI.ListTunnels(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.TunnelListParams{})
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

	tunnel, err := c.cloudflareAPI.GetTunnel(ctx, cloudflare.AccountIdentifier(c.accountID), c.tunnelID)
	if err != nil {
		c.logger.Error(err, "Failed to get the tunnel")
		return err
	}

	if tunnel.Name != c.tunnelName {
		// TODO: rename tunnel back to the original name

		//tunnel, err := c.cloudflareAPI.UpdateTunnelConfiguration(ctx, cloudflare.AccountIdentifier(c.tunnelID), cloudflare.TunnelConfigurationParams{
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
	tunnel, err := c.cloudflareAPI.CreateTunnel(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.TunnelCreateParams{
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

func (c *Client) EnsureTunnelConfiguration(ctx context.Context, logger logr.Logger, config Config) error {
	logger.Info("Ensuring Cloudflare Tunnel configuration")

	tc, err := c.cloudflareAPI.GetTunnelConfiguration(ctx, cloudflare.AccountIdentifier(c.accountID), c.tunnelID)
	if err != nil {
		logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	tunnelConfig := tc.Config

	newHostname := make(map[string]bool)
	deletedHostname := make(map[string]bool)

	for _, ing := range config.Ingresses {
		newHostname[ing.Hostname] = true
		deletedHostname[ing.Hostname] = true

		for _, ingRule := range tc.Config.Ingress {
			// is it new hostname?
			// if so, we should add it to the configuration
			if ingRule.Hostname == ing.Hostname {
				newHostname[ing.Hostname] = false
				deletedHostname[ing.Hostname] = false
			}

			// is it the same rule? -> skip
			if ingRule.Hostname == ing.Hostname && ingRule.Path == ing.Path && ingRule.Service == ing.Service {
				continue
			}
		}
	}

	// create new hostnames
	for hostname, isNew := range newHostname {
		if !isNew {
			continue
		}

		for i, ing := range config.Ingresses {
			if ing.Hostname == hostname {
				err = c.addNewIngressToTunnelConfigurationStructAndCreateDNSRecord(ctx, logger, &tunnelConfig, config.Ingresses[i])
				if err != nil {
					logger.Error(err, "Failed to add new ingress rule to tunnel configuration")
					return err
				}

				break
			}
		}
	}

	// delete hostnames
	for hostname, isDeleted := range deletedHostname {
		if !isDeleted {
			continue
		}

		for i, ing := range config.Ingresses {
			if ing.Hostname == hostname {
				err = c.deleteIngressFromTunnelConfigurationStructAndDeleteDNSRecord(ctx, logger, &tunnelConfig, config.Ingresses[i])
				if err != nil {
					logger.Error(err, "Failed to delete ingress rule from tunnel configuration")
					return err
				}

				break
			}
		}
	}

	_, err = c.cloudflareAPI.UpdateTunnelConfiguration(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.TunnelConfigurationParams{
		TunnelID: c.tunnelID,
		Config:   tc.Config,
	})
	if err != nil {
		logger.Error(err, "Failed to update tunnel configuration")
		return err
	}

	return nil
}

func (c *Client) addNewIngressToTunnelConfigurationStructAndCreateDNSRecord(ctx context.Context, logger logr.Logger, tunnelConfig *cloudflare.TunnelConfiguration, ingress IngressConfig) error {
	logger.Info("Adding new ingress rule to tunnel configuration")

	newIngressRule := cloudflare.UnvalidatedIngressRule{
		Hostname: ingress.Hostname,
		Path:     ingress.Path,
		Service:  ingress.Service,
	}

	tunnelConfig.Ingress = append(tunnelConfig.Ingress, newIngressRule)

	logger.Info("Added new ingress rule to tunnel configuration, creating new DNS record")
	err := c.createDNSRecord(ctx, logger, ingress)
	if err != nil {
		logger.Error(err, "Failed to create DNS record")
		return err
	}

	return nil
}

func (c *Client) deleteIngressFromTunnelConfigurationStructAndDeleteDNSRecord(ctx context.Context, logger logr.Logger, tunnelConfig *cloudflare.TunnelConfiguration, ingress IngressConfig) error {
	logger.Info("Deleting ingress rule from tunnel configuration")

	var idx int
	for i, rule := range tunnelConfig.Ingress {
		if rule.Hostname == ingress.Hostname && rule.Path == ingress.Path && rule.Service == ingress.Service {
			idx = i
			break
		}
	}

	// remove idx-th element from the slice
	tunnelConfig.Ingress = append(tunnelConfig.Ingress[:idx], tunnelConfig.Ingress[idx+1:]...)

	logger.Info("Deleted ingress rule from tunnel configuration, deleting DNS record")

	err := c.deleteDNSRecord(ctx, logger, ingress)
	if err != nil {
		logger.Error(err, "Failed to delete DNS record")
		return err
	}

	return nil
}

func (c *Client) createDNSRecord(ctx context.Context, logger logr.Logger, ingress IngressConfig) error {
	logger.Info("Creating new DNS record")

	truth := true

	// get the zone id
	zones, err := c.cloudflareAPI.ListZones(ctx)
	if err != nil {
		logger.Error(err, "Failed to list zones")
		return err
	}

	zoneID := ""
	for _, zone := range zones {
		if strings.HasSuffix(ingress.Hostname, zone.Name) {
			zoneID = zone.ID
			break
		}
	}

	if zoneID == "" {
		logger.Error(err, "Failed to find zone ID")
		return err
	}

	// create the DNS record
	_, err = c.cloudflareAPI.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.CreateDNSRecordParams{
		ZoneID:  zoneID,
		Type:    "CNAME",
		Proxied: &truth,
		Name:    ingress.Hostname,
		Content: c.tunnelID + "." + tunnelDomain,
		Comment: "Automatically created by Cloudflare Tunnel Ingress Controller",
	})
	if err != nil {
		logger.Error(err, "Failed to create DNS record")
		return err
	}

	return nil
}

func (c *Client) deleteDNSRecord(ctx context.Context, logger logr.Logger, ingress IngressConfig) error {
	logger.Info("Deleting new DNS record")

	// get the zone id
	zones, err := c.cloudflareAPI.ListZones(ctx)
	if err != nil {
		logger.Error(err, "Failed to list zones")
		return err
	}

	zoneID := ""
	for _, zone := range zones {
		if strings.HasSuffix(ingress.Hostname, zone.Name) {
			zoneID = zone.ID
			break
		}
	}

	if zoneID == "" {
		logger.Error(err, "Failed to find zone ID")
		return err
	}

	// get the DNS record ID
	records, _, err := c.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
		Content: c.tunnelID + "." + tunnelDomain,
	})
	if err != nil {
		logger.Error(err, "Failed to list DNS records")
		return err
	}

	if len(records) == 0 {
		logger.Info("No DNS record found")
		return nil
	}

	for _, record := range records {
		if record.Name == ingress.Hostname {
			err = c.cloudflareAPI.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), record.ID)
			if err != nil {
				logger.Error(err, "Failed to delete DNS record")
				return err
			}

			break
		}
	}

	return nil
}
