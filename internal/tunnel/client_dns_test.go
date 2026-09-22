package tunnel

import (
	"reflect"
	"testing"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/cftest"
	"github.com/go-logr/logr"
)

func TestSync_CreatesOnlyMissingDnsRecords(t *testing.T) {
	fake := cftest.New(t)
	fake.AddDNSRecord("a.example.com", cftest.TunnelTarget)
	c := newTestClient(fake)
	config := newConfig(
		record("a.example.com", "", "http://a.ns:80", originRequest{}),
		record("b.example.com", "^/x", "http://b.ns:80", originRequest{}),
		record("b.example.com", "", "http://b.ns:80", originRequest{}),
	)

	if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if fake.DNSCreates() != 1 {
		t.Errorf("expected 1 DNS record created, got %d", fake.DNSCreates())
	}
	if got := fake.DNSRecordNames(); !reflect.DeepEqual(got, []string{"a.example.com", "b.example.com"}) {
		t.Errorf("expected records [a.example.com b.example.com], got %v", got)
	}
}

func TestSync_DeletesStaleRecordsInEveryZone(t *testing.T) {
	fake := cftest.New(t)
	fake.AddZone("other.net")
	fake.AddDNSRecord("keep.example.com", cftest.TunnelTarget)
	fake.AddDNSRecord("gone.example.com", cftest.TunnelTarget)
	fake.AddDNSRecord("gone.other.net", cftest.TunnelTarget)
	c := newTestClient(fake)
	config := newConfig(record("keep.example.com", "", "http://keep.ns:80", originRequest{}))

	if _, err := c.Sync(t.Context(), logr.Discard(), config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := fake.DNSRecordNames(); !reflect.DeepEqual(got, []string{"keep.example.com"}) {
		t.Errorf("expected only keep.example.com to remain, got %v", got)
	}
}

func TestSync_LeavesRecordsNotPointingToTunnel(t *testing.T) {
	fake := cftest.New(t)
	fake.AddDNSRecord("foreign.example.com", "somewhere-else.example.net")
	c := newTestClient(fake)

	if _, err := c.Sync(t.Context(), logr.Discard(), newConfig()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := fake.DNSRecordNames(); !reflect.DeepEqual(got, []string{"foreign.example.com"}) {
		t.Errorf("expected the foreign record to remain, got %v", got)
	}
}

func TestSync_SkipsZoneWithoutDnsPermission(t *testing.T) {
	fake := cftest.New(t)
	other_zone_id := fake.AddZone("other.net")
	fake.DenyDNS(other_zone_id)
	c := newTestClient(fake)
	config := newConfig(record("a.example.com", "", "http://a.ns:80", originRequest{}))
	logger, sink := newRecordingLogger()

	if _, err := c.Sync(t.Context(), logger, config); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := fake.DNSRecordNames(); !reflect.DeepEqual(got, []string{"a.example.com"}) {
		t.Errorf("expected a.example.com to be created, got %v", got)
	}
	if n := sink.count("info", "Skipping zone without DNS read permission"); n != 1 {
		t.Errorf("expected exactly 1 info-level skip log, got %d", n)
	}
	if n := sink.count("error", "Failed to list DNS records"); n != 0 {
		t.Errorf("expected no error-level log for the skipped 403, got %d", n)
	}
}

func TestSync_FailsWhenZoneWithDesiredHostnameIsForbidden(t *testing.T) {
	fake := cftest.New(t)
	fake.DenyDNS(cftest.ZoneID)
	c := newTestClient(fake)
	config := newConfig(record("a.example.com", "", "http://a.ns:80", originRequest{}))
	logger, sink := newRecordingLogger()

	if _, err := c.Sync(t.Context(), logger, config); err == nil {
		t.Fatal("expected Sync to fail for a forbidden zone holding a desired hostname")
	}
	if n := sink.count("error", "Failed to list DNS records"); n != 1 {
		t.Errorf("expected exactly 1 error-level log for the non-skipped 403, got %d", n)
	}
}

func TestSync_ReportsDnsConflict(t *testing.T) {
	fake := cftest.New(t)
	fake.AddDNSRecord("taken.example.com", "somewhere-else.example.net")
	c := newTestClient(fake)
	config := newConfig(record("taken.example.com", "", "http://taken.ns:80", originRequest{}))

	result, err := c.Sync(t.Context(), logr.Discard(), config)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if !reflect.DeepEqual(result.DNSConflicts, []string{"taken.example.com"}) {
		t.Errorf("expected DNS conflict for taken.example.com, got %v", result.DNSConflicts)
	}
}
