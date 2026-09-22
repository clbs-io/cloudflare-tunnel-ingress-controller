package controller

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/cftest"
	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testClass           = "cloudflare-tunnel"
	testControllerClass = "clbs.io/cloudflare-tunnel-ingress-controller"
)

type fixture struct {
	controller *IngressController
	k8s        client.Client
	cloudflare *cftest.Server
	recorder   *events.FakeRecorder
}

func newFixture(t *testing.T, objects ...client.Object) *fixture {
	t.Helper()
	cloudflare := cftest.New(t)
	k8s := fake.NewClientBuilder().
		WithScheme(clientgoscheme.Scheme).
		WithStatusSubresource(&networkingv1.Ingress{}).
		WithObjects(testIngressClass(testClass, testControllerClass)).
		WithObjects(objects...).
		Build()
	recorder := events.NewFakeRecorder(100)
	return &fixture{
		controller: &IngressController{
			logger:                logr.Discard(),
			client:                k8s,
			reader:                k8s,
			recorder:              recorder,
			tunnelClient:          tunnel.NewClient(cloudflare.Client(), cftest.AccountID, cftest.TunnelName, logr.Discard()),
			ingressClassName:      testClass,
			controllerClassName:   testControllerClass,
			resyncPeriod:          10 * time.Minute,
			tunnelTokenSecret:     testTokenSecret,
			cloudflaredDeployment: testCloudflared,
		},
		k8s:        k8s,
		cloudflare: cloudflare,
		recorder:   recorder,
	}
}

func testIngressClass(name, controller string) *networkingv1.IngressClass {
	return &networkingv1.IngressClass{
		Name: name,
		Spec: networkingv1.IngressClassSpec{Controller: controller},
	}
}

func tunnelIngress(name, host string) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		UID:               types.UID("uid-" + name),
		Name:              name,
		Namespace:         "ns",
		CreationTimestamp: metav1.NewTime(testCreated),
		Spec: networkingv1.IngressSpec{
			IngressClassName: new(testClass),
			Rules: []networkingv1.IngressRule{{
				Host: host,
				HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
					Path:     "/",
					PathType: new(networkingv1.PathTypePrefix),
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
						Name: name, Port: networkingv1.ServiceBackendPort{Number: 80},
					}},
				}}},
			}},
		},
	}
}

func (f *fixture) reconcile(t *testing.T) (ctrl.Result, error) {
	t.Helper()
	return f.controller.Reconcile(t.Context(), ctrl.Request{})
}

func (f *fixture) mustReconcile(t *testing.T) ctrl.Result {
	t.Helper()
	result, err := f.reconcile(t)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return result
}

func (f *fixture) get(t *testing.T, name string) (*networkingv1.Ingress, error) {
	t.Helper()
	ingress := &networkingv1.Ingress{}
	err := f.k8s.Get(t.Context(), client.ObjectKey{Namespace: "ns", Name: name}, ingress)
	return ingress, err
}

func (f *fixture) publishedHosts(t *testing.T) []string {
	t.Helper()
	if len(f.cloudflare.ConfigPuts) == 0 {
		return nil
	}
	var rules []struct{ Hostname string }
	if err := json.Unmarshal(f.cloudflare.ConfigPuts[len(f.cloudflare.ConfigPuts)-1], &rules); err != nil {
		t.Fatalf("decode rules: %v", err)
	}
	hosts := make([]string, 0, len(rules))
	for _, r := range rules {
		if len(r.Hostname) > 0 {
			hosts = append(hosts, r.Hostname)
		}
	}
	return hosts
}

