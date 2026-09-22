package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/go-logr/logr"
)

func (c *IngressController) ensureCloudflareTunnelExists(ctx context.Context, logger logr.Logger) (string, error) {
	logger.Info("Ensuring Cloudflare Tunnel exists")
	if err := c.tunnelClient.EnsureTunnelExists(ctx, logger); err != nil {
		logger.Error(err, "Failed to ensure Cloudflare Tunnel exists")
		return "", err
	}

	token, err := c.tunnelClient.GetTunnelToken(ctx)
	if err != nil {
		logger.Error(err, "Failed to get Cloudflare Tunnel token")
		return "", err
	}
	return token, nil
}

func applyOriginRequestAnnotations(origin_config *zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest, annotations map[string]string) []warning {
	var warnings []warning
	for _, k := range slices.Sorted(maps.Keys(annotations)) {
		v := annotations[k]
		switch k {
		case AnnotationAccessRequired:
			t, err := strconv.ParseBool(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.Access.Required = t
			}
		case AnnotationAccessTeamName:
			origin_config.Access.TeamName = v
		case AnnotationAccessAudTag:
			origin_config.Access.AUDTag = strings.Split(v, ",")
		case AnnotationOriginConnectTimeout:
			t, err := parseSeconds(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.ConnectTimeout = t
			}
		case AnnotationOriginTlsTimeout:
			t, err := parseSeconds(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.TLSTimeout = t
			}
		case AnnotationOriginTcpKeepalive:
			t, err := parseSeconds(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.TCPKeepAlive = t
			}
		case AnnotationOriginNoHappyEyeballs:
			t, err := strconv.ParseBool(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.NoHappyEyeballs = t
			}
		case AnnotationOriginKeepaliveConnections:
			t, err := strconv.Atoi(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.KeepAliveConnections = int64(t)
			}
		case AnnotationOriginKeepaliveTimeout:
			t, err := parseSeconds(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.KeepAliveTimeout = t
			}
		case AnnotationOriginHttpHostHeader:
			origin_config.HTTPHostHeader = v
		case AnnotationOriginServerName:
			origin_config.OriginServerName = v
		case AnnotationOriginNoTlsVerify:
			t, err := strconv.ParseBool(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.NoTLSVerify = t
			}
		case AnnotationOriginDisableChunkedEncoding:
			t, err := strconv.ParseBool(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.DisableChunkedEncoding = t
			}
		case AnnotationOriginProxyType:
			origin_config.ProxyType = v
		case AnnotationOriginHttp2Origin:
			t, err := strconv.ParseBool(v)
			if err != nil {
				warnings = append(warnings, invalidAnnotation(k, err))
			} else {
				origin_config.HTTP2Origin = t
			}
		}
	}
	return warnings
}

func invalidAnnotation(annotation string, err error) warning {
	return warning{Reason: ReasonInvalidAnnotation, Message: fmt.Sprintf("annotation %s: %v", annotation, err)}
}

// backendScheme returns the origin URL scheme selected by the
// backend-protocol annotation, http by default.
func backendScheme(annotations map[string]string) (string, []warning) {
	value, ok := annotations[AnnotationBackendProtocol]
	if !ok {
		return "http", nil
	}
	for _, protocol := range SupportedBackendProtocols {
		if strings.EqualFold(value, protocol) {
			return strings.ToLower(protocol), nil
		}
	}
	return "http", []warning{{
		Reason:  ReasonInvalidAnnotation,
		Message: fmt.Sprintf("annotation %s: unsupported value %q, using http", AnnotationBackendProtocol, value),
	}}
}

// parseSeconds parses a duration that is a whole, positive number of seconds,
// the unit of origin timeouts in the tunnel configuration.
func parseSeconds(value string) (int64, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d < time.Second || d%time.Second != 0 {
		return 0, fmt.Errorf("duration %q must be a whole number of seconds, at least 1s", value)
	}
	return int64(d / time.Second), nil
}
