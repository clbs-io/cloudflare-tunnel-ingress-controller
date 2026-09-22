package controller

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/cftest"
	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testTokenSecret = "test-token-secret"
	testCloudflared = "test-cloudflared"
	// testToken is the tunnel token the cftest fake returns.
	testToken = "tunnel-token"
)

func chartCloudflared(available int32, annotations map[string]string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		Name:      testCloudflared,
		Namespace: Namespace(),
		Labels:    map[string]string{managedByLabel: "Helm"},
	}
	deployment.Spec.Template.Annotations = annotations
	deployment.Status.AvailableReplicas = available
	return deployment
}

func legacyCloudflared(managed_by string) *appsv1.Deployment {
	return &appsv1.Deployment{
		Name:      legacyCloudflaredDeployment,
		Namespace: Namespace(),
		Labels:    map[string]string{managedByLabel: managed_by},
	}
}

func tokenSecret(token string) *corev1.Secret {
	return &corev1.Secret{
		Name:      testTokenSecret,
		Namespace: Namespace(),
		Data:      map[string][]byte{tunnelTokenKey: []byte(token)},
	}
}

func (f *fixture) secret(t *testing.T) (*corev1.Secret, error) {
	t.Helper()
	secret := &corev1.Secret{}
	err := f.k8s.Get(t.Context(), client.ObjectKey{Namespace: Namespace(), Name: testTokenSecret}, secret)
	return secret, err
}

func (f *fixture) deployment(t *testing.T, name string) (*appsv1.Deployment, error) {
	t.Helper()
	deployment := &appsv1.Deployment{}
	err := f.k8s.Get(t.Context(), client.ObjectKey{Namespace: Namespace(), Name: name}, deployment)
	return deployment, err
}

func TestEnsureCloudflared_CreatesTokenSecret(t *testing.T) {
	f := newFixture(t)

	if _, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok"); err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	secret, err := f.secret(t)
	if err != nil {
		t.Fatalf("expected the token Secret to exist: %v", err)
	}
	if string(secret.Data[tunnelTokenKey]) != "tok" {
		t.Errorf("expected token %q, got %q", "tok", secret.Data[tunnelTokenKey])
	}
	if secret.Labels[managedByLabel] != managedByValue {
		t.Errorf("expected label %s=%s, got %v", managedByLabel, managedByValue, secret.Labels)
	}
}

func TestEnsureCloudflared_UpdatesTokenSecretOnChange(t *testing.T) {
	f := newFixture(t, tokenSecret("old"))

	if _, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "new"); err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	secret, _ := f.secret(t)
	if string(secret.Data[tunnelTokenKey]) != "new" {
		t.Errorf("expected token %q, got %q", "new", secret.Data[tunnelTokenKey])
	}
}

func TestEnsureCloudflared_NoSecretWriteWhenEqual(t *testing.T) {
	f := newFixture(t, tokenSecret("tok"))
	before, _ := f.secret(t)

	if _, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok"); err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	after, _ := f.secret(t)
	if after.ResourceVersion != before.ResourceVersion {
		t.Errorf("expected no Secret write, resourceVersion %s -> %s", before.ResourceVersion, after.ResourceVersion)
	}
}

func TestEnsureCloudflared_AnnotatesPodTemplateWithTokenHash(t *testing.T) {
	f := newFixture(t, chartCloudflared(1, nil))

	for range 2 {
		if _, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok"); err != nil {
			t.Fatalf("ensureCloudflared: %v", err)
		}
	}
	first, _ := f.deployment(t, testCloudflared)
	if got := first.Spec.Template.Annotations[tokenHashAnnotation]; got != tokenHash("tok") {
		t.Fatalf("expected annotation %q, got %q", tokenHash("tok"), got)
	}

	if _, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok"); err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}
	second, _ := f.deployment(t, testCloudflared)
	if second.ResourceVersion != first.ResourceVersion {
		t.Errorf("expected no Deployment write when the hash matches, resourceVersion %s -> %s", first.ResourceVersion, second.ResourceVersion)
	}
}

func TestEnsureCloudflared_RollsOutOnTokenChange(t *testing.T) {
	f := newFixture(t, chartCloudflared(1, map[string]string{tokenHashAnnotation: tokenHash("old")}), tokenSecret("old"))

	if _, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "new"); err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	deployment, _ := f.deployment(t, testCloudflared)
	if got := deployment.Spec.Template.Annotations[tokenHashAnnotation]; got != tokenHash("new") {
		t.Errorf("expected annotation %q, got %q", tokenHash("new"), got)
	}
}

func TestEnsureCloudflared_MissingChartDeploymentSkipsMigration(t *testing.T) {
	f := newFixture(t, legacyCloudflared(managedByValue))

	pending, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok")
	if err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	if pending {
		t.Error("expected no pending migration without the chart Deployment")
	}
	if _, err := f.deployment(t, legacyCloudflaredDeployment); err != nil {
		t.Errorf("expected the legacy Deployment to stay: %v", err)
	}
}

func TestEnsureCloudflared_DeletesLegacyWhenChartAvailable(t *testing.T) {
	f := newFixture(t, chartCloudflared(1, nil), legacyCloudflared(managedByValue))

	pending, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok")
	if err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	if pending {
		t.Error("expected the migration to be complete")
	}
	if _, err := f.deployment(t, legacyCloudflaredDeployment); !apierrors.IsNotFound(err) {
		t.Errorf("expected the legacy Deployment to be deleted, got %v", err)
	}
}

