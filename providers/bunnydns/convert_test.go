package bunnydns

import (
	"testing"

	"github.com/DNSControl/dnscontrol/v4/models"
)

func TestFromRecordConfigPullZone(t *testing.T) {
	rc := &models.RecordConfig{
		Type: "BUNNY_DNS_PZ",
	}
	rc.SetLabelFromFQDN("cdn.example.com", "example.com")
	rc.MustSetTarget("12345")

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.PullZoneID != 12345 {
		t.Fatalf("expected PullZoneId=12345; got=%d", rec.PullZoneID)
	}
}

func TestFromRecordConfigPullZoneInvalidTarget(t *testing.T) {
	rc := &models.RecordConfig{
		Type: "BUNNY_DNS_PZ",
	}
	rc.SetLabelFromFQDN("cdn.example.com", "example.com")
	rc.MustSetTarget("abc")

	_, err := fromRecordConfig(rc)
	if err == nil {
		t.Fatalf("expected error for invalid Pull Zone ID")
	}
}

func TestToRecordConfigPullZoneLinkName(t *testing.T) {
	rec := &record{
		Type:     recordTypePullZone,
		Name:     "cdn",
		TTL:      300,
		LinkName: "12345",
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if rc.Type != "BUNNY_DNS_PZ" {
		t.Fatalf("expected type BUNNY_DNS_PZ; got=%s", rc.Type)
	}
	if rc.GetTargetField() != "12345" {
		t.Fatalf("expected target 12345; got=%s", rc.GetTargetField())
	}
	if rc.GetLabel() != "cdn" {
		t.Fatalf("expected label cdn; got=%s", rc.GetLabel())
	}
}

func TestToRecordConfigPullZoneMissingID(t *testing.T) {
	rec := &record{
		Type: recordTypePullZone,
		Name: "cdn",
		TTL:  300,
	}

	_, err := toRecordConfig("example.com", rec)
	if err == nil {
		t.Fatalf("expected error for missing Pull Zone LinkName")
	}
}

func TestFromRecordConfigGeographicRouting(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "A",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("1.2.3.4")
	rc.Metadata[metaSmartRoutingType] = "geographic"
	rc.Metadata[metaGeolocationLatitude] = "40.7128"
	rc.Metadata[metaGeolocationLongitude] = "-74.0060"

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.SmartRoutingType != smartRoutingGeographic {
		t.Fatalf("expected SmartRoutingType=%d; got=%d", smartRoutingGeographic, rec.SmartRoutingType)
	}
	if rec.GeolocationLatitude == nil || *rec.GeolocationLatitude != 40.7128 {
		t.Fatalf("expected GeolocationLatitude=40.7128; got=%v", rec.GeolocationLatitude)
	}
	if rec.GeolocationLongitude == nil || *rec.GeolocationLongitude != -74.0060 {
		t.Fatalf("expected GeolocationLongitude=-74.0060; got=%v", rec.GeolocationLongitude)
	}
	if rec.LatencyZone != "" {
		t.Fatalf("expected empty LatencyZone; got=%q", rec.LatencyZone)
	}
}

func TestFromRecordConfigLatencyRouting(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "A",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("1.2.3.4")
	rc.Metadata[metaSmartRoutingType] = "latency"
	rc.Metadata[metaLatencyZone] = "NY"

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.SmartRoutingType != smartRoutingLatency {
		t.Fatalf("expected SmartRoutingType=%d; got=%d", smartRoutingLatency, rec.SmartRoutingType)
	}
	if rec.LatencyZone != "NY" {
		t.Fatalf("expected LatencyZone=NY; got=%q", rec.LatencyZone)
	}
	if rec.GeolocationLatitude != nil || rec.GeolocationLongitude != nil {
		t.Fatalf("expected nil geolocation coords for latency routing")
	}
}

func TestFromRecordConfigInvalidSmartRoutingType(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "A",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("1.2.3.4")
	rc.Metadata[metaSmartRoutingType] = "invalid"

	_, err := fromRecordConfig(rc)
	if err == nil {
		t.Fatalf("expected error for invalid smart routing type")
	}
}

func TestFromRecordConfigInvalidLatitude(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "A",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("1.2.3.4")
	rc.Metadata[metaSmartRoutingType] = "geographic"
	rc.Metadata[metaGeolocationLatitude] = "not-a-number"

	_, err := fromRecordConfig(rc)
	if err == nil {
		t.Fatalf("expected error for invalid latitude")
	}
}

func TestFromRecordConfigSmartRoutingOnlyOnAAndAAAA(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "CNAME",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("target.example.com")
	rc.Metadata[metaSmartRoutingType] = "latency"
	rc.Metadata[metaLatencyZone] = "NY"

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.SmartRoutingType != smartRoutingNone {
		t.Fatalf("expected SmartRoutingType=0 for non-A/AAAA; got=%d", rec.SmartRoutingType)
	}
	if rec.LatencyZone != "" {
		t.Fatalf("expected empty LatencyZone for non-A/AAAA; got=%q", rec.LatencyZone)
	}
}

func TestToRecordConfigGeographicRouting(t *testing.T) {
	lat := 40.7128
	lon := -74.0060
	rec := &record{
		Type:                 recordTypeA,
		Name:                 "www",
		Value:                "1.2.3.4",
		TTL:                  300,
		SmartRoutingType:     smartRoutingGeographic,
		GeolocationLatitude:  &lat,
		GeolocationLongitude: &lon,
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if rc.Metadata[metaSmartRoutingType] != "geographic" {
		t.Fatalf("expected metadata %s=geographic; got=%q", metaSmartRoutingType, rc.Metadata[metaSmartRoutingType])
	}
	if rc.Metadata[metaGeolocationLatitude] != "40.7128" {
		t.Fatalf("expected metadata %s=40.7128; got=%q", metaGeolocationLatitude, rc.Metadata[metaGeolocationLatitude])
	}
	if rc.Metadata[metaGeolocationLongitude] != "-74.006" {
		t.Fatalf("expected metadata %s=-74.006; got=%q", metaGeolocationLongitude, rc.Metadata[metaGeolocationLongitude])
	}
	if _, ok := rc.Metadata[metaLatencyZone]; ok {
		t.Fatalf("expected no %s metadata for geographic routing", metaLatencyZone)
	}
}

func TestToRecordConfigLatencyRouting(t *testing.T) {
	rec := &record{
		Type:             recordTypeAAAA,
		Name:             "www",
		Value:            "::1",
		TTL:              300,
		SmartRoutingType: smartRoutingLatency,
		LatencyZone:      "NY",
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if rc.Metadata[metaSmartRoutingType] != "latency" {
		t.Fatalf("expected metadata %s=latency; got=%q", metaSmartRoutingType, rc.Metadata[metaSmartRoutingType])
	}
	if rc.Metadata[metaLatencyZone] != "NY" {
		t.Fatalf("expected metadata %s=NY; got=%q", metaLatencyZone, rc.Metadata[metaLatencyZone])
	}
	if _, ok := rc.Metadata[metaGeolocationLatitude]; ok {
		t.Fatalf("expected no %s metadata for latency routing", metaGeolocationLatitude)
	}
}

func TestToRecordConfigNoSmartRouting(t *testing.T) {
	rec := &record{
		Type:  recordTypeA,
		Name:  "www",
		Value: "1.2.3.4",
		TTL:   300,
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if len(rc.Metadata) != 0 {
		t.Fatalf("expected no metadata for record without smart routing; got=%v", rc.Metadata)
	}
}

func TestParseSmartRoutingType(t *testing.T) {
	tests := []struct {
		input string
		want  smartRoutingType
		err   bool
	}{
		{"", smartRoutingNone, false},
		{"none", smartRoutingNone, false},
		{"latency", smartRoutingLatency, false},
		{"LATENCY", smartRoutingLatency, false},
		{"geographic", smartRoutingGeographic, false},
		{"geo", smartRoutingGeographic, false},
		{"GEO", smartRoutingGeographic, false},
		{"invalid", smartRoutingNone, true},
	}
	for _, tt := range tests {
		got, err := parseSmartRoutingType(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parseSmartRoutingType(%q) expected error; got nil", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSmartRoutingType(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseSmartRoutingType(%q) expected %d; got %d", tt.input, tt.want, got)
		}
	}
}

func TestFromRecordConfigMonitorPing(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "A",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("1.2.3.4")
	rc.Metadata[metaMonitorType] = "ping"

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.MonitorType != monitorPing {
		t.Fatalf("expected MonitorType=%d; got=%d", monitorPing, rec.MonitorType)
	}
}

func TestFromRecordConfigMonitorHTTP(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "AAAA",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("::1")
	rc.Metadata[metaMonitorType] = "http"

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.MonitorType != monitorHTTP {
		t.Fatalf("expected MonitorType=%d; got=%d", monitorHTTP, rec.MonitorType)
	}
}

func TestFromRecordConfigMonitorCNAME(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "CNAME",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("target.example.com")
	rc.Metadata[metaMonitorType] = "ping"

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.MonitorType != monitorPing {
		t.Fatalf("expected MonitorType=%d; got=%d", monitorPing, rec.MonitorType)
	}
}

func TestFromRecordConfigMonitorOnlyOnSupportedTypes(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "TXT",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("text")
	rc.Metadata[metaMonitorType] = "ping"

	rec, err := fromRecordConfig(rc)
	if err != nil {
		t.Fatalf("fromRecordConfig returned error: %v", err)
	}
	if rec.MonitorType != monitorNone {
		t.Fatalf("expected MonitorType=0 for TXT; got=%d", rec.MonitorType)
	}
}

func TestFromRecordConfigInvalidMonitorType(t *testing.T) {
	rc := &models.RecordConfig{
		Type:     "A",
		Metadata: map[string]string{},
	}
	rc.SetLabelFromFQDN("www.example.com", "example.com")
	rc.MustSetTarget("1.2.3.4")
	rc.Metadata[metaMonitorType] = "invalid"

	_, err := fromRecordConfig(rc)
	if err == nil {
		t.Fatalf("expected error for invalid monitor type")
	}
}

func TestToRecordConfigMonitorPing(t *testing.T) {
	rec := &record{
		Type:        recordTypeA,
		Name:        "www",
		Value:       "1.2.3.4",
		TTL:         300,
		MonitorType: monitorPing,
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if rc.Metadata[metaMonitorType] != "ping" {
		t.Fatalf("expected metadata %s=ping; got=%q", metaMonitorType, rc.Metadata[metaMonitorType])
	}
}

func TestToRecordConfigMonitorHTTP(t *testing.T) {
	rec := &record{
		Type:        recordTypeAAAA,
		Name:        "www",
		Value:       "::1",
		TTL:         300,
		MonitorType: monitorHTTP,
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if rc.Metadata[metaMonitorType] != "http" {
		t.Fatalf("expected metadata %s=http; got=%q", metaMonitorType, rc.Metadata[metaMonitorType])
	}
}

func TestToRecordConfigMonitorCNAME(t *testing.T) {
	rec := &record{
		Type:        recordTypeCNAME,
		Name:        "www",
		Value:       "target",
		TTL:         300,
		MonitorType: monitorPing,
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if rc.Metadata[metaMonitorType] != "ping" {
		t.Fatalf("expected metadata %s=ping; got=%q", metaMonitorType, rc.Metadata[metaMonitorType])
	}
}

func TestToRecordConfigNoMonitor(t *testing.T) {
	rec := &record{
		Type:  recordTypeA,
		Name:  "www",
		Value: "1.2.3.4",
		TTL:   300,
	}

	rc, err := toRecordConfig("example.com", rec)
	if err != nil {
		t.Fatalf("toRecordConfig returned error: %v", err)
	}
	if _, ok := rc.Metadata[metaMonitorType]; ok {
		t.Fatalf("expected no %s metadata for record without monitoring", metaMonitorType)
	}
}

func TestParseMonitorType(t *testing.T) {
	tests := []struct {
		input string
		want  monitorType
		err   bool
	}{
		{"", monitorNone, false},
		{"none", monitorNone, false},
		{"ping", monitorPing, false},
		{"PING", monitorPing, false},
		{"http", monitorHTTP, false},
		{"HTTP", monitorHTTP, false},
		{"monitor", monitorCustom, false},
		{"invalid", monitorNone, true},
	}
	for _, tt := range tests {
		got, err := parseMonitorType(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("parseMonitorType(%q) expected error; got nil", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMonitorType(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseMonitorType(%q) expected %d; got %d", tt.input, tt.want, got)
		}
	}
}
