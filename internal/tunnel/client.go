package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	"github.com/cloudflare/cloudflare-go/v4/zones"
	"github.com/go-logr/logr"
)

const tunnelDomain = "cfargotunnel.com"

type Client struct {
	logger logr.Logger

	cloudflareAPI *cloudflare.Client
	accountID     string
	tunnelName    string

	tunnelID    string
	tunnelToken string
}

var (
	dummy          = struct{}{}
	socksProxyType = "socks"
)

func NewClient(cloudflareAPI *cloudflare.Client, accountID, tunnelName string, logger logr.Logger) *Client {
	return &Client{
		logger:        logger,
		cloudflareAPI: cloudflareAPI,
		accountID:     accountID,
		tunnelName:    tunnelName,
	}
}

func (c *Client) GetTunnelToken(ctx context.Context) (string, error) {
	if len(c.tunnelToken) == 0 {
		tunnel_token, err := c.cloudflareAPI.ZeroTrust.Tunnels.Token.Get(ctx, c.tunnelID, zero_trust.TunnelTokenGetParams{
			AccountID: cloudflare.F(c.accountID),
		})
		if err != nil {
			return "", err
		}
		if tunnel_token == nil {
			return "", errors.New("tunnel token not found")
		}
		c.tunnelToken = *tunnel_token
	}

	return c.tunnelToken, nil
}

