package controller

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var testCreated = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type testPath struct {
	path     string
	pathType networkingv1.PathType
	service  string
	portName string
}

type testRule struct {
	host  string
	paths []testPath
}

func prefixPath(path, service string) testPath {
	return testPath{path: path, pathType: networkingv1.PathTypePrefix, service: service}
}

func exactPath(path, service string) testPath {
	return testPath{path: path, pathType: networkingv1.PathTypeExact, service: service}
}

func regexPath(path, service string) testPath {
	return testPath{path: path, pathType: networkingv1.PathTypeImplementationSpecific, service: service}
}

// testIngress builds an Ingress in namespace "ns" created minute minutes
// after testCreated; services listen on port 80 unless a port name is set.
func testIngress(name string, minute int, rules ...testRule) networkingv1.Ingress {
	ingress := networkingv1.Ingress{
		UID:               types.UID("uid-" + name),
		Name:              name,
		Namespace:         "ns",
		CreationTimestamp: metav1.NewTime(testCreated.Add(time.Duration(minute) * time.Minute)),
	}
	for _, r := range rules {
		rule := networkingv1.IngressRule{Host: r.host, HTTP: &networkingv1.HTTPIngressRuleValue{}}
		for _, p := range r.paths {
			port := networkingv1.ServiceBackendPort{Number: 80}
			if len(p.portName) > 0 {
				port = networkingv1.ServiceBackendPort{Name: p.portName}
			}
			rule.HTTP.Paths = append(rule.HTTP.Paths, networkingv1.HTTPIngressPath{
				Path:     p.path,
				PathType: new(p.pathType),
				Backend: networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{Name: p.service, Port: port},
				},
			})
		}
		ingress.Spec.Rules = append(ingress.Spec.Rules, rule)
	}
	return ingress
}

// noServices is a portLookup that finds no Service.
func noServices(namespace, service, port string) (int32, error) {
	return 0, errors.New("no services")
}

// svc is the origin URL render produces for Service name on port 80 in the
// namespace testIngress uses.
func svc(name string) string {
	return fmt.Sprintf("http://%s.ns:80", name)
}

// firstMatch returns the service of the first rule cloudflared matches for
// host and path, or "" when only the catch-all would match.
func firstMatch(rules tunnel.IngressRecords, host, path string) string {
	for _, r := range rules {
		host_match := r.Hostname == "" || r.Hostname == host ||
			(strings.HasPrefix(r.Hostname, "*.") && strings.HasSuffix(host, r.Hostname[1:]))
		if !host_match {
			continue
		}
		if r.Path == "" || regexp.MustCompile(r.Path).MatchString(path) {
			return r.Service
		}
	}
	return ""
}

func hasWarning(result *ingressResult, reason, substring string) bool {
	return slices.ContainsFunc(result.Warnings, func(w warning) bool {
		return w.Reason == reason && strings.Contains(w.Message, substring)
	})
}

func TestTranslatePath(t *testing.T) {
	tests := []struct {
		name     string
		pathType networkingv1.PathType
		path     string
		hostless bool
		want     string
		exact    bool
		length   int
		wantErr  bool
	}{
		{"prefix", networkingv1.PathTypePrefix, "/static", false, `^/static(/.*)?$`, false, 7, false},
		{"prefix trailing slash", networkingv1.PathTypePrefix, "/static/", false, `^/static(/.*)?$`, false, 7, false},
		{"prefix quotes metacharacters", networkingv1.PathTypePrefix, "/v1.0", false, `^/v1\.0(/.*)?$`, false, 5, false},
		{"prefix root", networkingv1.PathTypePrefix, "/", false, "", false, 0, false},
		{"prefix root without host", networkingv1.PathTypePrefix, "/", true, "^/", false, 0, false},
		{"exact", networkingv1.PathTypeExact, "/healthz", false, `^/healthz$`, true, 8, false},
		{"implementation specific regex", networkingv1.PathTypeImplementationSpecific, "^/api/v[12]/", false, "^/api/v[12]/", false, 12, false},
		{"implementation specific root", networkingv1.PathTypeImplementationSpecific, "/", false, "", false, 0, false},
		{"implementation specific root without host", networkingv1.PathTypeImplementationSpecific, "/", true, "^/", false, 0, false},
		{"implementation specific anchored root", networkingv1.PathTypeImplementationSpecific, "^/", false, "", false, 0, false},
		{"implementation specific anchored root without host", networkingv1.PathTypeImplementationSpecific, "^/", true, "^/", false, 0, false},
		{"implementation specific empty", networkingv1.PathTypeImplementationSpecific, "", false, "", false, 0, false},
		{"implementation specific empty without host", networkingv1.PathTypeImplementationSpecific, "", true, "^/", false, 0, false},
		{"invalid regex", networkingv1.PathTypeImplementationSpecific, "(", false, "", false, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, exact, length, err := translatePath(tt.pathType, tt.path, tt.hostless)
			if (err != nil) != tt.wantErr {
				t.Fatalf("translatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want || exact != tt.exact || length != tt.length {
				t.Errorf("translatePath() = (%q, %v, %d), want (%q, %v, %d)", got, exact, length, tt.want, tt.exact, tt.length)
			}
		})
	}
}

