package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/go-logr/logr"
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

var (
	dummy          = struct{}{}
	socksProxyType = "socks"
)

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

func (c *Client) EnsureTunnelExists(ctx context.Context, logger logr.Logger) error {
	if c.tunnelID == "" {
		logger.Info("TunnelID not set, looking for an existing tunnel")

		tunnels, _, err := c.cloudflareAPI.ListTunnels(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.TunnelListParams{})
		if err != nil {
			logger.Error(err, "Failed to list tunnels")
			return err
		}

		for _, tunnel := range tunnels {
			if tunnel.DeletedAt != nil {
				// This is some deleted tunnel, skip it
				continue
			}
			if tunnel.Name == c.tunnelName {
				logger.Info("Cloudflare Tunnel found", "tunnelID", tunnel.ID)
				c.tunnelID = tunnel.ID
				return nil
			}
		}

		logger.Info("Cloudflare Tunnel not found, creating a new one")

		return c.createTunnel(ctx, logger)
	}

	tunnel, err := c.cloudflareAPI.GetTunnel(ctx, cloudflare.AccountIdentifier(c.accountID), c.tunnelID)
	if err != nil {
		logger.Error(err, "Failed to get the tunnel")
		return err
	}

	if tunnel.Name != c.tunnelName {
		logger.Error(errors.New("tunnel name mismatch"), "Tunnel name mismatch, this will force creation new tunnel, please review your configuration", "expected", c.tunnelName, "actual", tunnel.Name)
	}

	logger.Info("Tunnel exists")

	return nil
}

func (c *Client) createTunnel(ctx context.Context, logger logr.Logger) error {
	secret := make([]byte, 64)
	_, err := rand.Read(secret)
	if err != nil {
		logger.Error(err, "Failed to generate a secret for the tunnel")
		return err
	}

	hexSecret := hex.EncodeToString(secret)
	tunnel, err := c.cloudflareAPI.CreateTunnel(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.TunnelCreateParams{
		Name:      c.tunnelName,
		Secret:    hexSecret,
		ConfigSrc: "cloudflare",
	})
	if err != nil {
		logger.Error(err, "Failed to create a tunnel")
		return err
	}

	logger.Info("Cloudflare Tunnel created", "tunnelID", tunnel.ID)
	c.tunnelID = tunnel.ID
	return nil
}

func (c *Client) DeleteFromTunnelConfiguration(ctx context.Context, logger logr.Logger, ingressRecords *IngressRecords) error {
	if ingressRecords == nil {
		return nil
	}

	logger.Info("Deleting from Cloudflare Tunnel configuration")

	err := c.deleteFromTunnelConfiguration(ctx, logger, ingressRecords)
	if err != nil {
		return err
	}

	err = c.deleteFromDns(ctx, logger, ingressRecords)
	return err
}

func (c *Client) deleteFromTunnelConfiguration(ctx context.Context, logger logr.Logger, ingressRecords *IngressRecords) error {
	tc, err := c.cloudflareAPI.GetTunnelConfiguration(ctx, cloudflare.AccountIdentifier(c.accountID), c.tunnelID)
	if err != nil {
		logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	tunnelConfig := &tc.Config
	if tunnelConfig.Ingress == nil {
		tunnelConfig.Ingress = make([]cloudflare.UnvalidatedIngressRule, 0)
	}

	for _, ing := range *ingressRecords {
		for i, ingRule := range tunnelConfig.Ingress {
			// we are not checking the service, since it is not important when deleting
			if ingRule.Hostname == ing.Hostname && ingRule.Path == ing.Path {
				// remove idx-th element from the slice
				tunnelConfig.Ingress = append(tunnelConfig.Ingress[:i], tunnelConfig.Ingress[i+1:]...)
				break
			}
		}
	}

	c.flush404IfLast(tunnelConfig)

	_, err = c.cloudflareAPI.UpdateTunnelConfiguration(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.TunnelConfigurationParams{
		TunnelID: c.tunnelID,
		Config:   *tunnelConfig,
	})
	if err != nil {
		logger.Error(err, "Failed to update tunnel configuration", "tunnelConfig", tunnelConfig)
		return err
	}

	return nil
}

func (c *Client) deleteFromDns(ctx context.Context, logger logr.Logger, ingressRecords *IngressRecords) error {
	zone_map, err := c.getDnsZoneMap(ctx, logger)
	if err != nil {
		return err
	}

	zones_recods_cache := make(map[string]*[]cloudflare.DNSRecord)

	for _, ingress := range *ingressRecords {
		for zoneID, zoneName := range zone_map {
			if !strings.HasSuffix(ingress.Hostname, zoneName) {
				continue
			}
			if zone_records, ok := zones_recods_cache[zoneID]; !ok {
				dns_records, _, err := c.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
					Content: c.tunnelID + "." + tunnelDomain,
				})
				if err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zones_recods_cache[zoneID] = &dns_records
			} else {
				for _, record := range *zone_records {
					if record.Name != ingress.Hostname {
						continue
					}
					err := c.cloudflareAPI.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), record.ID)
					if err != nil {
						logger.Error(err, "Failed to delete DNS record")
						return err
					}
					break
				}
			}
			break
		}
	}

	return nil
}

