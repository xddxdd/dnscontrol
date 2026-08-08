package bunnydns

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DNSControl/dnscontrol/v5/models"
)

// newTestServer returns an httptest server that mocks the Bunny compute script API.
func newTestServer(t *testing.T) (*[]addScriptRequest, *[]updateScriptCodeRequest, *[]int64) {
	t.Helper()

	scripts := []script{
		{ID: 1, Name: "dnscontrol-cdn.example.com", ScriptType: scriptTypeDNS},
	}
	codes := map[int64]string{1: "old code"}
	var created []addScriptRequest
	var updated []updateScriptCodeRequest
	var published []int64

	mux := http.NewServeMux()
	mux.HandleFunc("/compute/script", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(listScriptsResponse{Items: scripts, TotalItems: int32(len(scripts))})
		case http.MethodPost:
			var req addScriptRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode addScriptRequest: %v", err)
				return
			}
			created = append(created, req)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(script{ID: 2, Name: req.Name, ScriptType: req.ScriptType})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/compute/script/1/code", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(scriptCodeResponse{Code: codes[1]})
		case http.MethodPost:
			var req updateScriptCodeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode updateScriptCodeRequest: %v", err)
				return
			}
			updated = append(updated, req)
			codes[1] = req.Code
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	for _, id := range []int64{1, 2} {
		mux.HandleFunc(fmt.Sprintf("/compute/script/%d/publish", id), func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// Bunny rejects bodyless POSTs with 415; the publish request must carry a JSON body.
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("publish: expected Content-Type application/json; got=%q", ct)
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			var req publishScriptRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode publishScriptRequest: %v", err)
				return
			}
			published = append(published, id)
			w.WriteHeader(http.StatusNoContent)
		})
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	return &created, &updated, &published
}

func TestDeployScriptUpdateExisting(t *testing.T) {
	_, updated, published := newTestServer(t)
	b := &bunnydnsProvider{apiKey: "test"}

	// The script exists with a different code, so it must be updated and published.
	id, err := b.deployScript("dnscontrol-cdn.example.com", "new code")
	if err != nil {
		t.Fatalf("deployScript returned error: %v", err)
	}
	if id != 1 {
		t.Fatalf("expected script ID 1; got=%d", id)
	}
	if len(*updated) != 1 || (*updated)[0].Code != "new code" {
		t.Fatalf("expected code update to 'new code'; got=%v", *updated)
	}
	if len(*published) != 1 || (*published)[0] != 1 {
		t.Fatalf("expected script 1 to be published; got=%v", *published)
	}

	// Deploying the same code again must not trigger another update or publish.
	id, err = b.deployScript("dnscontrol-cdn.example.com", "new code")
	if err != nil {
		t.Fatalf("deployScript returned error: %v", err)
	}
	if id != 1 {
		t.Fatalf("expected script ID 1; got=%d", id)
	}
	if len(*updated) != 1 {
		t.Fatalf("expected no additional update; got=%d updates", len(*updated))
	}
	if len(*published) != 1 {
		t.Fatalf("expected no additional publish; got=%d publishes", len(*published))
	}
}

func TestDeployScriptCreateNew(t *testing.T) {
	created, _, published := newTestServer(t)
	b := &bunnydnsProvider{apiKey: "test"}

	id, err := b.deployScript("dnscontrol-other.example.com", "code")
	if err != nil {
		t.Fatalf("deployScript returned error: %v", err)
	}
	if id != 2 {
		t.Fatalf("expected script ID 2; got=%d", id)
	}
	if len(*created) != 1 {
		t.Fatalf("expected 1 script creation; got=%d", len(*created))
	}
	req := (*created)[0]
	if req.Name != "dnscontrol-other.example.com" || req.Code != "code" || req.ScriptType != scriptTypeDNS {
		t.Fatalf("unexpected create request: %+v", req)
	}
	if len(*published) != 1 || (*published)[0] != 2 {
		t.Fatalf("expected new script 2 to be published; got=%v", *published)
	}
}