func TestTranslatePath_MatchesKubernetesSemantics(t *testing.T) {
	tests := []struct {
		pathType networkingv1.PathType
		path     string
		matches  []string
		misses   []string
	}{
		{networkingv1.PathTypePrefix, "/static", []string{"/static", "/static/", "/static/x/y"}, []string{"/staticfoo", "/x/static", "/"}},
		{networkingv1.PathTypePrefix, "/static/", []string{"/static", "/static/x"}, []string{"/staticfoo"}},
		{networkingv1.PathTypeExact, "/healthz", []string{"/healthz"}, []string{"/healthz/", "/healthzx", "/x/healthz"}},
	}
	for _, tt := range tests {
		translated, _, _, err := translatePath(tt.pathType, tt.path, false)
		if err != nil {
			t.Fatalf("translatePath(%s %q): %v", tt.pathType, tt.path, err)
		}
		re := regexp.MustCompile(translated)
		for _, p := range tt.matches {
			if !re.MatchString(p) {
				t.Errorf("%s %q should match %q (regex %q)", tt.pathType, tt.path, p, translated)
			}
		}
		for _, p := range tt.misses {
			if re.MatchString(p) {
				t.Errorf("%s %q should not match %q (regex %q)", tt.pathType, tt.path, p, translated)
			}
		}
	}
}

func TestRender_OrdersRulesForFirstMatch(t *testing.T) {
	ingresses := []networkingv1.Ingress{
		testIngress("old", 0,
			testRule{host: "a.example.com", paths: []testPath{prefixPath("/", "root")}},
			testRule{host: "*.example.com", paths: []testPath{prefixPath("/", "wild")}},
			testRule{host: "", paths: []testPath{prefixPath("/", "fallback")}},
		),
		testIngress("young", 5,
			testRule{host: "a.example.com", paths: []testPath{
				prefixPath("/api", "api"),
				exactPath("/api/health", "health"),
			}},
			testRule{host: "*.b.example.com", paths: []testPath{prefixPath("/", "deep-wild")}},
		),
	}

	out := render(ingresses, KubernetesApiTunnelConfig{}, noServices)

	tests := []struct{ host, path, want string }{
		{"a.example.com", "/api/health", svc("health")},
		{"a.example.com", "/api/health/x", svc("api")},
		{"a.example.com", "/api", svc("api")},
		{"a.example.com", "/apix", svc("root")},
		{"a.example.com", "/", svc("root")},
		{"c.example.com", "/x", svc("wild")},
		{"x.b.example.com", "/x", svc("deep-wild")},
		{"other.net", "/x", svc("fallback")},
	}
	for _, tt := range tests {
		if got := firstMatch(out.Rules, tt.host, tt.path); got != tt.want {
			t.Errorf("request %s%s routed to %q, want %q", tt.host, tt.path, got, tt.want)
		}
	}
}

func TestRender_OldestIngressWinsConflict(t *testing.T) {
	ingresses := []networkingv1.Ingress{
		testIngress("second", 5, testRule{host: "a.example.com", paths: []testPath{
			prefixPath("/", "second"),
			prefixPath("/only-second", "second-only"),
		}}),
		testIngress("first", 0, testRule{host: "a.example.com", paths: []testPath{prefixPath("/", "first")}}),
	}

	out := render(ingresses, KubernetesApiTunnelConfig{}, noServices)

	if got := firstMatch(out.Rules, "a.example.com", "/"); got != svc("first") {
		t.Errorf("a.example.com/ routed to %q, want the oldest Ingress", got)
	}
	if got := firstMatch(out.Rules, "a.example.com", "/only-second"); got != svc("second-only") {
		t.Errorf("the losing Ingress's other rule should still publish, got %q", got)
	}
	if !hasWarning(out.Results["uid-second"], ReasonRuleConflict, "ns/first") {
		t.Errorf("expected RuleConflict warning naming ns/first, got %+v", out.Results["uid-second"].Warnings)
	}
	if len(out.Results["uid-first"].Warnings) != 0 {
		t.Errorf("winner should have no warnings, got %+v", out.Results["uid-first"].Warnings)
	}
}

