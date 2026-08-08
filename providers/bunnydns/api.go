package bunnydns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"slices"

	"github.com/DNSControl/dnscontrol/v5/pkg/printer"
)

var baseURL = "https://api.bunny.net"

const (
	pageSize         = 100
	scriptPageSize   = 1000
	scriptTypeDNS    = 0
	scriptNamePrefix = "dnscontrol-"
)

type zone struct {
	ID          int64  `json:"Id"`
	Domain      string `json:"Domain"`
	Nameserver1 string `json:"Nameserver1"`
	Nameserver2 string `json:"Nameserver2"`
	HasDNSSEC   bool   `json:"DnsSecEnabled"`
}

type record struct {
	ID         int64      `json:"Id,omitempty"`
	Type       recordType `json:"Type"`
	Name       string     `json:"Name"`
	Value      string     `json:"Value"`
	Disabled   bool       `json:"Disabled"`
	TTL        uint32     `json:"Ttl"`
	Flags      uint8      `json:"Flags"`
	Priority   uint16     `json:"Priority"`
	Weight     uint16     `json:"Weight"`
	Port       uint16     `json:"Port"`
	Tag        string     `json:"Tag"`
	PullZoneID int64      `json:"PullZoneId,omitempty"`
	ScriptID   int64      `json:"ScriptId,omitempty"`
	LinkName   string     `json:"LinkName,omitempty"`

	SmartRoutingType     smartRoutingType `json:"SmartRoutingType,omitempty"`
	GeolocationLatitude  *float64         `json:"GeolocationLatitude,omitempty"`
	GeolocationLongitude *float64         `json:"GeolocationLongitude,omitempty"`
	LatencyZone          string           `json:"LatencyZone,omitempty"`
	MonitorType          monitorType      `json:"MonitorType,omitempty"`
}

type smartRoutingType int

const (
	smartRoutingNone       smartRoutingType = 0
	smartRoutingLatency    smartRoutingType = 1
	smartRoutingGeographic smartRoutingType = 2
)

type monitorType int

const (
	monitorNone   monitorType = 0
	monitorPing   monitorType = 1
	monitorHTTP   monitorType = 2
	monitorCustom monitorType = 3
)

type listZonesResponse struct {
	Items        []zone `json:"Items"`
	TotalItems   int32  `json:"TotalItems"`
	HasMoreItems bool   `json:"HasMoreItems"`
}

type getZoneResponse struct {
	zone
	Records []record `json:"Records"`
}

type script struct {
	ID         int64  `json:"Id"`
	Name       string `json:"Name"`
	ScriptType int    `json:"ScriptType"`
}

type listScriptsResponse struct {
	Items        []script `json:"Items"`
	TotalItems   int32    `json:"TotalItems"`
	HasMoreItems bool     `json:"HasMoreItems"`
}

type addScriptRequest struct {
	Name       string `json:"Name"`
	Code       string `json:"Code"`
	ScriptType int    `json:"ScriptType"`
}

type scriptCodeResponse struct {
	Code string `json:"Code"`
}

type updateScriptCodeRequest struct {
	Code string `json:"Code"`
}

type publishScriptRequest struct {
	Note string `json:"Note"`
}

type queryParams map[string]string

func (b *bunnydnsProvider) findZoneByDomain(domain string) (*zone, error) {
	if b.zones == nil {
		zones, err := b.getAllZones()
		if err != nil {
			return nil, err
		}

		b.zones = make(map[string]*zone, len(zones))
		for _, zone := range zones {
			b.zones[zone.Domain] = zone
		}
	}

	zone, ok := b.zones[domain]
	if !ok {
		return nil, fmt.Errorf("%q is not a zone in this BUNNY_DNS account", domain)
	}

	return zone, nil
}

func (b *bunnydnsProvider) getAllZones() ([]*zone, error) {
	var zones []*zone
	page := 1

	for {
		res := listZonesResponse{}
		query := queryParams{"page": strconv.Itoa(page), "perPage": strconv.Itoa(pageSize)}
		if err := b.request("GET", "/dnszone", query, nil, &res, nil); err != nil {
			return nil, fmt.Errorf("could not fetch zones: %w", err)
		}

		if zones == nil {
			zones = make([]*zone, 0, res.TotalItems)
		}
		for i := range res.Items {
			zones = append(zones, &res.Items[i])
		}

		if !res.HasMoreItems {
			break
		}
		page++
	}

	return zones, nil
}

