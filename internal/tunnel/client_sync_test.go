package tunnel

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/cftest"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
)

type originRequest = zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest

func newTestClient(fake *cftest.Server) *Client {
	c := NewClient(fake.Client(), cftest.AccountID, cftest.TunnelName, logr.Discard())
	c.tunnelID = cftest.TunnelID
	return c
}

func record(hostname, path, service string, origin originRequest) *zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress {
	return &zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress{
		Hostname:      hostname,
		Path:          path,
		Service:       service,
		OriginRequest: origin,
	}
}

func newConfig(ingresses map[types.UID]IngressRecords) *Config {
	config := &Config{
		Ingresses:         make(map[types.UID]*IngressRecords),
		AccessAppRequests: make(map[string]string),
	}
	for uid, records := range ingresses {
		config.Ingresses[uid] = &records
	}
	return config
}

// decodeRules decodes an ingress array as sent to or stored by the Cloudflare API.
func decodeRules(t *testing.T, raw json.RawMessage) []map[string]any {
	t.Helper()
	var rules []map[string]any
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatalf("decode ingress rules: %v", err)
	}
	return rules
}

func lastPut(t *testing.T, fake *cftest.Server) []map[string]any {
	t.Helper()
	if len(fake.ConfigPuts) == 0 {
		t.Fatal("expected a tunnel configuration update, got none")
	}
	return decodeRules(t, fake.ConfigPuts[len(fake.ConfigPuts)-1])
}

func routes(rules []map[string]any) [][2]string {
	out := make([][2]string, 0, len(rules))
	for _, r := range rules {
		host, _ := r["hostname"].(string)
		path, _ := r["path"].(string)
		out = append(out, [2]string{host, path})
	}
	return out
}

func TestEnsureTunnelConfiguration_CreatesAccountLevelAccessApp(t *testing.T) {
	fake := cftest.New(t)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("app.example.com", "/", "http://app.ns:80", originRequest{})},
	})
	config.AccessAppRequests["app.example.com"] = "My App"

	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	apps := fake.AccessApps()
	if len(apps) != 1 {
		t.Fatalf("expected 1 Access application, got %d", len(apps))
	}
	if apps[0].Scope != "accounts" || apps[0].Domain != "app.example.com" || apps[0].Name != "My App" {
		t.Errorf("unexpected Access application: %+v", apps[0])
	}
}

func TestEnsureTunnelConfiguration_KeepsExistingAccessApp(t *testing.T) {
	fake := cftest.New(t)
	fake.AddAccessApp("Existing", "app.example.com")
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("app.example.com", "/", "http://app.ns:80", originRequest{})},
	})
	config.AccessAppRequests["app.example.com"] = "My App"

	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	if apps := fake.AccessApps(); len(apps) != 1 || apps[0].Name != "Existing" {
		t.Errorf("expected only the existing Access application, got %+v", apps)
	}
}

func TestEnsureTunnelConfiguration_PushesOriginRequestChange(t *testing.T) {
	fake := cftest.New(t)
	fake.SetIngress(`[
		{"hostname":"x.example.com","path":"/","service":"https://x.ns:443"},
		{"service":"http_status:404"}]`)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("x.example.com", "/", "https://x.ns:443", originRequest{NoTLSVerify: true})},
	})

	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	rules := lastPut(t, fake)
	if got := rules[0]["originRequest"]; !reflect.DeepEqual(got, map[string]any{"noTLSVerify": true}) {
		t.Errorf("expected originRequest {noTLSVerify: true}, got %v", got)
	}
}

func TestEnsureTunnelConfiguration_RemovesStaleRules(t *testing.T) {
	fake := cftest.New(t)
	fake.SetIngress(`[
		{"hostname":"x.example.com","path":"/a","service":"http://x.ns:80"},
		{"hostname":"x.example.com","path":"/b","service":"http://x.ns:80"},
		{"hostname":"gone.example.com","path":"/","service":"http://gone.ns:80"},
		{"service":"http_status:404"}]`)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("x.example.com", "/a", "http://x.ns:80", originRequest{})},
	})

	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	want := [][2]string{{"x.example.com", "/a"}, {"", ""}}
	if got := routes(lastPut(t, fake)); !reflect.DeepEqual(got, want) {
		t.Errorf("expected routes %v, got %v", want, got)
	}
}

