package tunnel

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/cftest"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/go-logr/logr"
)

type originRequest = zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest

// newTestClient returns a Client for the fake's tunnel with the tunnel ID
// already resolved, so tests can call Sync without EnsureTunnelExists.
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

func newConfig(records ...*zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress) *Config {
	return &Config{Rules: records, AccessAppRequests: make(map[string]string)}
}

// recordingSink is a logr.LogSink that records every Info and Error call, for
// tests asserting on log level.
type recordingSink struct {
	mu      sync.Mutex
	entries []struct{ level, msg string }
}

// newRecordingLogger returns a logger backed by a fresh recordingSink.
func newRecordingLogger() (logr.Logger, *recordingSink) {
	sink := &recordingSink{}
	return logr.New(sink), sink
}

func (s *recordingSink) Init(logr.RuntimeInfo) {}
func (s *recordingSink) Enabled(int) bool      { return true }

func (s *recordingSink) Info(_ int, msg string, _ ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, struct{ level, msg string }{"info", msg})
}

func (s *recordingSink) Error(_ error, msg string, _ ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, struct{ level, msg string }{"error", msg})
}

func (s *recordingSink) WithValues(...any) logr.LogSink { return s }
func (s *recordingSink) WithName(string) logr.LogSink   { return s }

// count returns how many recorded log calls match level and msg.
func (s *recordingSink) count(level, msg string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entries {
		if e.level == level && e.msg == msg {
			n++
		}
	}
	return n
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

// lastPut decodes the rules of the last configuration write and fails the
// test when there was none.
func lastPut(t *testing.T, fake *cftest.Server) []map[string]any {
	t.Helper()
	if len(fake.ConfigPuts) == 0 {
		t.Fatal("expected a tunnel configuration update, got none")
	}
	return decodeRules(t, fake.ConfigPuts[len(fake.ConfigPuts)-1])
}

// routes reduces rules to their hostname and path, in order, for asserting
// on rule order.
func routes(rules []map[string]any) [][2]string {
	out := make([][2]string, 0, len(rules))
	for _, r := range rules {
		host, _ := r["hostname"].(string)
		path, _ := r["path"].(string)
		out = append(out, [2]string{host, path})
	}
	return out
}

func TestEnsureAccessApplications_CreatesAccountLevelApp(t *testing.T) {
	fake := cftest.New(t)
	c := newTestClient(fake)
	config := newConfig(record("app.example.com", "", "http://app.ns:80", originRequest{}))
	config.AccessAppRequests["app.example.com"] = "My App"

	if err := c.EnsureAccessApplications(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureAccessApplications: %v", err)
	}

	apps := fake.AccessApps()
	if len(apps) != 1 || apps[0].Scope != "accounts" || apps[0].Domain != "app.example.com" || apps[0].Name != "My App" {
		t.Errorf("unexpected Access applications: %+v", apps)
	}
}

func TestEnsureAccessApplications_KeepsExistingApp(t *testing.T) {
	fake := cftest.New(t)
	fake.AddAccessApp("Existing", "app.example.com")
	c := newTestClient(fake)
	config := newConfig(record("app.example.com", "", "http://app.ns:80", originRequest{}))
	config.AccessAppRequests["app.example.com"] = "My App"

	if err := c.EnsureAccessApplications(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureAccessApplications: %v", err)
	}

	if apps := fake.AccessApps(); len(apps) != 1 || apps[0].Name != "Existing" {
		t.Errorf("expected only the existing Access application, got %+v", apps)
	}
}

func TestSync_PushesOriginRequestChange(t *testing.T) {
	fake := cftest.New(t)
	fake.SetIngress(`[
		{"hostname":"x.example.com","service":"https://x.ns:443"},
		{"service":"http_status:404"}]`)
	c := newTestClient(fake)
	config := newConfig(record("x.example.com", "", "https://x.ns:443", originRequest{NoTLSVerify: true}))

	if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := lastPut(t, fake)[0]["originRequest"]; !reflect.DeepEqual(got, map[string]any{"noTLSVerify": true}) {
		t.Errorf("expected originRequest {noTLSVerify: true}, got %v", got)
	}
}

func TestSync_RemovesStaleRules(t *testing.T) {
	fake := cftest.New(t)
	fake.SetIngress(`[
		{"hostname":"x.example.com","path":"/a","service":"http://x.ns:80"},
		{"hostname":"gone.example.com","service":"http://gone.ns:80"},
		{"service":"http_status:404"}]`)
	c := newTestClient(fake)
	config := newConfig(record("x.example.com", "/a", "http://x.ns:80", originRequest{}))

	if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	want := [][2]string{{"x.example.com", "/a"}, {"", ""}}
	if got := routes(lastPut(t, fake)); !reflect.DeepEqual(got, want) {
		t.Errorf("expected routes %v, got %v", want, got)
	}
}

func TestSync_ReplacesExplicitZeroOriginSettings(t *testing.T) {
	fake := cftest.New(t)
	fake.SetIngress(`[
		{"hostname":"x.example.com","service":"http://x.ns:80","originRequest":{
			"access":{"audTag":[],"teamName":"","required":false},
			"connectTimeout":0,"keepAliveConnections":0,"noTLSVerify":false,"tcpKeepAlive":0,"tlsTimeout":0}},
		{"service":"http_status:404"}]`)
	c := newTestClient(fake)
	config := newConfig(record("x.example.com", "", "http://x.ns:80", originRequest{}))

	if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if origin, ok := lastPut(t, fake)[0]["originRequest"]; ok {
		t.Errorf("expected no originRequest for a rule without settings, got %v", origin)
	}
}

func TestSync_SendsOnlySetOriginSettings(t *testing.T) {
	fake := cftest.New(t)
	c := newTestClient(fake)
	config := newConfig(record("x.example.com", "", "http://x.ns:80", originRequest{
		ConnectTimeout:   5,
		KeepAliveTimeout: 120,
		HTTPHostHeader:   "internal.example.com",
	}))

	if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	want := map[string]any{"connectTimeout": float64(5), "keepAliveTimeout": float64(120), "httpHostHeader": "internal.example.com"}
	if got := lastPut(t, fake)[0]["originRequest"]; !reflect.DeepEqual(got, want) {
		t.Errorf("expected originRequest %v, got %v", want, got)
	}
}

func TestSync_KeepsRuleOrderAndAppendsCatchAll(t *testing.T) {
	fake := cftest.New(t)
	c := newTestClient(fake)
	config := newConfig(
		record("b.example.com", "", "http://b.ns:80", originRequest{}),
		record("a.example.com", "^/x", "http://a.ns:80", originRequest{}),
		record("k.example.com", "", "tcp://kubernetes.default.svc:443", originRequest{ProxyType: "socks"}),
	)

	if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	rules := lastPut(t, fake)
	want := [][2]string{{"b.example.com", ""}, {"a.example.com", "^/x"}, {"k.example.com", ""}, {"", ""}}
	if got := routes(rules); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected routes %v, got %v", want, got)
	}
	if rules[3]["service"] != "http_status:404" {
		t.Errorf("expected http_status:404 catch-all last, got %v", rules[3])
	}
}

func TestSync_NoUpdateWhenInSync(t *testing.T) {
	fake := cftest.New(t)
	c := newTestClient(fake)
	config := newConfig(
		record("a.example.com", "", "http://a.ns:80", originRequest{NoTLSVerify: true}),
		record("b.example.com", "^/static(/.*)?$", "http://b.ns:81", originRequest{}),
		record("k.example.com", "", "tcp://kubernetes.default.svc:443", originRequest{ProxyType: "socks"}),
	)

	for range 3 {
		if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
			t.Fatalf("Sync: %v", err)
		}
	}

	if len(fake.ConfigPuts) != 1 {
		t.Errorf("expected exactly 1 configuration update across repeated syncs, got %d", len(fake.ConfigPuts))
	}
	if fake.DNSCreates() != 3 {
		t.Errorf("expected 3 DNS records created once, got %d creates", fake.DNSCreates())
	}
}