func TestRecordForDeploymentManagedScriptUpdate(t *testing.T) {
	_, updated, published := newTestServer(t)
	b := &bunnydnsProvider{apiKey: "test"}
	dc := models.MustNewDomainConfig("example.com")
	rc := dc.MustNewRecordConfig("cdn", 300, "BUNNY_DNS_SCRIPT", "new code")

	rec, err := b.recordForDeployment(rc)
	if err != nil {
		t.Fatalf("recordForDeployment returned error: %v", err)
	}
	if rec.ScriptID != 1 {
		t.Fatalf("expected ScriptID=1; got=%d", rec.ScriptID)
	}
	if len(*updated) != 1 || (*updated)[0].Code != "new code" {
		t.Fatalf("expected code update to 'new code'; got=%v", *updated)
	}
	if len(*published) != 1 || (*published)[0] != 1 {
		t.Fatalf("expected script 1 to be published; got=%v", *published)
	}
}

func TestRecordForDeploymentManagedScriptCreate(t *testing.T) {
	created, _, published := newTestServer(t)
	b := &bunnydnsProvider{apiKey: "test"}
	dc := models.MustNewDomainConfig("example.com")
	rc := dc.MustNewRecordConfig("other", 300, "BUNNY_DNS_SCRIPT", "code")

	rec, err := b.recordForDeployment(rc)
	if err != nil {
		t.Fatalf("recordForDeployment returned error: %v", err)
	}
	if rec.ScriptID != 2 {
		t.Fatalf("expected ScriptID=2; got=%d", rec.ScriptID)
	}
	if len(*created) != 1 {
		t.Fatalf("expected 1 script creation; got=%d", len(*created))
	}
	if len(*published) != 1 || (*published)[0] != 2 {
		t.Fatalf("expected new script 2 to be published; got=%v", *published)
	}
}

