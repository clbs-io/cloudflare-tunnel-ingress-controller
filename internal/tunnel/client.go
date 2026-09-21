package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/cloudflare/cloudflare-go/v7/zones"
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
		tunnel_token, err := c.cloudflareAPI.ZeroTrust.Tunnels.Cloudflared.Token.Get(ctx, c.tunnelID, zero_trust.TunnelCloudflaredTokenGetParams{
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

	tunnel, err := c.cloudflareAPI.ZeroTrust.Tunnels.Cloudflared.Get(ctx, c.tunnelID, zero_trust.TunnelCloudflaredGetParams{
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

	tunnel, err := c.cloudflareAPI.ZeroTrust.Tunnels.Cloudflared.New(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID:    cloudflare.F(c.accountID),
		Name:         cloudflare.F(c.tunnelName),
		TunnelSecret: cloudflare.F(base64.StdEncoding.EncodeToString(secret)),
		ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
	})
	if err != nil {
		logger.Error(err, "Failed to create a tunnel")
		return err
	}

	logger.Info("Cloudflare Tunnel created", "tunnelID", tunnel.ID)
	c.tunnelID = tunnel.ID
	return nil
}

// DeleteFromTunnelConfiguration synchronizes the tunnel after an Ingress was
// removed from config, and deletes the tunnel DNS records of removed_hostnames
// that no remaining rule uses.
func (c *Client) DeleteFromTunnelConfiguration(ctx context.Context, logger logr.Logger, config *Config, removed_hostnames []string) error {
	logger.Info("Deleting from Cloudflare Tunnel configuration")

	zone_map, err := c.getDnsZoneMap(ctx, logger)
	if err != nil {
		return err
	}

	err = c.synchronizeTunnelConfiguration(ctx, logger, config)
	if err != nil {
		return err
	}

	return c.synchronizeDns(ctx, logger, config, zone_map, removed_hostnames)
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

	err = c.synchronizeDns(ctx, logger, config, zone_map, nil)
	if err != nil {
		return err
	}

	if config.KubernetesApiTunnelConfig.Enabled {
		err := c.ensureAccessApplication(ctx, logger, config.KubernetesApiTunnelConfig.Domain, config.KubernetesApiTunnelConfig.CloudflareAccessAppName)
		if err != nil {
			return err
		}
	}

	for hostname, app_name := range config.AccessAppRequests {
		err := c.ensureAccessApplication(ctx, logger, hostname, app_name)
		if err != nil {
			return err
		}
	}

	return nil
}

// synchronizeTunnelConfiguration replaces the remote ingress rules with the
// rules rendered from config whenever the two differ.
func (c *Client) synchronizeTunnelConfiguration(ctx context.Context, logger logr.Logger, config *Config) error {
	tc, err := c.cloudflareAPI.ZeroTrust.Tunnels.Cloudflared.Configurations.Get(ctx, c.tunnelID, zero_trust.TunnelCloudflaredConfigurationGetParams{
		AccountID: cloudflare.F(c.accountID),
	})
	if err != nil {
		logger.Error(err, "Failed to get tunnel configuration")
		return err
	}

	desired := desiredIngressRules(config)

	in_sync, err := ingressRulesEqual(desired, tc.Config.JSON.Ingress.Raw())
	if err != nil {
		logger.Error(err, "Failed to compare tunnel configuration")
		return err
	}
	if in_sync {
		return nil
	}

	logger.Info("Updating Cloudflare Tunnel configuration", "rules", len(desired))

	_, err = c.cloudflareAPI.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, c.tunnelID, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
		AccountID: cloudflare.F(c.accountID),
		Config: cloudflare.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
			Ingress: cloudflare.F(desired),
		}),
	})
	if err != nil {
		logger.Error(err, "Failed to update tunnel configuration")
		return err
	}

	return nil
}

// desiredIngressRules renders the complete rule list in a stable order: the
// rules of each Ingress in Ingress UID order, the Kubernetes API rule, and the
// catch-all rule cloudflared requires last.
func desiredIngressRules(config *Config) []zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress {
	rules := make([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress, 0)

	for _, uid := range slices.Sorted(maps.Keys(config.Ingresses)) {
		for _, record := range *config.Ingresses[uid] {
			rules = append(rules, ingressRuleParam(record))
		}
	}

	if config.KubernetesApiTunnelConfig.Enabled {
		rules = append(rules, zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
			Hostname: cloudflare.F(config.KubernetesApiTunnelConfig.Domain),
			Service:  cloudflare.F(config.KubernetesApiTunnelConfig.GetService()),
			OriginRequest: cloudflare.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequest{
				ProxyType: cloudflare.F(socksProxyType),
			}),
		})
	}

	if len(rules) > 0 {
		rules = append(rules, zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
			Service: cloudflare.F("http_status:404"),
		})
	}

	return rules
}