func TestRender_ImplementationSpecificRootConflictsWithPrefixRoot(t *testing.T) {
	ingresses := []networkingv1.Ingress{
		testIngress("young", 5, testRule{host: "a.example.com", paths: []testPath{regexPath("/", "young")}}),
		testIngress("old", 0, testRule{host: "a.example.com", paths: []testPath{prefixPath("/", "old")}}),
	}

	out := render(ingresses, KubernetesApiTunnelConfig{}, noServices)

	for _, path := range []string{"/", "/x"} {
		if got := firstMatch(out.Rules, "a.example.com", path); got != svc("old") {
			t.Errorf("a.example.com%s routed to %q, want the oldest Ingress", path, got)
		}
	}
	if !hasWarning(out.Results["uid-young"], ReasonRuleConflict, "ns/old") {
		t.Errorf("expected RuleConflict warning naming ns/old, got %+v", out.Results["uid-young"].Warnings)
	}
}

func TestRender_KubernetesApiHostIsReserved(t *testing.T) {
	kubeAPI := KubernetesApiTunnelConfig{Enabled: true, Server: "kubernetes.default.svc:443", Domain: "k.example.com", CloudflareAccessAppName: "Kubernetes API"}
	ingress := testIngress("app", 0,
		testRule{host: "k.example.com", paths: []testPath{prefixPath("/foo", "shadow")}},
		testRule{host: "a.example.com", paths: []testPath{prefixPath("/", "a")}},
	)

	out := render([]networkingv1.Ingress{ingress}, kubeAPI, noServices)

	var host_rules tunnel.IngressRecords
	for _, r := range out.Rules {
		if r.Hostname == "k.example.com" {
			host_rules = append(host_rules, r)
		}
	}
	if len(host_rules) != 1 || host_rules[0].OriginRequest.ProxyType != "socks" {
		t.Errorf("expected only the socks rule for k.example.com, got %+v", host_rules)
	}
	if !hasWarning(out.Results["uid-app"], ReasonRuleConflict, "the Kubernetes API tunnel") {
		t.Errorf("expected RuleConflict naming the Kubernetes API tunnel, got %+v", out.Results["uid-app"].Warnings)
	}
	if got := out.Results["uid-app"].Hostnames; !slices.Equal(got, []string{"a.example.com"}) {
		t.Errorf("expected published hostnames [a.example.com], got %v", got)
	}
}

func TestRender_SkipsUnpublishableParts(t *testing.T) {
	ingress := testIngress("app", 0,
		testRule{host: "good.example.com", paths: []testPath{prefixPath("/", "good")}},
		testRule{host: "bad.example.com", paths: []testPath{
			regexPath("(", "bad-regex"),
			{path: "/named", pathType: networkingv1.PathTypePrefix, service: "named", portName: "http"},
		}},
	)
	ingress.Spec.Rules = append(ingress.Spec.Rules,
		networkingv1.IngressRule{Host: "bad.example.com", HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
			{Path: "/resource", PathType: new(networkingv1.PathTypePrefix), Backend: networkingv1.IngressBackend{
				Resource: &testResourceBackend,
			}},
			{Path: "/no-type", Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{Name: "no-type", Port: networkingv1.ServiceBackendPort{Number: 80}},
			}},
		}}},
	)
	ingress.Spec.DefaultBackend = &networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{Name: "default", Port: networkingv1.ServiceBackendPort{Number: 80}},
	}

	out := render([]networkingv1.Ingress{ingress}, KubernetesApiTunnelConfig{}, noServices)

	result := out.Results["uid-app"]
	for _, want := range []struct{ reason, substring string }{
		{ReasonRuleSkipped, "invalid path regex"},
		{ReasonRuleSkipped, "no services"},
		{ReasonRuleSkipped, "only service backends"},
		{ReasonRuleSkipped, "pathType is required"},
		{ReasonUnsupported, "defaultBackend"},
	} {
		if !hasWarning(result, want.reason, want.substring) {
			t.Errorf("expected %s warning containing %q, got %+v", want.reason, want.substring, result.Warnings)
		}
	}
	if len(out.Rules) != 1 || out.Rules[0].Service != svc("good") {
		t.Errorf("expected only the good rule, got %+v", out.Rules)
	}
	if !slices.Equal(result.Hostnames, []string{"good.example.com"}) {
		t.Errorf("expected published hostnames [good.example.com], got %v", result.Hostnames)
	}
}

func TestRender_ResolvesNamedPort(t *testing.T) {
	ingress := testIngress("app", 0, testRule{host: "a.example.com", paths: []testPath{
		{path: "/", pathType: networkingv1.PathTypePrefix, service: "web", portName: "http"},
	}})
	lookup := func(namespace, service, port string) (int32, error) {
		if namespace == "ns" && service == "web" && port == "http" {
			return 8080, nil
		}
		return 0, errors.New("unexpected lookup")
	}

	out := render([]networkingv1.Ingress{ingress}, KubernetesApiTunnelConfig{}, lookup)

	if len(out.Rules) != 1 || out.Rules[0].Service != "http://web.ns:8080" {
		t.Errorf("expected service http://web.ns:8080, got %+v", out.Rules)
	}
}