func (b *bunnydnsProvider) createZone(domain string) (*zone, error) {
	zone := &zone{}
	body := map[string]string{"domain": domain}
	err := b.request("POST", "/dnszone", nil, body, &zone, []int{http.StatusCreated})
	if err != nil {
		return nil, err
	}

	b.zones[domain] = zone
	return zone, nil
}

func (b *bunnydnsProvider) getAllRecords(zoneID int64) ([]*record, error) {
	zone := &getZoneResponse{}
	err := b.request("GET", fmt.Sprintf("/dnszone/%d", zoneID), nil, nil, zone, nil)
	if err != nil {
		return nil, err
	}

	records := make([]*record, 0, len(zone.Records))
	for i := range zone.Records {
		records = append(records, &zone.Records[i])
	}

	return records, nil
}

func (b *bunnydnsProvider) createRecord(zoneID int64, r *record) error {
	url := fmt.Sprintf("/dnszone/%d/records", zoneID)
	return b.request("PUT", url, nil, r, nil, []int{http.StatusCreated})
}

func (b *bunnydnsProvider) modifyRecord(zoneID int64, recordID int64, r *record) error {
	url := fmt.Sprintf("/dnszone/%d/records/%d", zoneID, recordID)
	return b.request("POST", url, nil, r, nil, []int{http.StatusNoContent})
}

func (b *bunnydnsProvider) deleteRecord(zoneID, recordID int64) error {
	url := fmt.Sprintf("/dnszone/%d/records/%d", zoneID, recordID)
	return b.request("DELETE", url, nil, nil, nil, []int{http.StatusNoContent})
}

func (b *bunnydnsProvider) getAllScripts() ([]*script, error) {
	var scripts []*script
	page := 1

	for {
		res := listScriptsResponse{}
		query := queryParams{"page": strconv.Itoa(page), "perPage": strconv.Itoa(scriptPageSize)}
		if err := b.request("GET", "/compute/script", query, nil, &res, nil); err != nil {
			return nil, fmt.Errorf("could not fetch scripts: %w", err)
		}

		if scripts == nil {
			scripts = make([]*script, 0, res.TotalItems)
		}
		for i := range res.Items {
			scripts = append(scripts, &res.Items[i])
		}

		if !res.HasMoreItems {
			break
		}
		page++
	}

	return scripts, nil
}

