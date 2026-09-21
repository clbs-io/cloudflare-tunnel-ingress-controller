package controller

import (
	"testing"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/cftest"
	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
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

	expected := int64(5)
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

	expected := int64(10)
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
	if origin.ConnectTimeout != 3 {
		t.Errorf("expected ConnectTimeout = 3, got %d", origin.ConnectTimeout)
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

func TestApplyOriginRequestAnnotations_TimeoutsInSeconds(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginTcpKeepalive:     "30s",
		AnnotationOriginKeepaliveTimeout: "1m30s",
	})

	if origin.TCPKeepAlive != 30 {
		t.Errorf("expected TCPKeepAlive = 30, got %d", origin.TCPKeepAlive)
	}
	if origin.KeepAliveTimeout != 90 {
		t.Errorf("expected KeepAliveTimeout = 90, got %d", origin.KeepAliveTimeout)
	}
}

func TestApplyOriginRequestAnnotations_TimeoutRejectsPartialSeconds(t *testing.T) {
	logger := logr.Discard()
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(logger, &origin, map[string]string{
		AnnotationOriginConnectTimeout: "1500ms",
		AnnotationOriginTlsTimeout:     "0s",
	})

	if origin.ConnectTimeout != 0 || origin.TLSTimeout != 0 {
		t.Errorf("expected timeouts that are not whole positive seconds to be ignored, got connect=%d tls=%d", origin.ConnectTimeout, origin.TLSTimeout)
	}
}

func newTestIngressController(t *testing.T, fake *cftest.Server) *IngressController {
	t.Helper()
	tunnelClient := tunnel.NewClient(fake.Client(), cftest.AccountID, cftest.TunnelName, logr.Discard())
	if err := tunnelClient.EnsureTunnelExists(t.Context(), logr.Discard()); err != nil {
		t.Fatalf("EnsureTunnelExists: %v", err)
	}
	return &IngressController{tunnelClient: tunnelClient}
}

func newTestIngress(uid types.UID, hosts ...string) *networkingv1.Ingress {
	ing := &networkingv1.Ingress{UID: uid, Name: "app", Namespace: "ns"}
	for _, host := range hosts {
		ing.Spec.Rules = append(ing.Spec.Rules, networkingv1.IngressRule{Host: host})
	}
	return ing
}

func newTestTunnelConfig(uid types.UID, hosts ...string) *tunnel.Config {
	config := &tunnel.Config{
		Ingresses:         make(map[types.UID]*tunnel.IngressRecords),
		AccessAppRequests: make(map[string]string),
	}
	records := make(tunnel.IngressRecords, 0, len(hosts))
	for _, host := range hosts {
		records = append(records, &zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress{
			Hostname: host,
			Path:     "/",
			Service:  "http://svc.ns:80",
		})
		config.AccessAppRequests[host] = "My App"
	}
	config.Ingresses[uid] = &records
	return config
}

func TestDeleteTunnelConfigurationForIngress_RemovesIngressAndAccessAppRequests(t *testing.T) {
	fake := cftest.New(t)
	c := newTestIngressController(t, fake)
	uid := types.UID("test-uid-123")
	config := newTestTunnelConfig(uid, "app.example.com", "api.example.com")
	config.AccessAppRequests["other.example.com"] = "Other App"

	if err := c.deleteTunnelConfigurationForIngress(t.Context(), logr.Discard(), config, newTestIngress(uid, "app.example.com", "api.example.com")); err != nil {
		t.Fatalf("deleteTunnelConfigurationForIngress: %v", err)
	}

	if _, ok := config.Ingresses[uid]; ok {
		t.Error("expected ingress to be removed")
	}
	if _, ok := config.AccessAppRequests["app.example.com"]; ok {
		t.Error("expected app.example.com to be removed from AccessAppRequests")
	}
	if _, ok := config.AccessAppRequests["api.example.com"]; ok {
		t.Error("expected api.example.com to be removed from AccessAppRequests")
	}
	if _, ok := config.AccessAppRequests["other.example.com"]; !ok {
		t.Error("expected other.example.com to remain in AccessAppRequests")
	}
}

func TestDeleteTunnelConfigurationForIngress_KeepsStateWhenUpdateFails(t *testing.T) {
	fake := cftest.New(t)
	fake.SetIngress(`[{"hostname":"app.example.com","path":"/","service":"http://svc.ns:80"},{"service":"http_status:404"}]`)
	fake.FailConfigPut = true
	c := newTestIngressController(t, fake)
	uid := types.UID("test-uid-123")
	config := newTestTunnelConfig(uid, "app.example.com")

	if err := c.deleteTunnelConfigurationForIngress(t.Context(), logr.Discard(), config, newTestIngress(uid, "app.example.com")); err == nil {
		t.Fatal("expected an error when the tunnel configuration update fails")
	}

	if _, ok := config.Ingresses[uid]; !ok {
		t.Error("expected ingress records to be kept for the retry")
	}
	if _, ok := config.AccessAppRequests["app.example.com"]; !ok {
		t.Error("expected Access application request to be kept for the retry")
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
