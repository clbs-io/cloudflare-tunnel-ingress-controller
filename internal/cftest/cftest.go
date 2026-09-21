// Package cftest provides an in-memory fake of the Cloudflare API endpoints
// used by the controller, for tests only.
package cftest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/option"
)

const (
	AccountID  = "acc"
	TunnelID   = "tun-id"
	TunnelName = "tun"
	ZoneID     = "zone-example"
	ZoneName   = "example.com"
)

// TunnelTarget is the CNAME content of DNS records that route to the fake tunnel.
const TunnelTarget = TunnelID + ".cfargotunnel.com"

type DNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type AccessApp struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Type   string `json:"type"`
	// Scope is "accounts" or "zones", taken from the request path.
	Scope string `json:"-"`
}

// Server is a stateful fake: configuration PUTs are returned by later GETs,
// DNS records are created and deleted, Access applications are stored.
type Server struct {
	mu sync.Mutex

	ingress    json.RawMessage
	records    []DNSRecord
	accessApps []AccessApp
	nextID     int

	// ConfigPuts holds the ingress array of every tunnel configuration PUT.
	ConfigPuts []json.RawMessage
	// FailConfigPut makes tunnel configuration PUTs fail with HTTP 500.
	FailConfigPut bool

	srv *httptest.Server
}

func New(t testing.TB) *Server {
	s := &Server{ingress: json.RawMessage("[]")}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /accounts/{acc}/tunnels", s.listTunnels)
	mux.HandleFunc("GET /accounts/{acc}/cfd_tunnel/{id}", s.getTunnel)
	mux.HandleFunc("GET /accounts/{acc}/cfd_tunnel/{id}/token", s.getToken)
	mux.HandleFunc("GET /accounts/{acc}/cfd_tunnel/{id}/configurations", s.getConfig)
	mux.HandleFunc("PUT /accounts/{acc}/cfd_tunnel/{id}/configurations", s.putConfig)
	mux.HandleFunc("GET /zones", s.listZones)
	mux.HandleFunc("GET /zones/{zone}/dns_records", s.listRecords)
	mux.HandleFunc("POST /zones/{zone}/dns_records", s.createRecord)
	mux.HandleFunc("DELETE /zones/{zone}/dns_records/{id}", s.deleteRecord)
	mux.HandleFunc("GET /accounts/{acc}/access/apps", s.listAccessApps)
	mux.HandleFunc("POST /{scope}/{id}/access/apps", s.createAccessApp)

	s.srv = httptest.NewTestServer(t, mux)
	return s
}

// Client returns a Cloudflare API client talking to the fake.
func (s *Server) Client() *cloudflare.Client {
	// The in-memory network client routes every request to the fake,
	// whatever the host in the base URL.
	return cloudflare.NewClient(
		option.WithBaseURL("http://cloudflare.test/"),
		option.WithHTTPClient(s.srv.Client()),
		option.WithAPIToken("test-token"),
		option.WithMaxRetries(0),
	)
}

// SetIngress replaces the remote tunnel ingress rules with the given JSON array.
func (s *Server) SetIngress(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ingress = json.RawMessage(raw)
}

// AddDNSRecord stores a CNAME record in the fake zone.
func (s *Server) AddDNSRecord(name, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	s.records = append(s.records, DNSRecord{ID: fmt.Sprintf("rec-%d", s.nextID), Name: name, Type: "CNAME", Content: content})
}

// DNSRecordNames returns the names of all stored DNS records, sorted.
func (s *Server) DNSRecordNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for _, r := range s.records {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	return names
}

// AccessApps returns the stored Access applications.
func (s *Server) AccessApps() []AccessApp {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.accessApps)
}

// AddAccessApp stores an account-level Access application.
func (s *Server) AddAccessApp(name, domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	s.accessApps = append(s.accessApps, AccessApp{ID: fmt.Sprintf("app-%d", s.nextID), Name: name, Domain: domain, Type: "self_hosted", Scope: "accounts"})
}

func writeResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []any{},
		"result":   result,
	})
}

func writeError(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  false,
		"errors":   []any{map[string]any{"code": code, "message": message}},
		"messages": []any{},
		"result":   nil,
	})
}

// writePage returns items on the first page only; the SDK auto-pager stops
// at the first empty page.
func writePage[T any](w http.ResponseWriter, r *http.Request, items []T) {
	if page := r.URL.Query().Get("page"); page != "" && page != "1" {
		items = nil
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"errors":      []any{},
		"messages":    []any{},
		"result":      append([]T{}, items...),
		"result_info": map[string]any{"page": 1, "per_page": 50, "count": len(items), "total_count": len(items)},
	})
}

func (s *Server) listTunnels(w http.ResponseWriter, r *http.Request) {
	writePage(w, r, []map[string]any{{"id": TunnelID, "name": TunnelName}})
}

func (s *Server) getTunnel(w http.ResponseWriter, r *http.Request) {
	writeResult(w, map[string]any{"id": TunnelID, "name": TunnelName})
}

func (s *Server) getToken(w http.ResponseWriter, r *http.Request) {
	writeResult(w, "tunnel-token")
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeResult(w, map[string]any{
		"account_id": AccountID,
		"tunnel_id":  TunnelID,
		"version":    len(s.ConfigPuts) + 1,
		"config":     map[string]any{"ingress": s.ingress},
	})
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Config struct {
			Ingress json.RawMessage `json:"ingress"`
		} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailConfigPut {
		writeError(w, http.StatusInternalServerError, 1002, "configuration update failed")
		return
	}
	s.ConfigPuts = append(s.ConfigPuts, body.Config.Ingress)
	s.ingress = body.Config.Ingress
	writeResult(w, map[string]any{"account_id": AccountID, "tunnel_id": TunnelID, "config": map[string]any{"ingress": s.ingress}})
}

func (s *Server) listZones(w http.ResponseWriter, r *http.Request) {
	writePage(w, r, []map[string]any{{"id": ZoneID, "name": ZoneName}})
}

func (s *Server) listRecords(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content := r.URL.Query().Get("content.exact")
	var out []DNSRecord
	for _, rec := range s.records {
		if content == "" || rec.Content == content {
			out = append(out, rec)
		}
	}
	writePage(w, r, out)
}

func (s *Server) createRecord(w http.ResponseWriter, r *http.Request) {
	var rec DNSRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		writeError(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.ContainsFunc(s.records, func(e DNSRecord) bool { return e.Name == rec.Name }) {
		writeError(w, http.StatusBadRequest, 81053, "An A, AAAA, or CNAME record with that host already exists.")
		return
	}
	s.nextID++
	rec.ID = fmt.Sprintf("rec-%d", s.nextID)
	s.records = append(s.records, rec)
	writeResult(w, rec)
}

func (s *Server) deleteRecord(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	s.records = slices.DeleteFunc(s.records, func(e DNSRecord) bool { return e.ID == id })
	writeResult(w, map[string]any{"id": id})
}

func (s *Server) listAccessApps(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writePage(w, r, s.accessApps)
}

func (s *Server) createAccessApp(w http.ResponseWriter, r *http.Request) {
	var app AccessApp
	if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
		writeError(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	app.ID = fmt.Sprintf("app-%d", s.nextID)
	app.Scope = r.PathValue("scope")
	s.accessApps = append(s.accessApps, app)
	writeResult(w, app)
}