func (c *Client) EnsureTunnelExists(ctx context.Context, logger logr.Logger) error {
	if c.tunnelID == "" {
		logger.Info("TunnelID not set, looking for an existing tunnel")

		tunnels := c.cloudflareAPI.ZeroTrust.Tunnels.ListAutoPaging(ctx, zero_trust.TunnelListParams{
			AccountID: cloudflare.F(c.accountID),
		})
		for tunnels.Next() {
			tunnel := tunnels.Current()
			if !tunnel.DeletedAt.IsZero() {
				// This is some deleted tunnel, skip it
				continue
			}
			if tunnel.Name == c.tunnelName {
				logger.Info("Cloudflare Tunnel found", "tunnelID", tunnel.ID)
				c.tunnelID = tunnel.ID
				return nil
			}
		}
		if err := tunnels.Err(); err != nil {
			logger.Error(err, "Failed to list tunnels")
			return err
		}

		logger.Info("Cloudflare Tunnel not found, creating a new one")

		return c.createTunnel(ctx, logger)
	}

	tunnel, err := c.cloudflareAPI.ZeroTrust.Tunnels.Get(ctx, c.tunnelID, zero_trust.TunnelGetParams{
		AccountID: cloudflare.F(c.accountID),
	})
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

	tunnel, err := c.cloudflareAPI.ZeroTrust.Tunnels.New(ctx, zero_trust.TunnelNewParams{
		AccountID:    cloudflare.F(c.accountID),
		Name:         cloudflare.F(c.tunnelName),
		TunnelSecret: cloudflare.F(base64.StdEncoding.EncodeToString(secret)),
		ConfigSrc:    cloudflare.F(zero_trust.TunnelNewParamsConfigSrcCloudflare),
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
	tc, err := c.cloudflareAPI.ZeroTrust.Tunnels.Configurations.Get(ctx, c.tunnelID, zero_trust.TunnelConfigurationGetParams{
		AccountID: cloudflare.F(c.accountID),
	})
	if err != nil {
		logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	config := make([]zero_trust.TunnelConfigurationUpdateParamsConfigIngress, 0, len(tc.Config.Ingress))

	x := zero_trust.NewTunnelConfigurationService()
	_, err = x.Update(ctx, c.tunnelID, zero_trust.TunnelConfigurationUpdateParams{
		AccountID: cloudflare.F(c.accountID),
		Config: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfig{
			Ingress: cloudflare.F(config),
		}),
	})
	if err != nil {
		logger.Error(err, "Failed to update tunnel configuration")
		return err
	}

	tunnelConfig := &tc.Config
	for _, ing := range *ingressRecords {
		for _, ingRule := range tunnelConfig.Ingress {
			// we are not checking the service, since it is not important when deleting
			if ingRule.Hostname != ing.Hostname || ingRule.Path != ing.Path {
				config = append(config, zero_trust.TunnelConfigurationUpdateParamsConfigIngress{
					Hostname: cloudflare.F(ingRule.Hostname),
					Service:  cloudflare.F(ingRule.Service),
					Path:     cloudflare.F(ingRule.Path),
					OriginRequest: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfigIngressOriginRequest{
						Access: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfigIngressOriginRequestAccess{
							AUDTag:   cloudflare.F(ingRule.OriginRequest.Access.AUDTag),
							TeamName: cloudflare.F(ingRule.OriginRequest.Access.TeamName),
							Required: cloudflare.F(ingRule.OriginRequest.Access.Required),
						}),
						CAPool:                 cloudflare.F(ingRule.OriginRequest.CAPool),
						ConnectTimeout:         cloudflare.F(ingRule.OriginRequest.ConnectTimeout),
						DisableChunkedEncoding: cloudflare.F(ingRule.OriginRequest.DisableChunkedEncoding),
						HTTP2Origin:            cloudflare.F(ingRule.OriginRequest.HTTP2Origin),
						HTTPHostHeader:         cloudflare.F(ingRule.OriginRequest.HTTPHostHeader),
						KeepAliveConnections:   cloudflare.F(ingRule.OriginRequest.KeepAliveConnections),
						KeepAliveTimeout:       cloudflare.F(ingRule.OriginRequest.KeepAliveTimeout),
						NoHappyEyeballs:        cloudflare.F(ingRule.OriginRequest.NoHappyEyeballs),
						NoTLSVerify:            cloudflare.F(ingRule.OriginRequest.NoTLSVerify),
						OriginServerName:       cloudflare.F(ingRule.OriginRequest.OriginServerName),
						ProxyType:              cloudflare.F(ingRule.OriginRequest.ProxyType),
						TCPKeepAlive:           cloudflare.F(ingRule.OriginRequest.TCPKeepAlive),
						TLSTimeout:             cloudflare.F(ingRule.OriginRequest.TLSTimeout),
					}),
				})
			}
		}
	}

	c.flush404IfLast(tunnelConfig)

	_, err = c.cloudflareAPI.ZeroTrust.Tunnels.Configurations.Update(ctx, c.tunnelID, zero_trust.TunnelConfigurationUpdateParams{
		AccountID: cloudflare.F(c.accountID),
		Config: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfig{
			Ingress: cloudflare.F(config),
		}),
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

	zones_recods_cache := make(map[string][]*dns.RecordResponse)

	for _, ingress := range *ingressRecords {
		for zoneID, zoneName := range zone_map {
			if !strings.HasSuffix(ingress.Hostname, zoneName) {
				continue
			}
			zone_records, ok := zones_recods_cache[zoneID]
			if !ok {
				ch := c.cloudflareAPI.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{
					ZoneID: cloudflare.F(zoneID),
					Content: cloudflare.F(dns.RecordListParamsContent{
						Exact: cloudflare.String(tunnelDomain),
					}),
				})
				zone_records = make([]*dns.RecordResponse, 0)
				for ch.Next() {
					r := ch.Current()
					zone_records = append(zone_records, &r)
				}
				if err = ch.Err(); err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zones_recods_cache[zoneID] = zone_records
			}
			for _, record := range zone_records {
				if record.Name != ingress.Hostname {
					continue
				}
				_, err := c.cloudflareAPI.DNS.Records.Delete(ctx, record.ID, dns.RecordDeleteParams{
					ZoneID: cloudflare.F(zoneID),
				})
				if err != nil {
					logger.Error(err, "Failed to delete DNS record")
					return err
				}
				break
			}
		}
	}

	return nil
}

