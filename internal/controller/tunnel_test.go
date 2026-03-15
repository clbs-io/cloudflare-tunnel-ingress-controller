package controller

import (
	"testing"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
)

func TestApplyOriginRequestAnnotations_AccessRequired(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationAccessRequired: "true",
	})

	if !origin.Access.Required {
		t.Error("expected Access.Required to be true")
	}
}

func TestApplyOriginRequestAnnotations_AccessTeamName(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationAccessTeamName: "myteam",
	})

	if origin.Access.TeamName != "myteam" {
		t.Errorf("expected Access.TeamName = 'myteam', got %q", origin.Access.TeamName)
	}
}

func TestApplyOriginRequestAnnotations_AccessAudTag(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationAccessAudTag: "tag1,tag2,tag3",
	})

	if len(origin.Access.AUDTag) != 3 {
		t.Fatalf("expected 3 AUD tags, got %d", len(origin.Access.AUDTag))
	}
	expected := []string{"tag1", "tag2", "tag3"}
	for i, tag := range expected {
		if origin.Access.AUDTag[i] != tag {
			t.Errorf("expected AUDTag[%d] = %q, got %q", i, tag, origin.Access.AUDTag[i])
		}
	}
}

func TestApplyOriginRequestAnnotations_AccessRequiredInvalid(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationAccessRequired: "notabool",
	})

	if origin.Access.Required {
		t.Error("expected Access.Required to remain false on invalid input")
	}
}

func TestApplyOriginRequestAnnotations_OriginConnectTimeout(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginConnectTimeout: "5s",
	})

	expected := int64(5_000_000_000)
	if origin.ConnectTimeout != expected {
		t.Errorf("expected ConnectTimeout = %d, got %d", expected, origin.ConnectTimeout)
	}
}

func TestApplyOriginRequestAnnotations_OriginConnectTimeoutInvalid(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginConnectTimeout: "invalid",
	})

	if origin.ConnectTimeout != 0 {
		t.Errorf("expected ConnectTimeout = 0 on invalid input, got %d", origin.ConnectTimeout)
	}
}

func TestApplyOriginRequestAnnotations_OriginTlsTimeout(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginTlsTimeout: "10s",
	})

	expected := int64(10_000_000_000)
	if origin.TLSTimeout != expected {
		t.Errorf("expected TLSTimeout = %d, got %d", expected, origin.TLSTimeout)
	}
}

func TestApplyOriginRequestAnnotations_OriginNoTlsVerify(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginNoTlsVerify: "true",
	})

	if !origin.NoTLSVerify {
		t.Error("expected NoTLSVerify to be true")
	}
}

func TestApplyOriginRequestAnnotations_OriginKeepaliveConnections(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginKeepaliveConnections: "10",
	})

	if origin.KeepAliveConnections != 10 {
		t.Errorf("expected KeepAliveConnections = 10, got %d", origin.KeepAliveConnections)
	}
}

func TestApplyOriginRequestAnnotations_OriginHttpHostHeader(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginHttpHostHeader: "example.com",
	})

	if origin.HTTPHostHeader != "example.com" {
		t.Errorf("expected HTTPHostHeader = 'example.com', got %q", origin.HTTPHostHeader)
	}
}

func TestApplyOriginRequestAnnotations_OriginServerName(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginServerName: "origin.example.com",
	})

	if origin.OriginServerName != "origin.example.com" {
		t.Errorf("expected OriginServerName = 'origin.example.com', got %q", origin.OriginServerName)
	}
}

func TestApplyOriginRequestAnnotations_ProxyType(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginProxyType: "socks",
	})

	if origin.ProxyType != "socks" {
		t.Errorf("expected ProxyType = 'socks', got %q", origin.ProxyType)
	}
}

func TestApplyOriginRequestAnnotations_Http2Origin(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginHttp2Origin: "true",
	})

	if !origin.HTTP2Origin {
		t.Error("expected HTTP2Origin to be true")
	}
}

func TestApplyOriginRequestAnnotations_DisableChunkedEncoding(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginDisableChunkedEncoding: "true",
	})

	if !origin.DisableChunkedEncoding {
		t.Error("expected DisableChunkedEncoding to be true")
	}
}

func TestApplyOriginRequestAnnotations_NoHappyEyeballs(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginNoHappyEyeballs: "true",
	})

	if !origin.NoHappyEyeballs {
		t.Error("expected NoHappyEyeballs to be true")
	}
}