func (c *Client) EnsureTunnelConfiguration(ctx context.Context, logger logr.Logger, config *Config) error {
	logger.Info("Ensuring Cloudflare Tunnel configuration")

	err := c.synchronizeTunnelConfiguration(ctx, logger, config)
	if err != nil {
		return err
	}

	err = c.synchronizeDns(ctx, logger, config)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) synchronizeTunnelConfiguration(ctx context.Context, logger logr.Logger, config *Config) error {
	tc, err := c.cloudflareAPI.GetTunnelConfiguration(ctx, cloudflare.AccountIdentifier(c.accountID), c.tunnelID)
	if err != nil {
		logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	tunnelConfig := &tc.Config

	proposed_ingress := make([]cloudflare.UnvalidatedIngressRule, 0)
	active_ingress := tunnelConfig.Ingress
	if active_ingress == nil {
		active_ingress = make([]cloudflare.UnvalidatedIngressRule, 0)
	}

	tunnelConfigUpdated := false
	record_id := 0
	for _, ingressRecords := range config.Ingresses {
		for _, ingressRecord := range *ingressRecords {
			var new_rule *cloudflare.UnvalidatedIngressRule
			if record_id >= len(active_ingress) {
				tunnelConfigUpdated = true
				new_rule = c.createIngressToTunnelConfigurationStruct(tunnelConfig, logger, ingressRecord)
			} else {
				new_rule = &active_ingress[record_id]
				if new_rule.Hostname != ingressRecord.Hostname || new_rule.Path != ingressRecord.Path || new_rule.Service != ingressRecord.Service {
					tunnelConfigUpdated = true
					new_rule = c.createIngressToTunnelConfigurationStruct(tunnelConfig, logger, ingressRecord)
				}
			}
			proposed_ingress = append(proposed_ingress, *new_rule)
			record_id++
		}
	}

	create_kube_api_tunnel := false
	if record_id < len(active_ingress) {
		new_rule := &active_ingress[record_id]
		if active_ingress[record_id].Hostname == config.KubernetesApiTunnelConfig.Domain && active_ingress[record_id].Service == config.KubernetesApiTunnelConfig.GetService() {
			if config.KubernetesApiTunnelConfig.Enabled {
				proposed_ingress = append(proposed_ingress, *new_rule)
			} else {
				tunnelConfigUpdated = true
			}
		} else {
			create_kube_api_tunnel = config.KubernetesApiTunnelConfig.Enabled
		}
	} else {
		create_kube_api_tunnel = config.KubernetesApiTunnelConfig.Enabled
	}

	if create_kube_api_tunnel {
		new_rule := cloudflare.UnvalidatedIngressRule{
			Hostname: config.KubernetesApiTunnelConfig.Domain,
			Service:  config.KubernetesApiTunnelConfig.GetService(),
		}

		var tmp *cloudflare.OriginRequestConfig
		if tmp = new_rule.OriginRequest; tmp == nil {
			tmp = &cloudflare.OriginRequestConfig{}
			new_rule.OriginRequest = tmp
		}

		tmp.ProxyType = &socksProxyType

		proposed_ingress = append(proposed_ingress, new_rule)
		tunnelConfigUpdated = true
	}

	if tunnelConfigUpdated {
		if len(proposed_ingress) > 0 {
			proposed_ingress = append(proposed_ingress, cloudflare.UnvalidatedIngressRule{
				Service: "http_status:404",
			})
		}

		tunnelConfig.Ingress = proposed_ingress

		_, err = c.cloudflareAPI.UpdateTunnelConfiguration(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.TunnelConfigurationParams{
			TunnelID: c.tunnelID,
			Config:   *tunnelConfig,
		})
		if err != nil {
			logger.Error(err, "Failed to update tunnel configuration", "tunnelConfig", tunnelConfig)
			return err
		}

		if config.KubernetesApiTunnelConfig.Enabled {
			err := c.ensureKubeApiApplication(ctx, logger, config)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Client) isInZone(hostname string, zoneName string) bool {
	return (hostname == zoneName) || strings.HasSuffix(hostname, "."+zoneName)
}

func (c *Client) synchronizeDns(ctx context.Context, logger logr.Logger, config *Config) error {

	zone_map, err := c.getDnsZoneMap(ctx, logger)
	if err != nil {
		return err
	}

	// determine which hostnames are in which zone
	zone_hostnames := make(map[string]map[string]struct{})
	zone_records := make(map[string]*[]cloudflare.DNSRecord)
	for _, ingressRecords := range config.Ingresses {
		for _, ingress := range *ingressRecords {
			zoneID := ""
			for zone, zone_id := range zone_map {
				if c.isInZone(ingress.Hostname, zone) {
					zoneID = zone_id
					break
				}
			}

			if len(zoneID) == 0 {
				logger.Info("Failed to find zone ID", "hostname", ingress.Hostname)
				continue
			}

			if _, ok := zone_hostnames[zoneID]; !ok {
				zone_hostnames[zoneID] = make(map[string]struct{})

				dns_records, _, err := c.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
					Content: c.tunnelID + "." + tunnelDomain,
				})
				if err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zone_records[zoneID] = &dns_records
			}
			zone_hostnames[zoneID][ingress.Hostname] = dummy
		}
	}

	if config.KubernetesApiTunnelConfig.Enabled {
		zoneID := ""
		for zone, zone_id := range zone_map {
			if c.isInZone(config.KubernetesApiTunnelConfig.Domain, zone) {
				zoneID = zone_id
				break
			}
		}

		if len(zoneID) == 0 {
			logger.Info("Failed to find zone ID", "hostname", config.KubernetesApiTunnelConfig.Domain)
		} else {
			if _, ok := zone_hostnames[zoneID]; !ok {
				zone_hostnames[zoneID] = make(map[string]struct{})

				dns_records, _, err := c.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
					Content: c.tunnelID + "." + tunnelDomain,
				})
				if err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zone_records[zoneID] = &dns_records
			}
			zone_hostnames[zoneID][config.KubernetesApiTunnelConfig.Domain] = dummy
		}
	}

	// create DNS records (if needed)
	for zoneID, hostnames := range zone_hostnames {
		hostname_list := make([]string, 0, len(hostnames))
		for k := range hostnames {
			hostname_list = append(hostname_list, k)
		}
		err = c.createDNSRecords(ctx, logger, zoneID, hostname_list)
		if err != nil {
			return err
		}
	}

	for zoneID, dns_records := range zone_records {
		valid_hostnames := zone_hostnames[zoneID]
		for _, record := range *dns_records {
			if _, ok := valid_hostnames[record.Name]; !ok {
				err = c.cloudflareAPI.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), record.ID)
				if err != nil {
					logger.Error(err, "Failed to delete DNS record")
					return err
				}
			}
		}
	}

	return nil
}