func TestRender_KubernetesApiRuleOutranksIngress(t *testing.T) {
	kubeAPI := KubernetesApiTunnelConfig{Enabled: true, Server: "kubernetes.default.svc:443", Domain: "k.example.com", CloudflareAccessAppName: "Kubernetes API"}
	ingress := testIngress("app", 0, testRule{host: "k.example.com", paths: []testPath{prefixPath("/", "shadow")}})

	out := render([]networkingv1.Ingress{ingress}, kubeAPI, noServices)

	if len(out.Rules) != 1 {
		t.Fatalf("expected only the Kubernetes API rule, got %+v", out.Rules)
	}
	rule := out.Rules[0]
	if rule.Hostname != "k.example.com" || rule.Path != "" || rule.Service != "tcp://kubernetes.default.svc:443" || rule.OriginRequest.ProxyType != "socks" {
		t.Errorf("unexpected Kubernetes API rule: %+v", rule)
	}
	if !hasWarning(out.Results["uid-app"], ReasonRuleConflict, "Kubernetes API tunnel") {
		t.Errorf("expected RuleConflict naming the Kubernetes API tunnel, got %+v", out.Results["uid-app"].Warnings)
	}
	if out.AccessAppRequests["k.example.com"] != "Kubernetes API" {
		t.Errorf("expected Access request for k.example.com, got %v", out.AccessAppRequests)
	}
}

func TestRender_AccessRequestsOnlyForPublishedHostnames(t *testing.T) {
	ingress := testIngress("app", 0,
		testRule{host: "a.example.com", paths: []testPath{prefixPath("/", "a")}},
		testRule{host: "b.example.com", paths: []testPath{regexPath("(", "b")}},
	)
	ingress.Annotations = map[string]string{AnnotationAccessAppName: "My App"}

	out := render([]networkingv1.Ingress{ingress}, KubernetesApiTunnelConfig{}, noServices)

	want := map[string]string{"a.example.com": "My App"}
	if len(out.AccessAppRequests) != 1 || out.AccessAppRequests["a.example.com"] != want["a.example.com"] {
		t.Errorf("expected Access requests %v, got %v", want, out.AccessAppRequests)
	}
}

func TestRender_AppliesSchemeAndOriginSettings(t *testing.T) {
	ingress := testIngress("app", 0, testRule{host: "a.example.com", paths: []testPath{prefixPath("/", "web")}})
	ingress.Annotations = map[string]string{
		AnnotationBackendProtocol:   "HTTPS",
		AnnotationOriginNoTlsVerify: "true",
	}

	out := render([]networkingv1.Ingress{ingress}, KubernetesApiTunnelConfig{}, noServices)

	if len(out.Rules) != 1 || out.Rules[0].Service != "https://web.ns:80" || !out.Rules[0].OriginRequest.NoTLSVerify {
		t.Errorf("expected https service with noTLSVerify, got %+v", out.Rules)
	}
}

func TestRender_WarnsOnUnknownBackendProtocol(t *testing.T) {
	ingress := testIngress("app", 0, testRule{host: "a.example.com", paths: []testPath{prefixPath("/", "web")}})
	ingress.Annotations = map[string]string{AnnotationBackendProtocol: "ftp"}

	out := render([]networkingv1.Ingress{ingress}, KubernetesApiTunnelConfig{}, noServices)

	if out.Rules[0].Service != svc("web") {
		t.Errorf("expected http fallback, got %q", out.Rules[0].Service)
	}
	if !hasWarning(out.Results["uid-app"], ReasonInvalidAnnotation, "ftp") {
		t.Errorf("expected InvalidAnnotation warning, got %+v", out.Results["uid-app"].Warnings)
	}
}

func TestRender_HostnamesAreUniqueAndSorted(t *testing.T) {
	ingress := testIngress("app", 0,
		testRule{host: "z.example.com", paths: []testPath{prefixPath("/", "z")}},
		testRule{host: "a.example.com", paths: []testPath{prefixPath("/x", "a1"), prefixPath("/y", "a2")}},
	)

	out := render([]networkingv1.Ingress{ingress}, KubernetesApiTunnelConfig{}, noServices)

	if got := out.Results["uid-app"].Hostnames; !slices.Equal(got, []string{"a.example.com", "z.example.com"}) {
		t.Errorf("expected [a.example.com z.example.com], got %v", got)
	}
}

var testResourceBackend = corev1.TypedLocalObjectReference{Kind: "StorageBucket", Name: "bucket"}