func (c *Client) EnsureTunnelConfiguration(ctx context.Context, logger logr.Logger, config *Config) error {
	logger.Info("Ensuring Cloudflare Tunnel configuration")

	zone_map, err := c.getDnsZoneMap(ctx, logger)
	if err != nil {
		return err
	}

	err = c.synchronizeTunnelConfiguration(ctx, logger, config)
	if err != nil {
		return err
	}

	err = c.synchronizeDns(ctx, logger, config, zone_map)
	if err != nil {
		return err
	}

	if config.KubernetesApiTunnelConfig.Enabled {
		err := c.ensureKubeApiApplication(ctx, logger, config, zone_map)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) synchronizeTunnelConfiguration(ctx context.Context, logger logr.Logger, config *Config) error {
	tc, err := c.cloudflareAPI.ZeroTrust.Tunnels.Configurations.Get(ctx, c.tunnelID, zero_trust.TunnelConfigurationGetParams{
		AccountID: cloudflare.F(c.accountID),
	})
	if err != nil {
		logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	active_ingress := tc.Config.Ingress

	want_kube_api_tunnel := config.KubernetesApiTunnelConfig.Enabled
	has_kube_api_tunnel := false
	if want_kube_api_tunnel {
		for _, r := range active_ingress {
			// Check if the active_ingress has the kube API tunnel
			if r.Hostname == config.KubernetesApiTunnelConfig.Domain && r.Service == config.KubernetesApiTunnelConfig.GetService() {
				has_kube_api_tunnel = true
				break
			}
		}
	}

	// Discover whether all ingresses have the same definition (in the same order)
	tunnelConfigUpdated := false
	for _, ingressRecords := range config.Ingresses {
		// If the number of records is 0, they are the same
		if len(*ingressRecords) == 0 {
			continue
		}
		// Otherwise, we need to find the first record in the active_ingress that matches the first record in ingressRecords
		record_offset := -1
		first_record := (*ingressRecords)[0]
		for i, r := range active_ingress {
			if r.Hostname == first_record.Hostname && r.Path == first_record.Path && r.Service == first_record.Service {
				record_offset = i
			}
		}
		if record_offset == -1 {
			// If we didn't find a match, we have a change
			tunnelConfigUpdated = true
			break
		} else {
			// Otherwise, we need to check if all records match
			for i, ingressRecord := range *ingressRecords {
				if active_ingress[record_offset+i].Hostname != ingressRecord.Hostname || active_ingress[record_offset+i].Path != ingressRecord.Path || active_ingress[record_offset+i].Service != ingressRecord.Service {
					tunnelConfigUpdated = true
					break
				}
			}
			if tunnelConfigUpdated {
				break
			}
		}
	}

	var proposed_ingress []zero_trust.TunnelConfigurationUpdateParamsConfigIngress
	if tunnelConfigUpdated {
		proposed_ingress = make([]zero_trust.TunnelConfigurationUpdateParamsConfigIngress, 0)
		for _, ingressRecords := range config.Ingresses {
			for _, ingressRecord := range *ingressRecords {
				new_rule := c.createIngressToTunnelConfigurationStruct(logger, ingressRecord)
				proposed_ingress = append(proposed_ingress, *new_rule)
			}
		}
	}

	tunnelConfigUpdated = tunnelConfigUpdated || (want_kube_api_tunnel != has_kube_api_tunnel)

	if tunnelConfigUpdated && want_kube_api_tunnel {
		new_rule := zero_trust.TunnelConfigurationUpdateParamsConfigIngress{
			Hostname: cloudflare.String(config.KubernetesApiTunnelConfig.Domain),
			Service:  cloudflare.String(config.KubernetesApiTunnelConfig.GetService()),
			OriginRequest: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfigIngressOriginRequest{
				ProxyType: cloudflare.F(socksProxyType),
			}),
		}
		proposed_ingress = append(proposed_ingress, new_rule)
	}

	if tunnelConfigUpdated {
		if len(proposed_ingress) > 0 {
			proposed_ingress = append(proposed_ingress, zero_trust.TunnelConfigurationUpdateParamsConfigIngress{
				Service: cloudflare.String("http_status:404"),
			})
		}

		_, err = c.cloudflareAPI.ZeroTrust.Tunnels.Configurations.Update(ctx, c.tunnelID, zero_trust.TunnelConfigurationUpdateParams{
			AccountID: cloudflare.F(c.accountID),
			Config: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfig{
				Ingress: cloudflare.F(proposed_ingress),
			}),
		})
		if err != nil {
			logger.Error(err, "Failed to update tunnel configuration")
			return err
		}
	}

	return nil
}