// TestEndToEndManagedScript verifies that scripts are only created or updated
// while actually applying corrections (deployment), never while reading records
// or generating corrections.
func TestEndToEndManagedScript(t *testing.T) {
	zoneID := int64(42)
	recordID := int64(7)
	scripts := []script{{ID: 1, Name: "dnscontrol-cdn.example.com", ScriptType: scriptTypeDNS}}
	codes := map[int64]string{1: "code1"}
	var updatedCodes []string
	var createdScripts []addScriptRequest
	var modifiedRecords []record
	var publishedScripts []int64

	mux := http.NewServeMux()
	mux.HandleFunc("/dnszone", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(listZonesResponse{Items: []zone{{
			ID: zoneID, Domain: "example.com", Nameserver1: "ns1.bunny.net", Nameserver2: "ns2.bunny.net",
		}}, TotalItems: 1})
	})
	mux.HandleFunc("/dnszone/42", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(getZoneResponse{
			zone: zone{Domain: "example.com", Nameserver1: "ns1.bunny.net", Nameserver2: "ns2.bunny.net"},
			Records: []record{
				{ID: 8, Type: recordTypeNS, Name: "@", Value: "ns1.bunny.net"},
				{ID: 9, Type: recordTypeNS, Name: "@", Value: "ns2.bunny.net"},
				{ID: recordID, Type: recordTypeScript, Name: "cdn", TTL: 300, LinkName: "1"},
			},
		})
	})
	mux.HandleFunc("/dnszone/42/records/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var rec record
			_ = json.NewDecoder(r.Body).Decode(&rec)
			modifiedRecords = append(modifiedRecords, rec)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/compute/script", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(listScriptsResponse{Items: scripts, TotalItems: int32(len(scripts))})
		case http.MethodPost:
			var req addScriptRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			createdScripts = append(createdScripts, req)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(script{ID: 2, Name: req.Name, ScriptType: req.ScriptType})
		}
	})
	mux.HandleFunc("/compute/script/1/code", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(scriptCodeResponse{Code: codes[1]})
		case http.MethodPost:
			var req updateScriptCodeRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			updatedCodes = append(updatedCodes, req.Code)
			codes[1] = req.Code
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/compute/script/1/publish", func(w http.ResponseWriter, r *http.Request) {
		publishedScripts = append(publishedScripts, 1)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	b := &bunnydnsProvider{apiKey: "test"}

	// Reading existing records must not create or update scripts.
	dc := models.MustNewDomainConfig("example.com")
	existing, err := b.GetZoneRecords(dc)
	if err != nil {
		t.Fatal(err)
	}
	if len(createdScripts) != 0 || len(updatedCodes) != 0 {
		t.Fatalf("reading records must not create/update scripts; created=%v updated=%v", createdScripts, updatedCodes)
	}
	var scriptRec *models.RecordConfig
	for _, r := range existing {
		if r.Type == "BUNNY_DNS_SCRIPT" {
			scriptRec = r
		}
	}
	if scriptRec == nil {
		t.Fatal("expected existing BUNNY_DNS_SCRIPT record")
	}
	if scriptRec.GetTargetField() != "code1" {
		t.Fatalf("expected target code1; got=%q", scriptRec.GetTargetField())
	}

	// Generating corrections must not create or update scripts either.
	dc.Records = models.Records{
		dc.MustNewRecordConfig("@", 0, "NS", "ns1.bunny.net."),
		dc.MustNewRecordConfig("@", 0, "NS", "ns2.bunny.net."),
		dc.MustNewRecordConfig("cdn", 300, "BUNNY_DNS_SCRIPT", "code2"),
	}
	corrections, count, err := b.GetZoneRecordsCorrections(dc, existing)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 change; got=%d", count)
	}
	if len(createdScripts) != 0 || len(updatedCodes) != 0 {
		t.Fatalf("generating corrections must not create/update scripts; created=%v updated=%v", createdScripts, updatedCodes)
	}

	// Only executing the corrections (actual deployment) may touch scripts.
	for _, c := range corrections {
		if c.F != nil {
			if err := c.F(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(updatedCodes) != 1 || updatedCodes[0] != "code2" {
		t.Fatalf("expected script code updated to code2; got=%v", updatedCodes)
	}
	if len(publishedScripts) != 1 {
		t.Fatalf("expected updated script to be published once; got=%v", publishedScripts)
	}
	if len(modifiedRecords) != 1 || modifiedRecords[0].ScriptID != 1 {
		t.Fatalf("expected DNS record modified with ScriptID=1; got=%v", modifiedRecords)
	}
}

// TestEndToEndScriptNoChange verifies that a BUNNY_DNS_SCRIPT record whose
// script code matches the zone produces no corrections. normalize stamps
// "orig_custom_type" onto every custom record type; that metadata must not
// make desired and existing records compare unequal.
func TestEndToEndScriptNoChange(t *testing.T) {
	zoneID := int64(42)
	scripts := []script{{ID: 1, Name: "dnscontrol-cdn.example.com", ScriptType: scriptTypeDNS}}
	codes := map[int64]string{1: "code1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/dnszone", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(listZonesResponse{Items: []zone{{
			ID: zoneID, Domain: "example.com", Nameserver1: "ns1.bunny.net", Nameserver2: "ns2.bunny.net",
		}}, TotalItems: 1})
	})
	mux.HandleFunc("/dnszone/42", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(getZoneResponse{
			zone: zone{Domain: "example.com", Nameserver1: "ns1.bunny.net", Nameserver2: "ns2.bunny.net"},
			Records: []record{
				{ID: 8, Type: recordTypeNS, Name: "@", Value: "ns1.bunny.net"},
				{ID: 9, Type: recordTypeNS, Name: "@", Value: "ns2.bunny.net"},
				{ID: 7, Type: recordTypeScript, Name: "cdn", TTL: 300, LinkName: "1"},
			},
		})
	})
	mux.HandleFunc("/compute/script", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(listScriptsResponse{Items: scripts, TotalItems: int32(len(scripts))})
	})
	mux.HandleFunc("/compute/script/1/code", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(scriptCodeResponse{Code: codes[1]})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	b := &bunnydnsProvider{apiKey: "test"}
	dc := models.MustNewDomainConfig("example.com")
	existing, err := b.GetZoneRecords(dc)
	if err != nil {
		t.Fatal(err)
	}

	// Desired records as they look after normalize: the script record carries
	// the "orig_custom_type" metadata stamped onto custom record types.
	dc.Records = models.Records{
		dc.MustNewRecordConfig("@", 0, "NS", "ns1.bunny.net."),
		dc.MustNewRecordConfig("@", 0, "NS", "ns2.bunny.net."),
		func() *models.RecordConfig {
			rc := dc.MustNewRecordConfig("cdn", 300, "BUNNY_DNS_SCRIPT", "code1")
			rc.Metadata = map[string]string{"orig_custom_type": "BUNNY_DNS_SCRIPT"}
			return rc
		}(),
	}

	_, count, err := b.GetZoneRecordsCorrections(dc, existing)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 changes for identical script record; got=%d", count)
	}
}
