package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/cloudflare/cloudflare-go/v2"
	"github.com/cloudflare/cloudflare-go/v2/dns"
	"github.com/cloudflare/cloudflare-go/v2/shared"
	"github.com/cloudflare/cloudflare-go/v2/zero_trust"
	"github.com/cloudflare/cloudflare-go/v2/zones"
	"github.com/go-logr/logr"
)

const tunnelDomain = "cfargotunnel.com"

type Client struct {
	logger logr.Logger

	cloudflareClient *cloudflare.Client
	accountID        string
	tunnelName       string

	tunnelID    string
	tunnelToken string
}

var (
	dummy = struct{}{}
)

func NewClient(cloudflareClient *cloudflare.Client, accountID string, tunnelName string, logger logr.Logger) *Client {
	return &Client{
		logger:           logger,
		cloudflareClient: cloudflareClient,
		accountID:        accountID,
		tunnelName:       tunnelName,
	}
}

func (c *Client) GetTunnelToken(ctx context.Context) (string, error) {
	if c.tunnelToken == "" {
		res, err := c.cloudflareClient.ZeroTrust.Tunnels.Token.Get(ctx, c.tunnelID, zero_trust.TunnelTokenGetParams{
			AccountID: cloudflare.String(c.accountID),
		})
		if err != nil {
			return "", err
		}
		switch t := (*res).(type) {
		case shared.UnionString:
			c.tunnelToken = string(t)
		default:
			return "", errors.New("unexpected response type")
		}
	}

	return c.tunnelToken, nil
}