func (c *Client) createIngressToTunnelConfigurationStruct(tunnelConfig *cloudflare.TunnelConfiguration, logger logr.Logger, ingress *IngressConfig) *cloudflare.UnvalidatedIngressRule {
	logger.Info("Adding new ingress rule to tunnel configuration")

	newIngressRule := &cloudflare.UnvalidatedIngressRule{
		Hostname: ingress.Hostname,
		Path:     ingress.Path,
		Service:  ingress.Service,
	}

	if ingress.OriginConfig != nil {
		var tmp *cloudflare.OriginRequestConfig
		if tmp = newIngressRule.OriginRequest; tmp == nil {
			tmp = &cloudflare.OriginRequestConfig{}
			newIngressRule.OriginRequest = tmp
		}

		if connectTimeout := ingress.OriginConfig.ConnectTimeout; connectTimeout != nil {
			tmp.ConnectTimeout = &cloudflare.TunnelDuration{
				Duration: time.Duration(connectTimeout.Nanoseconds()),
			}
		}
		if tlsTimeout := ingress.OriginConfig.TLSTimeout; tlsTimeout != nil {
			tmp.TLSTimeout = &cloudflare.TunnelDuration{
				Duration: time.Duration(tlsTimeout.Nanoseconds()),
			}
		}
		if tcpKeepAlive := ingress.OriginConfig.TCPKeepAlive; tcpKeepAlive != nil {
			tmp.TCPKeepAlive = &cloudflare.TunnelDuration{
				Duration: time.Duration(tcpKeepAlive.Nanoseconds()),
			}
		}
		if keepAliveTimeout := ingress.OriginConfig.KeepAliveTimeout; keepAliveTimeout != nil {
			tmp.KeepAliveTimeout = &cloudflare.TunnelDuration{
				Duration: time.Duration(keepAliveTimeout.Nanoseconds()),
			}
		}
		tmp.NoHappyEyeballs = ingress.OriginConfig.NoHappyEyeballs
		tmp.KeepAliveConnections = ingress.OriginConfig.KeepAliveConnections
		tmp.HTTPHostHeader = ingress.OriginConfig.HTTPHostHeader
		tmp.OriginServerName = ingress.OriginConfig.OriginServerName
		tmp.NoTLSVerify = ingress.OriginConfig.NoTLSVerify
		tmp.DisableChunkedEncoding = ingress.OriginConfig.DisableChunkedEncoding
		tmp.BastionMode = ingress.OriginConfig.BastionMode
		tmp.ProxyAddress = ingress.OriginConfig.ProxyAddress
		tmp.ProxyPort = ingress.OriginConfig.ProxyPort
		tmp.ProxyType = ingress.OriginConfig.ProxyType
		tmp.Http2Origin = ingress.OriginConfig.Http2Origin
	}

	return newIngressRule
}