func (c *Client) isInZone(hostname string, zoneName string) bool {
	return (hostname == zoneName) || strings.HasSuffix(hostname, "."+zoneName)
}

func (c *Client) synchronizeDns(ctx context.Context, logger logr.Logger, config *Config, zone_map map[string]string) error {

	// determine which hostnames are in which zone
	zone_hostnames := make(map[string]map[string]struct{})
	zone_records := make(map[string][]*dns.RecordResponse)
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

				ch := c.cloudflareAPI.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{
					ZoneID: cloudflare.F(zoneID),
					Content: cloudflare.F(dns.RecordListParamsContent{
						Exact: cloudflare.String(tunnelDomain),
					}),
				})
				dns_records := make([]*dns.RecordResponse, 0)
				for ch.Next() {
					r := ch.Current()
					dns_records = append(dns_records, &r)
				}
				if err := ch.Err(); err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zone_records[zoneID] = dns_records
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

				ch := c.cloudflareAPI.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{
					ZoneID: cloudflare.F(zoneID),
					Content: cloudflare.F(dns.RecordListParamsContent{
						Exact: cloudflare.String(tunnelDomain),
					}),
				})
				dns_records := make([]*dns.RecordResponse, 0)
				for ch.Next() {
					r := ch.Current()
					dns_records = append(dns_records, &r)
				}
				if err := ch.Err(); err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zone_records[zoneID] = dns_records
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
		if err := c.createDNSRecords(ctx, logger, zoneID, hostname_list); err != nil {
			return err
		}
	}

	for zoneID, dns_records := range zone_records {
		valid_hostnames := zone_hostnames[zoneID]
		for _, record := range dns_records {
			if _, ok := valid_hostnames[record.Name]; !ok {
				_, err := c.cloudflareAPI.DNS.Records.Delete(ctx, record.ID, dns.RecordDeleteParams{
					ZoneID: cloudflare.F(zoneID),
				})
				if err != nil {
					logger.Error(err, "Failed to delete DNS record")
					return err
				}
			}
		}
	}

	return nil
}

func (c *Client) createIngressToTunnelConfigurationStruct(logger logr.Logger, ingress *zero_trust.TunnelConfigurationGetResponseConfigIngress) *zero_trust.TunnelConfigurationUpdateParamsConfigIngress {
	logger.Info("Adding new ingress rule to tunnel configuration")

	newIngressRule := &zero_trust.TunnelConfigurationUpdateParamsConfigIngress{
		Hostname: cloudflare.String(ingress.Hostname),
		Service:  cloudflare.String(ingress.Service),
		OriginRequest: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfigIngressOriginRequest{
			Access: cloudflare.F(zero_trust.TunnelConfigurationUpdateParamsConfigIngressOriginRequestAccess{
				AUDTag:   cloudflare.F(ingress.OriginRequest.Access.AUDTag),
				TeamName: cloudflare.F(ingress.OriginRequest.Access.TeamName),
				Required: cloudflare.F(ingress.OriginRequest.Access.Required),
			}),
			CAPool:                 cloudflare.F(ingress.OriginRequest.CAPool),
			ConnectTimeout:         cloudflare.F(ingress.OriginRequest.ConnectTimeout),
			DisableChunkedEncoding: cloudflare.F(ingress.OriginRequest.DisableChunkedEncoding),
			HTTP2Origin:            cloudflare.F(ingress.OriginRequest.HTTP2Origin),
			HTTPHostHeader:         cloudflare.F(ingress.OriginRequest.HTTPHostHeader),
			KeepAliveConnections:   cloudflare.F(ingress.OriginRequest.KeepAliveConnections),
			NoHappyEyeballs:        cloudflare.F(ingress.OriginRequest.NoHappyEyeballs),
			NoTLSVerify:            cloudflare.F(ingress.OriginRequest.NoTLSVerify),
			OriginServerName:       cloudflare.F(ingress.OriginRequest.OriginServerName),
			ProxyType:              cloudflare.F(ingress.OriginRequest.ProxyType),
			TCPKeepAlive:           cloudflare.F(ingress.OriginRequest.TCPKeepAlive),
			TLSTimeout:             cloudflare.F(ingress.OriginRequest.TLSTimeout),
		}),
		Path: cloudflare.String(ingress.Path),
	}

	return newIngressRule
}