func TestEnsureCloudflared_KeepsLegacyUntilChartAvailable(t *testing.T) {
	f := newFixture(t, chartCloudflared(0, nil), legacyCloudflared(managedByValue))

	pending, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok")
	if err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	if !pending {
		t.Error("expected the migration to be pending")
	}
	if _, err := f.deployment(t, legacyCloudflaredDeployment); err != nil {
		t.Errorf("expected the legacy Deployment to stay: %v", err)
	}
}

func TestEnsureCloudflared_KeepsLegacyNamedDeploymentOfAnotherOwner(t *testing.T) {
	f := newFixture(t, chartCloudflared(1, nil), legacyCloudflared("Helm"))

	pending, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok")
	if err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	if pending {
		t.Error("expected no pending migration for a Deployment we do not manage")
	}
	if _, err := f.deployment(t, legacyCloudflaredDeployment); err != nil {
		t.Errorf("expected the Deployment of another owner to stay: %v", err)
	}
}

func TestEnsureCloudflared_NeverDeletesChartDeploymentNamedLikeLegacy(t *testing.T) {
	deployment := legacyCloudflared(managedByValue)
	deployment.Status.AvailableReplicas = 1
	f := newFixture(t, deployment)
	f.controller.cloudflaredDeployment = legacyCloudflaredDeployment

	pending, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok")
	if err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}

	if pending {
		t.Error("expected no pending migration when the chart Deployment is named like the legacy one")
	}
	if _, err := f.deployment(t, legacyCloudflaredDeployment); err != nil {
		t.Errorf("expected the chart Deployment to stay: %v", err)
	}
}

func TestReconcile_RequeuesSoonWhileLegacyPending(t *testing.T) {
	f := newFixture(t, chartCloudflared(0, nil), legacyCloudflared(managedByValue))

	result := f.mustReconcile(t)

	if result.RequeueAfter != time.Minute {
		t.Errorf("expected requeue after 1m while the legacy Deployment is pending, got %v", result.RequeueAfter)
	}
}

// newFixtureReaders builds a fixture like newFixture, but wraps the
// controller's client and/or reader with the given interceptor Get behavior
// when non-nil, so a test can prove which one the controller reads Secrets
// and Deployments through. fixture.k8s always stays the plain, unwrapped
// fake, so assertions see the true cluster state regardless of what the
// controller was given to read from.
func newFixtureReaders(t *testing.T, client_funcs, reader_funcs *interceptor.Funcs, objects ...client.Object) *fixture {
	t.Helper()
	cloudflare := cftest.New(t)
	k8s := fake.NewClientBuilder().
		WithScheme(clientgoscheme.Scheme).
		WithStatusSubresource(&networkingv1.Ingress{}).
		WithObjects(testIngressClass(testClass, testControllerClass)).
		WithObjects(objects...).
		Build()

	var api_client client.Client = k8s
	if client_funcs != nil {
		api_client = interceptor.NewClient(k8s, *client_funcs)
	}
	var reader client.Reader = k8s
	if reader_funcs != nil {
		reader = interceptor.NewClient(k8s, *reader_funcs)
	}

	recorder := events.NewFakeRecorder(100)
	return &fixture{
		controller: &IngressController{
			logger:                logr.Discard(),
			client:                api_client,
			reader:                reader,
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

// blockSecretAndDeploymentGets errors on a Get of a Secret or a Deployment
// and delegates every other Get, so a test can prove the controller never
// reads those two kinds through this client.
func blockSecretAndDeploymentGets(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	switch obj.(type) {
	case *corev1.Secret, *appsv1.Deployment:
		return errors.New("reads must go through the API reader, not the client")
	default:
		return c.Get(ctx, key, obj, opts...)
	}
}

func TestEnsureCloudflared_ReadsThroughTheAPIReader(t *testing.T) {
	client_funcs := &interceptor.Funcs{Get: blockSecretAndDeploymentGets}
	f := newFixtureReaders(t, client_funcs, nil, chartCloudflared(1, nil), legacyCloudflared(managedByValue))

	pending, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok")
	if err != nil {
		t.Fatalf("ensureCloudflared: %v", err)
	}
	if pending {
		t.Error("expected the migration to be complete")
	}
	if _, err := f.secret(t); err != nil {
		t.Errorf("expected the token Secret to be created: %v", err)
	}
	if _, err := f.deployment(t, legacyCloudflaredDeployment); !apierrors.IsNotFound(err) {
		t.Errorf("expected the legacy Deployment to be deleted, got %v", err)
	}
}

func TestEnsureCloudflared_ForbiddenReadDeletesNothing(t *testing.T) {
	reader_funcs := &interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*appsv1.Deployment); ok {
				return apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, key.Name, errors.New("rbac"))
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}
	f := newFixtureReaders(t, nil, reader_funcs, chartCloudflared(1, nil), legacyCloudflared(managedByValue))

	if _, err := f.controller.ensureCloudflared(t.Context(), logr.Discard(), "tok"); err == nil {
		t.Fatal("expected an error when the API reader forbids reading Deployments")
	}

	if _, err := f.deployment(t, legacyCloudflaredDeployment); err != nil {
		t.Errorf("expected the legacy Deployment to still exist: %v", err)
	}
}

func TestReconcile_TokenNeverInDeploymentSpec(t *testing.T) {
	f := newFixture(t, chartCloudflared(1, nil), tunnelIngress("app", "app.example.com"))

	f.mustReconcile(t)

	deployments := &appsv1.DeploymentList{}
	if err := f.k8s.List(t.Context(), deployments); err != nil {
		t.Fatalf("list: %v", err)
	}
	raw, err := json.Marshal(deployments)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), testToken) {
		t.Errorf("expected no Deployment to contain the tunnel token")
	}
}