func TestEnsureTunnelConfiguration_ReplacesExplicitZeroOriginSettings(t *testing.T) {
	fake := cftest.New(t)
	fake.SetIngress(`[
		{"hostname":"x.example.com","path":"/","service":"http://x.ns:80","originRequest":{
			"access":{"audTag":[],"teamName":"","required":false},
			"caPool":"","connectTimeout":0,"disableChunkedEncoding":false,"http2Origin":false,
			"httpHostHeader":"","keepAliveConnections":0,"noHappyEyeballs":false,"noTLSVerify":false,
			"originServerName":"","proxyType":"","tcpKeepAlive":0,"tlsTimeout":0}},
		{"service":"http_status:404"}]`)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("x.example.com", "/", "http://x.ns:80", originRequest{})},
	})

	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	if origin, ok := lastPut(t, fake)[0]["originRequest"]; ok {
		t.Errorf("expected no originRequest for a rule without settings, got %v", origin)
	}
}

func TestEnsureTunnelConfiguration_SendsOnlySetOriginSettings(t *testing.T) {
	fake := cftest.New(t)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("x.example.com", "/", "http://x.ns:80", originRequest{
			ConnectTimeout:   5,
			KeepAliveTimeout: 120,
			HTTPHostHeader:   "internal.example.com",
		})},
	})

	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	want := map[string]any{"connectTimeout": float64(5), "keepAliveTimeout": float64(120), "httpHostHeader": "internal.example.com"}
	if got := lastPut(t, fake)[0]["originRequest"]; !reflect.DeepEqual(got, want) {
		t.Errorf("expected originRequest %v, got %v", want, got)
	}
}

func TestEnsureTunnelConfiguration_NoUpdateWhenInSync(t *testing.T) {
	fake := cftest.New(t)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("a.example.com", "/", "http://a.ns:80", originRequest{NoTLSVerify: true})},
		"u2": {record("b.example.com", "/static", "http://b.ns:81", originRequest{}), record("b.example.com", "/", "http://b.ns:80", originRequest{})},
		"u3": {record("c.example.com", "/", "http://c.ns:80", originRequest{ConnectTimeout: 5})},
	})
	config.KubernetesApiTunnelConfig = KubernetesApiTunnelConfig{Enabled: true, Server: "kubernetes.default.svc:443", Domain: "k.example.com"}
	fake.AddAccessApp("Kubernetes API", "k.example.com")

	for range 3 {
		if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
			t.Fatalf("EnsureTunnelConfiguration: %v", err)
		}
	}

	if len(fake.ConfigPuts) != 1 {
		t.Errorf("expected exactly 1 configuration update across repeated reconciles, got %d", len(fake.ConfigPuts))
	}
}

func TestEnsureTunnelConfiguration_KubernetesApiRuleBeforeCatchAll(t *testing.T) {
	fake := cftest.New(t)
	fake.AddAccessApp("Kubernetes API", "k.example.com")
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("a.example.com", "/", "http://a.ns:80", originRequest{})},
	})
	config.KubernetesApiTunnelConfig = KubernetesApiTunnelConfig{Enabled: true, Server: "kubernetes.default.svc:443", Domain: "k.example.com"}

	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	rules := lastPut(t, fake)
	want := [][2]string{{"a.example.com", "/"}, {"k.example.com", ""}, {"", ""}}
	if got := routes(rules); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected routes %v, got %v", want, got)
	}
	if rules[1]["service"] != "tcp://kubernetes.default.svc:443" || !reflect.DeepEqual(rules[1]["originRequest"], map[string]any{"proxyType": "socks"}) {
		t.Errorf("unexpected Kubernetes API rule: %v", rules[1])
	}
	if rules[2]["service"] != "http_status:404" {
		t.Errorf("expected http_status:404 catch-all last, got %v", rules[2])
	}
}