func (f *fixture) events() []string {
	var out []string
	for {
		select {
		case e := <-f.recorder.Events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func statusHosts(ingress *networkingv1.Ingress) []string {
	hosts := make([]string, 0, len(ingress.Status.LoadBalancer.Ingress))
	for _, lb := range ingress.Status.LoadBalancer.Ingress {
		hosts = append(hosts, lb.Hostname)
	}
	return hosts
}

func TestReconcile_PublishesNewIngress(t *testing.T) {
	f := newFixture(t, tunnelIngress("app", "app.example.com"))

	result := f.mustReconcile(t)

	if result.RequeueAfter != 10*time.Minute {
		t.Errorf("expected requeue after 10m, got %v", result.RequeueAfter)
	}
	ingress, err := f.get(t, "app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !slices.Contains(ingress.Finalizers, ingressTunnelFinalizer) {
		t.Errorf("expected finalizer, got %v", ingress.Finalizers)
	}
	if got := statusHosts(ingress); !slices.Equal(got, []string{"app.example.com"}) {
		t.Errorf("expected status [app.example.com], got %v", got)
	}
	if got := f.publishedHosts(t); !slices.Equal(got, []string{"app.example.com"}) {
		t.Errorf("expected published [app.example.com], got %v", got)
	}
	if got := f.cloudflare.DNSRecordNames(); !slices.Equal(got, []string{"app.example.com"}) {
		t.Errorf("expected DNS [app.example.com], got %v", got)
	}
}

func TestReconcile_ReleasesIngressSwitchedToAnotherClass(t *testing.T) {
	tests := []struct {
		name    string
		objects []client.Object
	}{
		{"class without IngressClass", nil},
		{"class of another controller", []client.Object{testIngressClass("other", "k8s.io/ingress-nginx")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, append(tt.objects, tunnelIngress("app", "app.example.com"))...)
			f.mustReconcile(t)

			ingress, _ := f.get(t, "app")
			ingress.Spec.IngressClassName = new("other")
			if err := f.k8s.Update(t.Context(), ingress); err != nil {
				t.Fatalf("update: %v", err)
			}
			f.mustReconcile(t)

			ingress, _ = f.get(t, "app")
			if slices.Contains(ingress.Finalizers, ingressTunnelFinalizer) {
				t.Errorf("expected finalizer to be released, got %v", ingress.Finalizers)
			}
			if got := f.publishedHosts(t); len(got) != 0 {
				t.Errorf("expected no published hosts, got %v", got)
			}
			if got := f.cloudflare.DNSRecordNames(); len(got) != 0 {
				t.Errorf("expected no DNS records, got %v", got)
			}
		})
	}
}

func TestReconcile_KeepsFinalizerOfAnotherInstallation(t *testing.T) {
	ingress := tunnelIngress("app", "app.example.com")
	ingress.Spec.IngressClassName = new("second")
	ingress.Finalizers = []string{ingressTunnelFinalizer}
	f := newFixture(t, testIngressClass("second", testControllerClass), ingress)
	before, err := f.get(t, "app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	f.mustReconcile(t)
	f.mustReconcile(t)

	after, err := f.get(t, "app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !slices.Contains(after.Finalizers, ingressTunnelFinalizer) {
		t.Errorf("expected the finalizer of the other installation to be kept, got %v", after.Finalizers)
	}
	if after.ResourceVersion != before.ResourceVersion {
		t.Errorf("expected no Ingress writes, resourceVersion %s -> %s", before.ResourceVersion, after.ResourceVersion)
	}
	if got := f.publishedHosts(t); len(got) != 0 {
		t.Errorf("expected no published hosts, got %v", got)
	}
}

func TestReconcile_IngressClassErrorAbortsBeforeSync(t *testing.T) {
	ingress := tunnelIngress("app", "app.example.com")
	ingress.Spec.IngressClassName = new("second")
	ingress.Finalizers = []string{ingressTunnelFinalizer}
	f := newFixture(t, tunnelIngress("mine", "mine.example.com"), ingress)
	f.controller.client = interceptor.NewClient(f.k8s.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*networkingv1.IngressClass); ok {
				return errors.New("cache unavailable")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	if _, err := f.reconcile(t); err == nil {
		t.Fatal("expected an error when the IngressClass lookup fails")
	}

	if len(f.cloudflare.ConfigPuts) != 0 {
		t.Errorf("expected no configuration update, got %d", len(f.cloudflare.ConfigPuts))
	}
	got, _ := f.get(t, "app")
	if !slices.Contains(got.Finalizers, ingressTunnelFinalizer) {
		t.Errorf("expected the finalizer to be kept, got %v", got.Finalizers)
	}
}

func TestReconcile_RemovesDeletedIngress(t *testing.T) {
	f := newFixture(t, tunnelIngress("app", "app.example.com"), tunnelIngress("other", "other.example.com"))
	f.mustReconcile(t)

	ingress, _ := f.get(t, "app")
	if err := f.k8s.Delete(t.Context(), ingress); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.mustReconcile(t)

	if _, err := f.get(t, "app"); !apierrors.IsNotFound(err) {
		t.Errorf("expected the Ingress to be gone, got %v", err)
	}
	if got := f.publishedHosts(t); !slices.Equal(got, []string{"other.example.com"}) {
		t.Errorf("expected published [other.example.com], got %v", got)
	}
	if got := f.cloudflare.DNSRecordNames(); !slices.Equal(got, []string{"other.example.com"}) {
		t.Errorf("expected DNS [other.example.com], got %v", got)
	}
}

func TestReconcile_KeepsFinalizerWhenCloudflareUpdateFails(t *testing.T) {
	f := newFixture(t, tunnelIngress("app", "app.example.com"))
	f.mustReconcile(t)

	ingress, _ := f.get(t, "app")
	if err := f.k8s.Delete(t.Context(), ingress); err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.cloudflare.FailConfigPut = true
	if _, err := f.reconcile(t); err == nil {
		t.Fatal("expected an error when the configuration update fails")
	}

	ingress, err := f.get(t, "app")
	if err != nil {
		t.Fatalf("expected the Ingress to still exist: %v", err)
	}
	if !slices.Contains(ingress.Finalizers, ingressTunnelFinalizer) {
		t.Errorf("expected finalizer to be kept, got %v", ingress.Finalizers)
	}
}

func TestReconcile_InvalidIngressDoesNotBlockOthers(t *testing.T) {
	bad := tunnelIngress("bad", "bad.example.com")
	bad.Spec.Rules[0].HTTP.Paths[0].PathType = new(networkingv1.PathTypeImplementationSpecific)
	bad.Spec.Rules[0].HTTP.Paths[0].Path = "("
	f := newFixture(t, tunnelIngress("good", "good.example.com"), bad)

	f.mustReconcile(t)

	if got := f.publishedHosts(t); !slices.Equal(got, []string{"good.example.com"}) {
		t.Errorf("expected published [good.example.com], got %v", got)
	}
	ingress, _ := f.get(t, "bad")
	if got := statusHosts(ingress); len(got) != 0 {
		t.Errorf("expected empty status for the invalid Ingress, got %v", got)
	}
	if got := f.events(); !slices.ContainsFunc(got, func(e string) bool { return strings.HasPrefix(e, "Warning RuleSkipped") }) {
		t.Errorf("expected a RuleSkipped warning event, got %v", got)
	}
}

func TestReconcile_ReleasesFinalizersWhenAccessFails(t *testing.T) {
	protected := tunnelIngress("protected", "protected.example.com")
	protected.Annotations = map[string]string{AnnotationAccessAppName: "Protected"}
	deleting := tunnelIngress("deleting", "deleting.example.com")
	deleting.Finalizers = []string{ingressTunnelFinalizer}
	deleting.DeletionTimestamp = new(metav1.NewTime(testCreated.Add(time.Hour)))
	f := newFixture(t, protected, deleting)
	f.cloudflare.FailAccessAppCreate = true

	if _, err := f.reconcile(t); err == nil {
		t.Fatal("expected an error when Access application creation fails")
	}

	if _, err := f.get(t, "deleting"); !apierrors.IsNotFound(err) {
		t.Errorf("expected the deleting Ingress to be released, got %v", err)
	}
	ingress, _ := f.get(t, "protected")
	if got := statusHosts(ingress); !slices.Equal(got, []string{"protected.example.com"}) {
		t.Errorf("expected status [protected.example.com], got %v", got)
	}
}

func TestReconcile_ReportsDnsConflict(t *testing.T) {
	f := newFixture(t, tunnelIngress("app", "taken.example.com"))
	f.cloudflare.AddDNSRecord("taken.example.com", "somewhere-else.example.net")

	f.mustReconcile(t)

	ingress, _ := f.get(t, "app")
	if got := statusHosts(ingress); len(got) != 0 {
		t.Errorf("expected the conflicting hostname to stay out of status, got %v", got)
	}
	if got := f.events(); !slices.ContainsFunc(got, func(e string) bool { return strings.HasPrefix(e, "Warning DNSConflict") }) {
		t.Errorf("expected a DNSConflict warning event, got %v", got)
	}
}

func TestReconcile_NoAccessApplicationForDnsConflict(t *testing.T) {
	protected := tunnelIngress("app", "taken.example.com")
	protected.Annotations = map[string]string{AnnotationAccessAppName: "Protected"}
	f := newFixture(t, protected)
	f.cloudflare.AddDNSRecord("taken.example.com", "somewhere-else.example.net")

	f.mustReconcile(t)

	if got := f.events(); !slices.ContainsFunc(got, func(e string) bool { return strings.HasPrefix(e, "Warning DNSConflict") }) {
		t.Errorf("expected a DNSConflict warning event, got %v", got)
	}
	if got := f.cloudflare.AccessApps(); len(got) != 0 {
		t.Errorf("expected no Access application for the conflicting hostname, got %+v", got)
	}
}

func TestReconcile_SecondRunMakesNoWrites(t *testing.T) {
	f := newFixture(t, tunnelIngress("app", "app.example.com"))
	f.mustReconcile(t)
	before, _ := f.get(t, "app")
	puts, creates := len(f.cloudflare.ConfigPuts), f.cloudflare.DNSCreates()

	f.mustReconcile(t)

	after, _ := f.get(t, "app")
	if len(f.cloudflare.ConfigPuts) != puts || f.cloudflare.DNSCreates() != creates {
		t.Errorf("expected no Cloudflare writes, got %d config updates and %d DNS creates more",
			len(f.cloudflare.ConfigPuts)-puts, f.cloudflare.DNSCreates()-creates)
	}
	if after.ResourceVersion != before.ResourceVersion {
		t.Errorf("expected no Ingress writes, resourceVersion %s -> %s", before.ResourceVersion, after.ResourceVersion)
	}
}

func TestReleaseIngress_IngressAlreadyGone(t *testing.T) {
	f := newFixture(t)
	gone := tunnelIngress("gone", "gone.example.com")
	gone.Finalizers = []string{ingressTunnelFinalizer}
	gone.ResourceVersion = "1"

	if err := f.controller.releaseIngress(t.Context(), logr.Discard(), gone); err != nil {
		t.Errorf("expected releasing a deleted Ingress to succeed, got %v", err)
	}
}
