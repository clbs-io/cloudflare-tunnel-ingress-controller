package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/shared"
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

// SyncResult reports conditions found while synchronizing that are not errors.
type SyncResult struct {
	// DNSConflicts lists hostnames whose DNS name is held by a record that
	// does not point to the tunnel.
	DNSConflicts []string
}

// Sync makes the tunnel configuration and the tunnel DNS records match config.
func (c *Client) Sync(ctx context.Context, logger logr.Logger, config *Config) (SyncResult, error) {
	zone_map, err := c.getDnsZoneMap(ctx, logger)
	if err != nil {
		return SyncResult{}, err
	}

	if err := c.synchronizeTunnelConfiguration(ctx, logger, config); err != nil {
		return SyncResult{}, err
	}

	conflicts, err := c.synchronizeDns(ctx, logger, config, zone_map)
	if err != nil {
		return SyncResult{}, err
	}

	return SyncResult{DNSConflicts: conflicts}, nil
}

// EnsureAccessApplications creates an account-level self-hosted Access
// application for every requested hostname that has none.
func (c *Client) EnsureAccessApplications(ctx context.Context, logger logr.Logger, config *Config) error {
	if len(config.AccessAppRequests) == 0 {
		return nil
	}

	existing := make(map[string]struct{})
	pager := c.cloudflareAPI.ZeroTrust.Access.Applications.ListAutoPaging(ctx, zero_trust.AccessApplicationListParams{
		AccountID: cloudflare.F(c.accountID),
	})
	for pager.Next() {
		existing[pager.Current().Domain] = struct{}{}
	}
	if err := pager.Err(); err != nil {
		logger.Error(err, "Failed to list Access Applications")
		return err
	}

	for _, domain := range slices.Sorted(maps.Keys(config.AccessAppRequests)) {
		if _, ok := existing[domain]; ok {
			continue
		}
		app_name := config.AccessAppRequests[domain]
		logger.Info("Creating Access application", "domain", domain, "name", app_name)
		_, err := c.cloudflareAPI.ZeroTrust.Access.Applications.New(ctx, zero_trust.AccessApplicationNewParams{
			AccountID: cloudflare.F(c.accountID),
			Body: zero_trust.AccessApplicationNewParamsBodySelfHostedApplication{
				Name:   cloudflare.F(app_name),
				Domain: cloudflare.F(domain),
				Type:   cloudflare.F(zero_trust.ApplicationTypeSelfHosted),
			},
		})
		if err != nil {
			logger.Error(err, "Failed to create Access application", "domain", domain)
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

// desiredIngressRules converts the rules to their API form and appends the
// catch-all rule cloudflared requires last.
func desiredIngressRules(config *Config) []zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress {
	rules := make([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress, 0, len(config.Rules)+1)
	for _, record := range config.Rules {
		rules = append(rules, ingressRuleParam(record))
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

// synchronizeDns makes the tunnel CNAME records in every zone match the rule
// hostnames and returns the hostnames whose name another record holds. A zone
// the token can list but not read DNS records in is skipped, unless a rule
// needs a hostname in it.
func (c *Client) synchronizeDns(ctx context.Context, logger logr.Logger, config *Config, zone_map map[string]string) ([]string, error) {
	desired := make(map[string]map[string]struct{})
	for _, record := range config.Rules {
		if len(record.Hostname) == 0 {
			continue
		}
		zoneID := c.zoneIDFor(record.Hostname, zone_map)
		if len(zoneID) == 0 {
			logger.Info("Failed to find zone ID", "hostname", record.Hostname)
			continue
		}
		if _, ok := desired[zoneID]; !ok {
			desired[zoneID] = make(map[string]struct{})
		}
		desired[zoneID][record.Hostname] = struct{}{}
	}

	conflicts := make([]string, 0)
	for _, zoneID := range slices.Sorted(maps.Values(zone_map)) {
		records, err := c.listTunnelDnsRecords(ctx, zoneID)
		if err != nil {
			if cfErr, ok := errors.AsType[*cloudflare.Error](err); ok && cfErr.StatusCode == http.StatusForbidden && len(desired[zoneID]) == 0 {
				logger.Info("Skipping zone without DNS read permission", "zoneID", zoneID)
				continue
			}
			logger.Error(err, "Failed to list DNS records", "zoneID", zoneID)
			return nil, err
		}

		existing := make(map[string]struct{}, len(records))
		for _, record := range records {
			if _, ok := desired[zoneID][record.Name]; ok {
				existing[record.Name] = struct{}{}
				continue
			}
			logger.Info("Deleting DNS record", "name", record.Name)
			_, err := c.cloudflareAPI.DNS.Records.Delete(ctx, record.ID, dns.RecordDeleteParams{
				ZoneID: cloudflare.F(zoneID),
			})
			if err != nil {
				logger.Error(err, "Failed to delete DNS record", "name", record.Name)
				return nil, err
			}
		}

		for _, hostname := range slices.Sorted(maps.Keys(desired[zoneID])) {
			if _, ok := existing[hostname]; ok {
				continue
			}
			conflict, err := c.createDNSRecord(ctx, logger, zoneID, hostname)
			if err != nil {
				return nil, err
			}
			if conflict {
				conflicts = append(conflicts, hostname)
			}
		}
	}

	return conflicts, nil
}

// dnsRecordExistsCode is the Cloudflare API error code for a DNS name that
// another A, AAAA or CNAME record already holds.
const dnsRecordExistsCode = 81053

// createDNSRecord creates a proxied CNAME from hostname to the tunnel and
// reports a conflict when another record already holds the name.
func (c *Client) createDNSRecord(ctx context.Context, logger logr.Logger, zoneID, hostname string) (bool, error) {
	logger.Info("Creating DNS record", "name", hostname)
	_, err := c.cloudflareAPI.DNS.Records.New(ctx, dns.RecordNewParams{
		ZoneID: cloudflare.F(zoneID),
		Body: dns.CNAMERecordParam{
			Proxied: cloudflare.F(true),
			Type:    cloudflare.F(dns.CNAMERecordTypeCNAME),
			Name:    cloudflare.F(hostname),
			Content: cloudflare.F(c.tunnelID + "." + tunnelDomain),
			Comment: cloudflare.F("Automatically created by Cloudflare Tunnel Ingress Controller"),
		},
	})
	if err == nil {
		return false, nil
	}
	if cfErr, ok := errors.AsType[*cloudflare.Error](err); ok && slices.ContainsFunc(cfErr.Errors, func(e shared.ErrorData) bool {
		return e.Code == dnsRecordExistsCode
	}) {
		logger.Info("DNS name is held by another record", "name", hostname)
		return true, nil
	}
	logger.Error(err, "Failed to create DNS record", "name", hostname)
	return false, err
}

// listTunnelDnsRecords lists the DNS records in a zone that point to the
// tunnel. The caller logs a listing error, since a forbidden zone with no
// desired hostname is only worth an info-level log, not an error.
func (c *Client) listTunnelDnsRecords(ctx context.Context, zoneID string) ([]dns.RecordResponse, error) {
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