func (c *Client) getDnsZoneMap(ctx context.Context, logger logr.Logger) (map[string]string, error) {
	// get the zone id
	zones, err := c.cloudflareAPI.ListZones(ctx)
	if err != nil {
		logger.Error(err, "Failed to list zones")
		return nil, err
	}

	zoneMap := make(map[string]string)
	for _, zone := range zones {
		zoneMap[zone.Name] = zone.ID
	}

	return zoneMap, nil
}

func (c *Client) createDNSRecords(ctx context.Context, logger logr.Logger, zoneID string, hostnames []string) error {
	logger.Info("Creating new DNS record")

	truth := true

	// create the DNS records
	for _, hostname := range hostnames {
		_, err := c.cloudflareAPI.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.CreateDNSRecordParams{
			Type:    "CNAME",
			Proxied: &truth,
			Name:    hostname,
			Content: c.tunnelID + "." + tunnelDomain,
			Comment: "Automatically created by Cloudflare Tunnel Ingress Controller",
		})
		if err != nil {
			cfErr := &cloudflare.RequestError{}
			if errors.As(err, &cfErr) {
				for _, e := range cfErr.Errors() {
					if e.Code != 81053 {
						logger.Error(err, "Failed to create DNS record")
						return err
					}
				}
				// 81053: "Record already exists"
				continue
			}

			logger.Error(err, "Failed to create DNS record")
			return err
		}
	}

	return nil
}

func (c *Client) flush404IfLast(tunnelConfig *cloudflare.TunnelConfiguration) {
	if len(tunnelConfig.Ingress) == 1 && tunnelConfig.Ingress[0].Service == "http_status:404" {
		tunnelConfig.Ingress = make([]cloudflare.UnvalidatedIngressRule, 0)
	}
}

func (c *Client) ensureKubeApiApplication(ctx context.Context, logger logr.Logger, config *Config) error {
	apps, _, err := c.cloudflareAPI.ListAccessApplications(ctx, cloudflare.AccountIdentifier(c.accountID), cloudflare.ListAccessApplicationsParams{})
	if err != nil {
		logger.Error(err, "Failed to list Access Applications")
		return err
	}

	for _, app := range apps {
		if app.Domain == config.KubernetesApiTunnelConfig.Domain {
			return nil
		}
	}

	params := cloudflare.CreateAccessApplicationParams{
		Name:   config.KubernetesApiTunnelConfig.CloudflareAccessAppName,
		Domain: config.KubernetesApiTunnelConfig.Domain,
		Type:   cloudflare.SelfHosted,
	}

	_, err = c.cloudflareAPI.CreateAccessApplication(ctx, cloudflare.AccountIdentifier(c.accountID), params)
	return err
}
