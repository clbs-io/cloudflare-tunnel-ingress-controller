package tunnel

import (
	"reflect"
	"testing"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/cftest"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
)

func TestDeleteFromTunnelConfiguration_RemovesAllRulesOfIngress(t *testing.T) {
	fake := cftest.New(t)
	fake.AddDNSRecord("x.example.com", cftest.TunnelTarget)
	fake.AddDNSRecord("y.example.com", cftest.TunnelTarget)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u1": {record("x.example.com", "/a", "http://x.ns:80", originRequest{}), record("x.example.com", "/b", "http://x.ns:80", originRequest{})},
		"u2": {record("y.example.com", "/", "http://y.ns:80", originRequest{})},
	})
	if err := c.EnsureTunnelConfiguration(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("EnsureTunnelConfiguration: %v", err)
	}

	delete(config.Ingresses, "u1")
	if err := c.DeleteFromTunnelConfiguration(t.Context(), logr.Discard(), config, []string{"x.example.com"}); err != nil {
		t.Fatalf("DeleteFromTunnelConfiguration: %v", err)
	}

	want := [][2]string{{"y.example.com", "/"}, {"", ""}}
	if got := routes(lastPut(t, fake)); !reflect.DeepEqual(got, want) {
		t.Errorf("expected routes %v, got %v", want, got)
	}
	if got := fake.DNSRecordNames(); !reflect.DeepEqual(got, []string{"y.example.com"}) {
		t.Errorf("expected only y.example.com DNS record to remain, got %v", got)
	}
}

func TestDeleteFromTunnelConfiguration_KeepsDnsRecordStillInUse(t *testing.T) {
	fake := cftest.New(t)
	fake.AddDNSRecord("x.example.com", cftest.TunnelTarget)
	c := newTestClient(fake)
	config := newConfig(map[types.UID]IngressRecords{
		"u2": {record("x.example.com", "/other", "http://other.ns:80", originRequest{})},
	})

	if err := c.DeleteFromTunnelConfiguration(t.Context(), logr.Discard(), config, []string{"x.example.com"}); err != nil {
		t.Fatalf("DeleteFromTunnelConfiguration: %v", err)
	}

	if got := fake.DNSRecordNames(); !reflect.DeepEqual(got, []string{"x.example.com"}) {
		t.Errorf("expected x.example.com DNS record to remain for the other Ingress, got %v", got)
	}
}

func TestDeleteFromTunnelConfiguration_LeavesForeignDnsRecords(t *testing.T) {
	fake := cftest.New(t)
	fake.AddDNSRecord("x.example.com", "somewhere-else.example.net")
	c := newTestClient(fake)
	config := newConfig(nil)

	if err := c.DeleteFromTunnelConfiguration(t.Context(), logr.Discard(), config, []string{"x.example.com"}); err != nil {
		t.Fatalf("DeleteFromTunnelConfiguration: %v", err)
	}

	if got := fake.DNSRecordNames(); !reflect.DeepEqual(got, []string{"x.example.com"}) {
		t.Errorf("expected DNS record not pointing to the tunnel to remain, got %v", got)
	}
}