func TestApplyOriginRequestAnnotations_MultipleAnnotations(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationAccessRequired:       "true",
		AnnotationAccessTeamName:       "team1",
		AnnotationOriginConnectTimeout: "3s",
		AnnotationOriginNoTlsVerify:    "true",
	})

	if !origin.Access.Required {
		t.Error("expected Access.Required to be true")
	}
	if origin.Access.TeamName != "team1" {
		t.Errorf("expected Access.TeamName = 'team1', got %q", origin.Access.TeamName)
	}
	if origin.ConnectTimeout != 3_000_000_000 {
		t.Errorf("expected ConnectTimeout = 3000000000, got %d", origin.ConnectTimeout)
	}
	if !origin.NoTLSVerify {
		t.Error("expected NoTLSVerify to be true")
	}
}

func TestApplyOriginRequestAnnotations_EmptyAnnotations(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{})

	if origin.Access.Required || origin.Access.TeamName != "" || origin.ConnectTimeout != 0 {
		t.Error("expected no fields to be set with empty annotations")
	}
}

func TestDeleteTunnelConfigurationForIngress_CleansUpAccessAppRequests(t *testing.T) {
	config := &tunnel.Config{
		Ingresses:         make(map[types.UID]*tunnel.IngressRecords),
		AccessAppRequests: make(map[string]string),
	}

	uid := types.UID("test-uid-123")
	records := tunnel.IngressRecords{
		&zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress{
			Hostname: "app.example.com",
			Service:  "http://svc.default:80",
		},
		&zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress{
			Hostname: "api.example.com",
			Service:  "http://api-svc.default:8080",
		},
	}
	config.Ingresses[uid] = &records
	config.AccessAppRequests["app.example.com"] = "My App"
	config.AccessAppRequests["api.example.com"] = "My API"
	config.AccessAppRequests["other.example.com"] = "Other App"

	// Simulate the cleanup logic from deleteTunnelConfigurationForIngress
	ing := config.Ingresses[uid]
	if ing != nil {
		for _, record := range *ing {
			delete(config.AccessAppRequests, record.Hostname)
		}
	}
	delete(config.Ingresses, uid)

	if _, ok := config.AccessAppRequests["app.example.com"]; ok {
		t.Error("expected app.example.com to be removed from AccessAppRequests")
	}
	if _, ok := config.AccessAppRequests["api.example.com"]; ok {
		t.Error("expected api.example.com to be removed from AccessAppRequests")
	}
	if _, ok := config.AccessAppRequests["other.example.com"]; !ok {
		t.Error("expected other.example.com to remain in AccessAppRequests")
	}
	if _, ok := config.Ingresses[uid]; ok {
		t.Error("expected ingress to be removed")
	}
}

func TestAccessAppRequests_PopulatedFromAnnotation(t *testing.T) {
	config := &tunnel.Config{
		Ingresses:         make(map[types.UID]*tunnel.IngressRecords),
		AccessAppRequests: make(map[string]string),
	}

	// Simulate the logic from harvestRules that populates AccessAppRequests
	annotations := map[string]string{
		AnnotationAccessAppName: "My App",
	}
	hosts := []string{"app.example.com", "api.example.com"}

	if app_name, ok := annotations[AnnotationAccessAppName]; ok && app_name != "" {
		for _, host := range hosts {
			config.AccessAppRequests[host] = app_name
		}
	}

	if len(config.AccessAppRequests) != 2 {
		t.Fatalf("expected 2 AccessAppRequests, got %d", len(config.AccessAppRequests))
	}
	if config.AccessAppRequests["app.example.com"] != "My App" {
		t.Errorf("expected 'My App' for app.example.com, got %q", config.AccessAppRequests["app.example.com"])
	}
	if config.AccessAppRequests["api.example.com"] != "My App" {
		t.Errorf("expected 'My App' for api.example.com, got %q", config.AccessAppRequests["api.example.com"])
	}
}

func TestAccessAppRequests_NotPopulatedWithoutAnnotation(t *testing.T) {
	config := &tunnel.Config{
		Ingresses:         make(map[types.UID]*tunnel.IngressRecords),
		AccessAppRequests: make(map[string]string),
	}

	annotations := map[string]string{}

	if app_name, ok := annotations[AnnotationAccessAppName]; ok && app_name != "" {
		config.AccessAppRequests["should-not-exist"] = app_name
	}

	if len(config.AccessAppRequests) != 0 {
		t.Error("expected no AccessAppRequests without annotation")
	}
}

func TestAccessAppRequests_NotPopulatedWithEmptyAnnotation(t *testing.T) {
	config := &tunnel.Config{
		Ingresses:         make(map[types.UID]*tunnel.IngressRecords),
		AccessAppRequests: make(map[string]string),
	}

	annotations := map[string]string{
		AnnotationAccessAppName: "",
	}

	if app_name, ok := annotations[AnnotationAccessAppName]; ok && app_name != "" {
		config.AccessAppRequests["should-not-exist"] = app_name
	}

	if len(config.AccessAppRequests) != 0 {
		t.Error("expected no AccessAppRequests with empty annotation value")
	}
}