// ingressRuleParam converts a rule to its API form. Settings with a zero value
// are left out, so cloudflared applies its own defaults to them.
func ingressRuleParam(record *zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress) zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress {
	rule := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
		Service: cloudflare.F(record.Service),
	}
	setIfNonZero(&rule.Hostname, record.Hostname, cloudflare.F)
	setIfNonZero(&rule.Path, record.Path, cloudflare.F)

	o := record.OriginRequest
	origin := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequest{}
	set := []bool{
		setIfNonZero(&origin.CAPool, o.CAPool, cloudflare.F),
		setIfNonZero(&origin.ConnectTimeout, o.ConnectTimeout, cloudflare.F),
		setIfNonZero(&origin.DisableChunkedEncoding, o.DisableChunkedEncoding, cloudflare.F),
		setIfNonZero(&origin.HTTP2Origin, o.HTTP2Origin, cloudflare.F),
		setIfNonZero(&origin.HTTPHostHeader, o.HTTPHostHeader, cloudflare.F),
		setIfNonZero(&origin.KeepAliveConnections, o.KeepAliveConnections, cloudflare.F),
		setIfNonZero(&origin.KeepAliveTimeout, o.KeepAliveTimeout, cloudflare.F),
		setIfNonZero(&origin.MatchSnItoHost, o.MatchSnItoHost, cloudflare.F),
		setIfNonZero(&origin.NoHappyEyeballs, o.NoHappyEyeballs, cloudflare.F),
		setIfNonZero(&origin.NoTLSVerify, o.NoTLSVerify, cloudflare.F),
		setIfNonZero(&origin.OriginServerName, o.OriginServerName, cloudflare.F),
		setIfNonZero(&origin.ProxyType, o.ProxyType, cloudflare.F),
		setIfNonZero(&origin.TCPKeepAlive, o.TCPKeepAlive, cloudflare.F),
		setIfNonZero(&origin.TLSTimeout, o.TLSTimeout, cloudflare.F),
	}

	if o.Access.Required || o.Access.TeamName != "" || len(o.Access.AUDTag) > 0 {
		aud_tags := o.Access.AUDTag
		if aud_tags == nil {
			aud_tags = []string{}
		}
		origin.Access = cloudflare.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequestAccess{
			AUDTag:   cloudflare.F(aud_tags),
			TeamName: cloudflare.F(o.Access.TeamName),
			Required: cloudflare.F(o.Access.Required),
		})
		set = append(set, true)
	}

	if slices.Contains(set, true) {
		rule.OriginRequest = cloudflare.F(origin)
	}

	return rule
}

// setIfNonZero sets dst to f(v) and reports true, unless v is the zero value.
func setIfNonZero[T comparable, F any](dst *F, v T, f func(T) F) bool {
	var zero T
	if v == zero {
		return false
	}
	*dst = f(v)
	return true
}

// ingressRulesEqual reports whether the remote rules, raw JSON as returned by
// the API, match the desired rules. Absent, null, empty and false values are
// equivalent; numbers compare even when zero, so explicit zero settings on the
// remote side get replaced.
func ingressRulesEqual(desired []zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress, remote_raw string) (bool, error) {
	desired_json, err := json.Marshal(desired)
	if err != nil {
		return false, err
	}

	var want, have any
	if err := json.Unmarshal(desired_json, &want); err != nil {
		return false, err
	}
	if len(strings.TrimSpace(remote_raw)) > 0 {
		if err := json.Unmarshal([]byte(remote_raw), &have); err != nil {
			return false, err
		}
	}
	if have == nil {
		have = []any{}
	}

	return reflect.DeepEqual(normalizeJSON(want), normalizeJSON(have)), nil
}

func normalizeJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			if value = normalizeJSON(value); !isEmptyJSON(value) {
				out[key] = value
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, value := range v {
			out = append(out, normalizeJSON(value))
		}
		return out
	default:
		return v
	}
}