func (b *bunnydnsProvider) createScript(name, code string) (*script, error) {
	body := addScriptRequest{
		Name:       name,
		Code:       code,
		ScriptType: scriptTypeDNS,
	}
	s := &script{}
	err := b.request("POST", "/compute/script", nil, body, s, []int{http.StatusCreated})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (b *bunnydnsProvider) updateScriptCode(scriptID int64, code string) error {
	body := updateScriptCodeRequest{Code: code}
	url := fmt.Sprintf("/compute/script/%d/code", scriptID)
	return b.request("POST", url, nil, body, nil, []int{http.StatusNoContent})
}

// publishScript publishes the script's current code as a release, making it
// live. Bunny requires this step after creating a script or updating its code;
// without it the saved code stays staged and never takes effect. The body is
// required: Bunny rejects bodyless POSTs with 415 Unsupported Media Type.
func (b *bunnydnsProvider) publishScript(scriptID int64) error {
	url := fmt.Sprintf("/compute/script/%d/publish", scriptID)
	return b.request("POST", url, nil, publishScriptRequest{Note: "Deployed by DNSControl"}, nil, []int{http.StatusNoContent, http.StatusOK})
}

func (b *bunnydnsProvider) fetchScriptCode(scriptID int64) (string, error) {
	res := scriptCodeResponse{}
	url := fmt.Sprintf("/compute/script/%d/code", scriptID)
	if err := b.request("GET", url, nil, nil, &res, nil); err != nil {
		return "", err
	}
	return res.Code, nil
}

// ensureScriptsLoaded fetches and caches all scripts of the account, if not done yet.
func (b *bunnydnsProvider) ensureScriptsLoaded() error {
	if b.scripts != nil {
		return nil
	}

	scripts, err := b.getAllScripts()
	if err != nil {
		return err
	}

	b.scripts = make(map[int64]*script, len(scripts))
	for _, s := range scripts {
		b.scripts[s.ID] = s
	}
	return nil
}

func (b *bunnydnsProvider) findScriptByID(id int64) (*script, error) {
	if err := b.ensureScriptsLoaded(); err != nil {
		return nil, err
	}
	return b.scripts[id], nil
}

func (b *bunnydnsProvider) findScriptByName(name string) (*script, error) {
	if err := b.ensureScriptsLoaded(); err != nil {
		return nil, err
	}
	for _, s := range b.scripts {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, nil
}

func (b *bunnydnsProvider) getScriptCode(scriptID int64) (string, error) {
	if b.codes != nil {
		if code, ok := b.codes[scriptID]; ok {
			return code, nil
		}
	}

	code, err := b.fetchScriptCode(scriptID)
	if err != nil {
		return "", err
	}

	if b.codes == nil {
		b.codes = make(map[int64]string)
	}
	b.codes[scriptID] = code
	return code, nil
}

// deployScript ensures a script with the given name, code, and released code
// exists, returning its ID. Existing scripts are matched by name and their code
// is updated and published if it differs.
func (b *bunnydnsProvider) deployScript(name, code string) (int64, error) {
	if err := b.ensureScriptsLoaded(); err != nil {
		return 0, err
	}

	for _, s := range b.scripts {
		if s.Name == name {
			existingCode, err := b.getScriptCode(s.ID)
			if err != nil {
				return 0, err
			}
			if existingCode != code {
				if err := b.updateScriptCode(s.ID, code); err != nil {
					return 0, err
				}
				if err := b.publishScript(s.ID); err != nil {
					return 0, err
				}
				b.codes[s.ID] = code
			}
			return s.ID, nil
		}
	}

	s, err := b.createScript(name, code)
	if err != nil {
		return 0, err
	}
	// Script creation only stages the code; it must be published to go live.
	if err := b.publishScript(s.ID); err != nil {
		return 0, err
	}
	b.scripts[s.ID] = s
	if b.codes == nil {
		b.codes = make(map[int64]string)
	}
	b.codes[s.ID] = code
	return s.ID, nil
}

func (b *bunnydnsProvider) enableDNSSEC(zoneID int64) error {
	url := fmt.Sprintf("/dnszone/%d/dnssec", zoneID)
	return b.request("POST", url, nil, nil, nil, []int{http.StatusOK})
}

func (b *bunnydnsProvider) disableDNSSEC(zoneID int64) error {
	url := fmt.Sprintf("/dnszone/%d/dnssec", zoneID)
	return b.request("DELETE", url, nil, nil, nil, []int{http.StatusOK})
}

func (b *bunnydnsProvider) request(method, endpoint string, query queryParams, body, target any, validStatus []int) error {
	if validStatus == nil {
		validStatus = []int{http.StatusOK}
	}

	var requestBody io.Reader
	if body != nil {
		requestBodyJSON, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewBuffer(requestBodyJSON)
	}

	req, err := http.NewRequest(method, baseURL+endpoint, requestBody)
	if err != nil {
		return err
	}

	req.Header.Add("AccessKey", b.apiKey)
	if requestBody != nil {
		req.Header.Add("Content-Type", "application/json")
	}

	if query != nil {
		q := req.URL.Query()
		for k, v := range query {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	cleanup := func() {
		if err := resp.Body.Close(); err != nil {
			printer.Printf("BUNNY_DNS: Could not close response body after API call: %q\n", err)
		}
	}

	if !slices.Contains(validStatus, resp.StatusCode) {
		data, _ := io.ReadAll(resp.Body)
		printer.Println(fmt.Sprintf("BUNNY_DNS: Bad API response for %s %s: %s", method, endpoint, string(data)))
		cleanup()
		return fmt.Errorf("bad status code from BUNNY_DNS: %d not in %v", resp.StatusCode, validStatus)
	}

	if target == nil {
		cleanup()
		return nil
	}

	err = json.NewDecoder(resp.Body).Decode(target)
	cleanup()
	return err
}
