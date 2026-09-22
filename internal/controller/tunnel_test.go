package controller

import (
	"strings"
	"testing"

	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
)

func TestApplyOriginRequestAnnotations_AccessRequired(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationAccessRequired: "true",
	})

	if !origin.Access.Required {
		t.Error("expected Access.Required to be true")
	}
}

func TestApplyOriginRequestAnnotations_AccessTeamName(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationAccessTeamName: "myteam",
	})

	if origin.Access.TeamName != "myteam" {
		t.Errorf("expected Access.TeamName = 'myteam', got %q", origin.Access.TeamName)
	}
}

func TestApplyOriginRequestAnnotations_AccessAudTag(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
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
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationAccessRequired: "notabool",
	})

	if origin.Access.Required {
		t.Error("expected Access.Required to remain false on invalid input")
	}
}

func TestApplyOriginRequestAnnotations_OriginConnectTimeout(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginConnectTimeout: "5s",
	})

	expected := int64(5)
	if origin.ConnectTimeout != expected {
		t.Errorf("expected ConnectTimeout = %d, got %d", expected, origin.ConnectTimeout)
	}
}

func TestApplyOriginRequestAnnotations_OriginConnectTimeoutInvalid(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginConnectTimeout: "invalid",
	})

	if origin.ConnectTimeout != 0 {
		t.Errorf("expected ConnectTimeout = 0 on invalid input, got %d", origin.ConnectTimeout)
	}
}

func TestApplyOriginRequestAnnotations_OriginTlsTimeout(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginTlsTimeout: "10s",
	})

	expected := int64(10)
	if origin.TLSTimeout != expected {
		t.Errorf("expected TLSTimeout = %d, got %d", expected, origin.TLSTimeout)
	}
}

func TestApplyOriginRequestAnnotations_OriginNoTlsVerify(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginNoTlsVerify: "true",
	})

	if !origin.NoTLSVerify {
		t.Error("expected NoTLSVerify to be true")
	}
}

func TestApplyOriginRequestAnnotations_OriginKeepaliveConnections(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginKeepaliveConnections: "10",
	})

	if origin.KeepAliveConnections != 10 {
		t.Errorf("expected KeepAliveConnections = 10, got %d", origin.KeepAliveConnections)
	}
}

func TestApplyOriginRequestAnnotations_OriginHttpHostHeader(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginHttpHostHeader: "example.com",
	})

	if origin.HTTPHostHeader != "example.com" {
		t.Errorf("expected HTTPHostHeader = 'example.com', got %q", origin.HTTPHostHeader)
	}
}

func TestApplyOriginRequestAnnotations_OriginServerName(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginServerName: "origin.example.com",
	})

	if origin.OriginServerName != "origin.example.com" {
		t.Errorf("expected OriginServerName = 'origin.example.com', got %q", origin.OriginServerName)
	}
}

func TestApplyOriginRequestAnnotations_ProxyType(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginProxyType: "socks",
	})

	if origin.ProxyType != "socks" {
		t.Errorf("expected ProxyType = 'socks', got %q", origin.ProxyType)
	}
}

func TestApplyOriginRequestAnnotations_Http2Origin(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginHttp2Origin: "true",
	})

	if !origin.HTTP2Origin {
		t.Error("expected HTTP2Origin to be true")
	}
}

func TestApplyOriginRequestAnnotations_DisableChunkedEncoding(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginDisableChunkedEncoding: "true",
	})

	if !origin.DisableChunkedEncoding {
		t.Error("expected DisableChunkedEncoding to be true")
	}
}

func TestApplyOriginRequestAnnotations_NoHappyEyeballs(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginNoHappyEyeballs: "true",
	})

	if !origin.NoHappyEyeballs {
		t.Error("expected NoHappyEyeballs to be true")
	}
}

func TestApplyOriginRequestAnnotations_MultipleAnnotations(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
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
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{})

	if origin.Access.Required || origin.Access.TeamName != "" || origin.ConnectTimeout != 0 {
		t.Error("expected no fields to be set with empty annotations")
	}
}

func TestApplyOriginRequestAnnotations_TimeoutsInSeconds(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
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
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginConnectTimeout: "1500ms",
		AnnotationOriginTlsTimeout:     "0s",
	})

	if origin.ConnectTimeout != 0 || origin.TLSTimeout != 0 {
		t.Errorf("expected timeouts that are not whole positive seconds to be ignored, got connect=%d tls=%d", origin.ConnectTimeout, origin.TLSTimeout)
	}
}

func TestApplyOriginRequestAnnotations_ReportsInvalidValues(t *testing.T) {
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}

	warnings := applyOriginRequestAnnotations(&origin, map[string]string{
		AnnotationOriginNoTlsVerify:    "maybe",
		AnnotationOriginConnectTimeout: "1500ms",
	})

	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %+v", warnings)
	}
	for _, w := range warnings {
		if w.Reason != ReasonInvalidAnnotation {
			t.Errorf("expected reason %s, got %s", ReasonInvalidAnnotation, w.Reason)
		}
	}
	if !strings.Contains(warnings[0].Message, AnnotationOriginConnectTimeout) || !strings.Contains(warnings[1].Message, AnnotationOriginNoTlsVerify) {
		t.Errorf("expected warnings ordered by annotation name, got %+v", warnings)
	}
}

func TestKubernetesApiTunnelConfig_GetService(t *testing.T) {
	config := KubernetesApiTunnelConfig{Server: "kubernetes.default.svc:443"}

	if got := config.GetService(); got != "tcp://kubernetes.default.svc:443" {
		t.Errorf("expected tcp://kubernetes.default.svc:443, got %q", got)
	}
}