func (c *Client) EnsureTunnelExists(ctx context.Context) error {
	if c.tunnelID == "" {
		c.logger.Info("TunnelID not set, looking for an existing tunnel")

		iter := c.cloudflareClient.ZeroTrust.Tunnels.ListAutoPaging(ctx, zero_trust.TunnelListParams{
			AccountID: cloudflare.String(c.accountID),
		})
		for iter.Next() {
			tunnel := iter.Current()
			if tunnel.Name == c.tunnelName {
				c.logger.Info("Cloudflare Tunnel found", "tunnelID", tunnel.ID)
				c.tunnelID = tunnel.ID
				return nil
			}
		}
		if err := iter.Err(); err != nil {
			return err
		}

		c.logger.Info("Cloudflare Tunnel not found, creating a new one")
		return c.createTunnel(ctx)
	}

	tunnel, err := c.cloudflareClient.ZeroTrust.Tunnels.Get(ctx, c.tunnelID, zero_trust.TunnelGetParams{
		AccountID: cloudflare.String(c.accountID),
	})
	if err != nil {
		c.logger.Error(err, "Failed to get the tunnel")
		return err
	}

	if tunnel.Name != c.tunnelName {
		c.logger.Error(errors.New("tunnel name mismatch"), "Tunnel name mismatch, this will force creation new tunnel, please review your configuration", "expected", c.tunnelName, "actual", tunnel.Name)
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
	tunnel, err := c.cloudflareClient.ZeroTrust.Tunnels.New(ctx, zero_trust.TunnelNewParams{
		AccountID:    cloudflare.String(c.accountID),
		Name:         cloudflare.String(c.tunnelName),
		TunnelSecret: cloudflare.String(hexSecret),
	})
	if err != nil {
		c.logger.Error(err, "Failed to create a tunnel")
		return err
	}

	c.logger.Info("Cloudflare Tunnel created", "tunnelID", tunnel.ID)
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
	tc, err := c.cloudflareClient.ZeroTrust.Tunnels.Configurations.Get(ctx, c.tunnelID, zero_trust.TunnelConfigurationGetParams{
		AccountID: cloudflare.String(c.accountID),
	})
	if err != nil {
		logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	switch t := (*tc).(type) {
	case zero_trust.TunnelConfigurationGetResponseArray:
		for _, tunnel := range t {
			c.logger.Info("Tunnel configuration", "config", tunnel)
		}
	default:
		return errors.New("unexpected response type")
	}

	tmp := zero_trust.TunnelConfigurationUpdateParamsConfig{}

	tunnelConfig := tmp
	if tunnelConfig.Ingress.Null {
		tunnelConfig.Ingress = cloudflare.F(make([]zero_trust.TunnelConfigurationUpdateParamsConfigIngress, 0))
	}

	for _, ing := range *ingressRecords {
		for i, ingRule := range tunnelConfig.Ingress.Value {
			// we are not checking the service, since it is not important when deleting
			if ingRule.Hostname.Value == ing.Hostname && ingRule.Path.Value == ing.Path {
				// remove idx-th element from the slice
				tunnelConfig.Ingress.Value = append(tunnelConfig.Ingress.Value[:i], tunnelConfig.Ingress.Value[i+1:]...)
				break
			}
		}
	}

	c.flush404IfLast(&tunnelConfig)

	_, err = c.cloudflareClient.ZeroTrust.Tunnels.Configurations.Update(ctx, c.tunnelID, zero_trust.TunnelConfigurationUpdateParams{
		AccountID: cloudflare.String(c.accountID),
		Config:    cloudflare.F(tunnelConfig),
	})
	if err != nil {
		c.logger.Error(err, "Failed to update tunnel configuration", "tunnelConfig", tunnelConfig)
		return err
	}

	return nil
}

func (c *Client) deleteFromDns(ctx context.Context, logger logr.Logger, ingressRecords *IngressRecords) error {
	zone_map, err := c.getDnsZoneMap(ctx)
	if err != nil {
		return err
	}

	zones_recods_cache := make(map[string]*[]dns.Record)

	for _, ingress := range *ingressRecords {
		for zoneID, zoneName := range zone_map {
			if !strings.HasSuffix(ingress.Hostname, zoneName) {
				continue
			}
			if zone_records, ok := zones_recods_cache[zoneID]; !ok {
				iter := c.cloudflareClient.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{
					Content: cloudflare.String(c.tunnelID + "." + tunnelDomain),
				})
				var dns_records []dns.Record
				for iter.Next() {
					dns_records = append(dns_records, iter.Current())
				}
				if err := iter.Err(); err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zones_recods_cache[zoneID] = &dns_records
			} else {
				for _, record := range *zone_records {
					if record.Name != ingress.Hostname {
						continue
					}
					_, err := c.cloudflareClient.DNS.Records.Delete(ctx, record.ID, dns.RecordDeleteParams{
						ZoneID: cloudflare.String(zoneID),
					})
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
	tc, err := c.cloudflareClient.ZeroTrust.Tunnels.Configurations.Get(ctx, c.tunnelID, zero_trust.TunnelConfigurationGetParams{
		AccountID: cloudflare.String(c.accountID),
	})
	c.logger.Info("Getting tunnel configuration", "tc", tc)
	if err != nil {
		c.logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	tunnelConfig := zero_trust.TunnelConfigurationUpdateParamsConfig{}
	switch t := (*tc).(type) {
	case zero_trust.TunnelConfigurationGetResponseArray:
		c.logger.Info("Tunnel configuration", "config", t)
		//tunnelConfig.Ingress = t
	}

	tunnelConfigUpdated := false

	if tunnelConfig.Ingress.Null {
		tunnelConfig.Ingress = cloudflare.F(make([]zero_trust.TunnelConfigurationUpdateParamsConfigIngress, 0))
	}

	for _, ingressRecords := range config.Ingresses {
		for _, ingressRecord := range *ingressRecords {
			is_new := true
			for _, tunnelRecord := range tunnelConfig.Ingress.Value {
				// is it new hostname?
				// if so, we should add it to the configuration
				if tunnelRecord.Hostname.Value == ingressRecord.Hostname && tunnelRecord.Path.Value == ingressRecord.Path && tunnelRecord.Service.Value == ingressRecord.Service {
					is_new = false
					break
				}
			}
			if is_new {
				err = c.addNewIngressToTunnelConfigurationStruct(&tunnelConfig, ingressRecord)
				if err != nil {
					c.logger.Error(err, "Failed to add new ingress rule to tunnel configuration")
					return err
				}

				tunnelConfigUpdated = true
			}
		}
	}

	// delete hostnames
	for tc_ingress_idx := len(tunnelConfig.Ingress.Value) - 1; tc_ingress_idx >= 0; tc_ingress_idx-- {
		tunnelRecord := tunnelConfig.Ingress.Value[tc_ingress_idx]

		if tunnelRecord.Service.Value == "http_status:404" {
			// Do not delete http_status:404 rule
			continue
		}

		still_exists := false
		for _, ingress := range config.Ingresses {
			for _, ingressRecord := range *ingress {
				// is it new hostname?
				// if so, we should add it to the configuration
				if ingressRecord.Hostname == tunnelRecord.Hostname.Value && ingressRecord.Path == tunnelRecord.Path.Value && ingressRecord.Service == tunnelRecord.Service.Value {
					still_exists = true
					break
				}
			}
			if still_exists {
				break
			}
		}
		if !still_exists {
			tunnelConfig.Ingress.Value = append(tunnelConfig.Ingress.Value[:tc_ingress_idx], tunnelConfig.Ingress.Value[tc_ingress_idx+1:]...)
			tunnelConfigUpdated = true
		}
	}

	if tunnelConfigUpdated {
		c.flush404IfLast(&tunnelConfig)

		_, err = c.cloudflareClient.ZeroTrust.Tunnels.Configurations.Update(ctx, c.tunnelID, zero_trust.TunnelConfigurationUpdateParams{
			AccountID: cloudflare.String(c.accountID),
			Config:    cloudflare.F(tunnelConfig),
		})
		if err != nil {
			logger.Error(err, "Failed to update tunnel configuration", "tunnelConfig", tunnelConfig)
			return err
		}
	}

	return nil
}

func (c *Client) synchronizeDns(ctx context.Context, logger logr.Logger, config *Config) error {

	zone_map, err := c.getDnsZoneMap(ctx)
	if err != nil {
		return err
	}

	// determine which hostnames are in which zone
	zone_hostnames := make(map[string]map[string]struct{})
	zone_records := make(map[string]*[]dns.Record)
	for _, ingressRecords := range config.Ingresses {
		for _, ingress := range *ingressRecords {
			zoneID := ""
			for zone, zone_id := range zone_map {
				if strings.HasSuffix(ingress.Hostname, zone) {
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

				iter := c.cloudflareClient.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{
					ZoneID:  cloudflare.String(zoneID),
					Content: cloudflare.String(c.tunnelID + "." + tunnelDomain),
				})
				var dns_records []dns.Record
				for iter.Next() {
					dns_records = append(dns_records, iter.Current())
				}
				if err = iter.Err(); err != nil {
					logger.Error(err, "Failed to list DNS records")
					return err
				}
				zone_records[zoneID] = &dns_records
			}
			zone_hostnames[zoneID][ingress.Hostname] = dummy
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
				_, err = c.cloudflareClient.DNS.Records.Delete(ctx, record.ID, dns.RecordDeleteParams{
					ZoneID: cloudflare.String(zoneID),
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

func (c *Client) addNewIngressToTunnelConfigurationStruct(tunnelConfig *zero_trust.TunnelConfigurationUpdateParamsConfig, ingress *IngressConfig) error {
	c.logger.Info("Adding new ingress rule to tunnel configuration")

	tunnel_records := tunnelConfig.Ingress

	newIngressRule := zero_trust.TunnelConfigurationUpdateParamsConfigIngress{
		Hostname: cloudflare.String(ingress.Hostname),
		Path:     cloudflare.String(ingress.Path),
		Service:  cloudflare.String(ingress.Service),
	}

	if ingress.OriginConfig != nil {
		var tmp zero_trust.TunnelConfigurationUpdateParamsConfigIngressOriginRequest
		if newIngressRule.OriginRequest.Null {
			tmp = zero_trust.TunnelConfigurationUpdateParamsConfigIngressOriginRequest{}
			newIngressRule.OriginRequest = cloudflare.F(tmp)
		} else {
			tmp = newIngressRule.OriginRequest.Value
		}

		if connectTimeout := ingress.OriginConfig.ConnectTimeout; connectTimeout != nil {
			tmp.ConnectTimeout = cloudflare.Int(connectTimeout.Nanoseconds())
		}
		if tlsTimeout := ingress.OriginConfig.TLSTimeout; tlsTimeout != nil {
			tmp.TLSTimeout = cloudflare.Int(tlsTimeout.Nanoseconds())
		}
		if tcpKeepAlive := ingress.OriginConfig.TCPKeepAlive; tcpKeepAlive != nil {
			tmp.TCPKeepAlive = cloudflare.Int(tcpKeepAlive.Nanoseconds())
		}
		if keepAliveTimeout := ingress.OriginConfig.KeepAliveTimeout; keepAliveTimeout != nil {
			tmp.KeepAliveTimeout = cloudflare.Int(keepAliveTimeout.Nanoseconds())
		}
		if noHappyEyeballs := ingress.OriginConfig.NoHappyEyeballs; noHappyEyeballs != nil {
			tmp.NoHappyEyeballs = cloudflare.Bool(*noHappyEyeballs)
		}
		if keepAliveConnections := ingress.OriginConfig.KeepAliveConnections; keepAliveConnections != nil {
			tmp.KeepAliveConnections = cloudflare.Int(int64(*keepAliveConnections))
		}
		if httpHostHeader := ingress.OriginConfig.HTTPHostHeader; httpHostHeader != nil {
			tmp.HTTPHostHeader = cloudflare.String(*httpHostHeader)
		}
		if originServerName := ingress.OriginConfig.OriginServerName; originServerName != nil {
			tmp.OriginServerName = cloudflare.String(*originServerName)
		}
		if noTLSVerify := ingress.OriginConfig.NoTLSVerify; noTLSVerify != nil {
			tmp.NoTLSVerify = cloudflare.Bool(*noTLSVerify)
		}
		if disableChunkedEncoding := ingress.OriginConfig.DisableChunkedEncoding; disableChunkedEncoding != nil {
			tmp.DisableChunkedEncoding = cloudflare.Bool(*disableChunkedEncoding)
		}
		if proxyType := ingress.OriginConfig.ProxyType; proxyType != nil {
			tmp.ProxyType = cloudflare.String(*proxyType)
		}
		if http2Origin := ingress.OriginConfig.Http2Origin; http2Origin != nil {
			tmp.HTTP2Origin = cloudflare.Bool(*http2Origin)
		}
	}

	last_id := len(tunnel_records.Value) - 1
	has_http_status_404 := last_id >= 0 && tunnel_records.Value[last_id].Service.Value == "http_status:404"
	if has_http_status_404 {
		// Keep the http_status:404 rule at the end (if it exists)
		tunnel_records.Value = append(tunnel_records.Value[:last_id], newIngressRule, tunnel_records.Value[last_id])
	} else {
		// Add the new rule at the end and add a http_status:404 rule after it
		tunnel_records.Value = append(tunnel_records.Value, newIngressRule, zero_trust.TunnelConfigurationUpdateParamsConfigIngress{
			Service: cloudflare.String("http_status:404"),
		})
	}

	return nil
}

func (c *Client) getDnsZoneMap(ctx context.Context) (map[string]string, error) {
	// get the zone id
	iter := c.cloudflareClient.Zones.ListAutoPaging(ctx, zones.ZoneListParams{})
	var zones []zones.Zone
	for iter.Next() {
		zones = append(zones, iter.Current())
	}
	if err := iter.Err(); err != nil {
		c.logger.Error(err, "Failed to list zones")
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

	// create the DNS records
	for _, hostname := range hostnames {
		_, err := c.cloudflareClient.DNS.Records.New(ctx, dns.RecordNewParams{
			ZoneID: cloudflare.String(zoneID),
			Record: dns.CNAMERecordParam{
				Type:    cloudflare.F(dns.CNAMERecordTypeCNAME),
				Proxied: cloudflare.Bool(true),
				Name:    cloudflare.String(hostname),
				// Content: cloudflare.F(c.tunnelID + "." + tunnelDomain),
				Comment: cloudflare.String("Automatically created by Cloudflare Tunnel Ingress Controller"),
			},
		})
		if err != nil {
			/*cfErr := &cloudflare.Error{}
			if errors.As(err, &cfErr) {
				for _, e := range cfErr.Errors() {
					if e.Code != 81053 {
						logger.Error(err, "Failed to create DNS record")
						return err
					}
				}
				// 81053: "Record already exists"
				continue
			}*/

			logger.Error(err, "Failed to create DNS record")
			return err
		}
	}

	return nil
}

func (c *Client) flush404IfLast(tunnelConfig *zero_trust.TunnelConfigurationUpdateParamsConfig) {
	if len(tunnelConfig.Ingress.Value) == 1 && tunnelConfig.Ingress.Value[0].Service.Value == "http_status:404" {
		tunnelConfig.Ingress = cloudflare.Null[[]zero_trust.TunnelConfigurationUpdateParamsConfigIngress]()
	}
}