func isEmptyJSON(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

func (c *Client) isInZone(hostname string, zoneName string) bool {
	return (hostname == zoneName) || strings.HasSuffix(hostname, "."+zoneName)
}

// zoneIDFor returns the ID of the most specific zone containing hostname, or
// an empty string when no zone contains it.
func (c *Client) zoneIDFor(hostname string, zone_map map[string]string) string {
	best := ""
	for zone := range zone_map {
		if c.isInZone(hostname, zone) && len(zone) > len(best) {
			best = zone
		}
	}
	if len(best) == 0 {
		return ""
	}
	return zone_map[best]
}

func desiredHostnames(config *Config) []string {
	hostnames := make([]string, 0)
	for _, ingressRecords := range config.Ingresses {
		for _, record := range *ingressRecords {
			if len(record.Hostname) > 0 {
				hostnames = append(hostnames, record.Hostname)
			}
		}
	}
	if config.KubernetesApiTunnelConfig.Enabled {
		hostnames = append(hostnames, config.KubernetesApiTunnelConfig.Domain)
	}
	return hostnames
}

// synchronizeDns creates a tunnel CNAME for every desired hostname and deletes
// tunnel CNAMEs no rule uses, in the zones of the desired hostnames and of
// removed_hostnames.
func (c *Client) synchronizeDns(ctx context.Context, logger logr.Logger, config *Config, zone_map map[string]string, removed_hostnames []string) error {
	zone_hostnames := make(map[string]map[string]struct{})
	scan_zones := make(map[string]struct{})

	for _, hostname := range desiredHostnames(config) {
		zoneID := c.zoneIDFor(hostname, zone_map)
		if len(zoneID) == 0 {
			logger.Info("Failed to find zone ID", "hostname", hostname)
			continue
		}
		if _, ok := zone_hostnames[zoneID]; !ok {
			zone_hostnames[zoneID] = make(map[string]struct{})
		}
		zone_hostnames[zoneID][hostname] = dummy
		scan_zones[zoneID] = dummy
	}

	for _, hostname := range removed_hostnames {
		if zoneID := c.zoneIDFor(hostname, zone_map); len(zoneID) > 0 {
			scan_zones[zoneID] = dummy
		}
	}

	for zoneID, hostnames := range zone_hostnames {
		if err := c.createDNSRecords(ctx, logger, zoneID, slices.Collect(maps.Keys(hostnames))); err != nil {
			return err
		}
	}

	for zoneID := range scan_zones {
		records, err := c.listTunnelDnsRecords(ctx, logger, zoneID)
		if err != nil {
			return err
		}
		for _, record := range records {
			if _, ok := zone_hostnames[zoneID][record.Name]; ok {
				continue
			}
			_, err := c.cloudflareAPI.DNS.Records.Delete(ctx, record.ID, dns.RecordDeleteParams{
				ZoneID: cloudflare.F(zoneID),
			})
			if err != nil {
				logger.Error(err, "Failed to delete DNS record")
				return err
			}
		}
	}

	return nil
}

// listTunnelDnsRecords lists the DNS records in a zone that point to the tunnel.
func (c *Client) listTunnelDnsRecords(ctx context.Context, logger logr.Logger, zoneID string) ([]dns.RecordResponse, error) {
	pager := c.cloudflareAPI.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{
		ZoneID: cloudflare.F(zoneID),
		Content: cloudflare.F(dns.RecordListParamsContent{
			Exact: cloudflare.F(c.tunnelID + "." + tunnelDomain),
		}),
	})
	records := make([]dns.RecordResponse, 0)
	for pager.Next() {
		records = append(records, pager.Current())
	}
	if err := pager.Err(); err != nil {
		logger.Error(err, "Failed to list DNS records")
		return nil, err
	}
	return records, nil
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
			Body: dns.CNAMERecordParam{
				Proxied: cloudflare.Bool(truth),
				Type:    cloudflare.F(dns.CNAMERecordTypeCNAME),
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

// ensureAccessApplication creates an account-level self-hosted Access
// application for domain unless an application for it already exists.
func (c *Client) ensureAccessApplication(ctx context.Context, logger logr.Logger, domain, app_name string) error {
	ch := c.cloudflareAPI.ZeroTrust.Access.Applications.ListAutoPaging(ctx, zero_trust.AccessApplicationListParams{
		AccountID: cloudflare.F(c.accountID),
	})
	for ch.Next() {
		app := ch.Current()
		if app.Domain == domain {
			return nil
		}
	}
	if err := ch.Err(); err != nil {
		logger.Error(err, "Failed to list Access Applications")
		return err
	}

	logger.Info("Creating Access application", "domain", domain, "name", app_name)

	_, err := c.cloudflareAPI.ZeroTrust.Access.Applications.New(ctx, zero_trust.AccessApplicationNewParams{
		AccountID: cloudflare.F(c.accountID),
		Body: zero_trust.AccessApplicationNewParamsBodySelfHostedApplication{
			Name:   cloudflare.String(app_name),
			Domain: cloudflare.String(domain),
			Type:   cloudflare.F(zero_trust.ApplicationTypeSelfHosted),
		},
	})
	if err != nil {
		logger.Error(err, "Failed to create Access application", "domain", domain)
	}
	return err
}