func (c *Client) getDnsZoneMap(ctx context.Context, logger logr.Logger) (map[string]string, error) {
	// get the zone id
	result := make(map[string]string)

	zones := c.cloudflareAPI.Zones.ListAutoPaging(ctx, zones.ZoneListParams{
		Account: cloudflare.F(zones.ZoneListParamsAccount{
			ID: cloudflare.String(c.accountID),
		}),
	})
	for zones.Next() {
		zone := zones.Current()
		result[zone.Name] = zone.ID
	}
	if err := zones.Err(); err != nil {
		logger.Error(err, "Failed to list zones")
		return nil, err
	}

	return result, nil
}

func (c *Client) createDNSRecords(ctx context.Context, logger logr.Logger, zoneID string, hostnames []string) error {
	logger.Info("Creating new DNS record")

	truth := true

	// create the DNS records
	for _, hostname := range hostnames {
		_, err := c.cloudflareAPI.DNS.Records.New(ctx, dns.RecordNewParams{
			ZoneID: cloudflare.String(zoneID),
			Record: dns.RecordParam{
				Proxied: cloudflare.Bool(truth),
				Type:    cloudflare.F(dns.RecordTypeCNAME),
				Name:    cloudflare.String(hostname),
				Content: cloudflare.String(c.tunnelID + "." + tunnelDomain),
				Comment: cloudflare.String("Automatically created by Cloudflare Tunnel Ingress Controller"),
			},
		})
		if err != nil {
			cfErr := &cloudflare.Error{}
			if errors.As(err, &cfErr) {
				for _, e := range cfErr.Errors {
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

func (c *Client) flush404IfLast(tunnelConfig *zero_trust.TunnelConfigurationGetResponseConfig) {
	if len(tunnelConfig.Ingress) == 1 && tunnelConfig.Ingress[0].Service == "http_status:404" {
		tunnelConfig.Ingress = make([]zero_trust.TunnelConfigurationGetResponseConfigIngress, 0)
	}
}

func (c *Client) ensureKubeApiApplication(ctx context.Context, logger logr.Logger, config *Config, zone_map map[string]string) error {
	ch := c.cloudflareAPI.ZeroTrust.Access.Applications.ListAutoPaging(ctx, zero_trust.AccessApplicationListParams{
		AccountID: cloudflare.F(c.accountID),
	})
	apps := make([]*zero_trust.AccessApplicationListResponse, 0)
	for ch.Next() {
		app := ch.Current()
		apps = append(apps, &app)
	}
	if err := ch.Err(); err != nil {
		logger.Error(err, "Failed to list Access Applications")
		return err
	}

	for _, app := range apps {
		if app.Domain == config.KubernetesApiTunnelConfig.Domain {
			return nil
		}
	}

	var zone_id string
	for k, v := range zone_map {
		if strings.HasSuffix(config.KubernetesApiTunnelConfig.Domain, v) {
			zone_id = k
		}
	}

	if len(zone_id) == 0 {
		return fmt.Errorf("failed to find zone ID for kube API tunnel: %s", config.KubernetesApiTunnelConfig.Domain)
	}

	_, err := c.cloudflareAPI.ZeroTrust.Access.Applications.New(ctx, zero_trust.AccessApplicationNewParams{
		AccountID: cloudflare.F(c.accountID),
		Body: zero_trust.AccessApplicationNewParamsBodySelfHostedApplication{
			Name:   cloudflare.String(config.KubernetesApiTunnelConfig.CloudflareAccessAppName),
			Domain: cloudflare.String(config.KubernetesApiTunnelConfig.Domain),
			Type:   cloudflare.String(string(zero_trust.ApplicationTypeSelfHosted)),
		},
		ZoneID: cloudflare.F(zone_id),
	})
	return err
}
