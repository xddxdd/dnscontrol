package bunnydns

import (
	"fmt"
	"strconv"
	"strings"

	"slices"

	"github.com/DNSControl/dnscontrol/v4/models"
	dnsutilv1 "github.com/miekg/dns/dnsutil"
)

var fqdnTypes = []recordType{recordTypeCNAME, recordTypeHTTPS, recordTypeMX, recordTypeNS, recordTypePTR, recordTypeSRV, recordTypeSVCB}
var nullTypes = []recordType{recordTypeHTTPS, recordTypeMX, recordTypeSVCB}

func fromRecordConfig(rc *models.RecordConfig) (*record, error) {
	r := record{
		Type:  recordTypeFromString(rc.Type),
		Name:  rc.GetLabel(),
		Value: rc.GetTargetField(),
		TTL:   rc.TTL,
	}

	switch r.Type {
	case recordTypeNS:
		if r.Name == "" {
			r.TTL = 0
		}
	case recordTypeSRV:
		r.Priority = rc.SrvPriority
		r.Weight = rc.SrvWeight
		r.Port = rc.SrvPort
	case recordTypeCAA:
		r.Flags = rc.CaaFlag
		r.Tag = rc.CaaTag
	case recordTypeMX:
		r.Priority = rc.MxPreference
	case recordTypeSVCB, recordTypeHTTPS:
		r.Priority = rc.SvcPriority
	case recordTypeTLSA:
		r.Value = fmt.Sprintf("%d %d %d %s", rc.TlsaUsage, rc.TlsaSelector, rc.TlsaMatchingType, rc.GetTargetField())
	case recordTypePullZone:
		// When creating Pull Zone records, the API expects an integer PullZoneId field,
		// while the Value field should be empty.
		pullZoneID, err := strconv.ParseInt(rc.GetTargetField(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Pull Zone ID for BUNNY_DNS_PZ: %w", err)
		}
		r.PullZoneID = pullZoneID
		r.Value = ""
	}

	// While Bunny DNS does not use trailing dots, it still accepts and even preserves them for certain record types.
	// To avoid confusion, any trailing dots are removed from the record value, except when managing a NullMX or a self-pointing HTTPS/SVCB record.
	isNullRecord := slices.Contains(nullTypes, r.Type) && r.Value == "."
	if slices.Contains(fqdnTypes, r.Type) && strings.HasSuffix(r.Value, ".") && !isNullRecord {
		r.Value = strings.TrimSuffix(r.Value, ".")
	}

	switch r.Type {
	case recordTypeSVCB, recordTypeHTTPS:
		// In the case of SVCB/HTTPS records, the Target is part of the Value.
		// After removing trailing dots for said target, we can add the params to the value.
		r.Value = fmt.Sprintf("%s %s", r.Value, rc.SvcParams)
	case recordTypeSRV:
		// SRV empty target is represented as "."
		if r.Value == "" {
			r.Value = "."
		}
	}

	// Smart routing (geographic / latency) metadata only applies to A and AAAA records.
	if r.Type == recordTypeA || r.Type == recordTypeAAAA {
		if srtStr, ok := rc.Metadata[metaSmartRoutingType]; ok {
			srt, err := parseSmartRoutingType(srtStr)
			if err != nil {
				return nil, err
			}
			r.SmartRoutingType = srt

			switch srt {
			case smartRoutingGeographic:
				if latStr, ok := rc.Metadata[metaGeolocationLatitude]; ok {
					lat, err := strconv.ParseFloat(latStr, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid %s: %w", metaGeolocationLatitude, err)
					}
					r.GeolocationLatitude = &lat
				}
				if lonStr, ok := rc.Metadata[metaGeolocationLongitude]; ok {
					lon, err := strconv.ParseFloat(lonStr, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid %s: %w", metaGeolocationLongitude, err)
					}
					r.GeolocationLongitude = &lon
				}
			case smartRoutingLatency:
				r.LatencyZone = rc.Metadata[metaLatencyZone]
			}
		}
	}

	// Health monitoring applies to A, AAAA, and CNAME records.
	if r.Type == recordTypeA || r.Type == recordTypeAAAA || r.Type == recordTypeCNAME {
		if mtStr, ok := rc.Metadata[metaMonitorType]; ok {
			mt, err := parseMonitorType(mtStr)
			if err != nil {
				return nil, err
			}
			r.MonitorType = mt
		}
	}

	return &r, nil
}

func toRecordConfig(domain string, r *record) (*models.RecordConfig, error) {
	rc := models.RecordConfig{
		Type:     recordTypeToString(r.Type),
		TTL:      r.TTL,
		Original: r,
	}
	rc.SetLabel(r.Name, domain)

	// Bunny DNS always operates with fully-qualified names and does not use any trailing dots.
	// If a record already contains a trailing dot, which the provider UI also accepts, the record value is left as-is.
	recordValue := r.Value

	// Bunny DNS has the Target and Params on the same Value, so we have to split them
	recordParts := strings.SplitN(recordValue, " ", 2)

	if slices.Contains(fqdnTypes, r.Type) && !strings.HasSuffix(recordParts[0], ".") {
		recordParts[0] = dnsutilv1.AddOrigin(recordParts[0]+".", domain)
		recordValue = strings.Join(recordParts, " ")
	}

	var err error
	switch rc.Type {
	case "BUNNY_DNS_PZ":
		// When reading Pull Zone records, the API provides the PullZoneId in the LinkName field as string.
		if r.LinkName == "" {
			return nil, fmt.Errorf("missing Pull Zone ID (LinkName) for BUNNY_DNS_PZ")
		}
		err = rc.SetTarget(r.LinkName)
	case "BUNNY_DNS_RDR":
		err = rc.SetTarget(r.Value)
	case "CAA":
		err = rc.SetTargetCAA(r.Flags, r.Tag, recordValue)
	case "MX":
		err = rc.SetTargetMX(r.Priority, recordValue)
	case "SRV":
		err = rc.SetTargetSRV(r.Priority, r.Weight, r.Port, recordValue)
	case "SVCB", "HTTPS":
		err = rc.SetTargetSVCBString(r.Name, fmt.Sprintf("%d %s", r.Priority, recordValue))
	case "TLSA":
		err = rc.SetTargetTLSAString(recordValue)
	default:
		err = rc.PopulateFromStringFunc(rc.Type, recordValue, domain, nil)
	}
	if err != nil {
		return nil, err
	}

	// Smart routing (geographic / latency) metadata only applies to A and AAAA records.
	if r.Type == recordTypeA || r.Type == recordTypeAAAA {
		if r.SmartRoutingType != smartRoutingNone {
			if rc.Metadata == nil {
				rc.Metadata = make(map[string]string)
			}
			rc.Metadata[metaSmartRoutingType] = smartRoutingTypeToString(r.SmartRoutingType)

			switch r.SmartRoutingType {
			case smartRoutingGeographic:
				if r.GeolocationLatitude != nil {
					rc.Metadata[metaGeolocationLatitude] = strconv.FormatFloat(*r.GeolocationLatitude, 'f', -1, 64)
				}
				if r.GeolocationLongitude != nil {
					rc.Metadata[metaGeolocationLongitude] = strconv.FormatFloat(*r.GeolocationLongitude, 'f', -1, 64)
				}
			case smartRoutingLatency:
				if r.LatencyZone != "" {
					rc.Metadata[metaLatencyZone] = r.LatencyZone
				}
			}
		}
	}

	// Health monitoring applies to A, AAAA, and CNAME records.
	if r.Type == recordTypeA || r.Type == recordTypeAAAA || r.Type == recordTypeCNAME {
		if r.MonitorType != monitorNone {
			if rc.Metadata == nil {
				rc.Metadata = make(map[string]string)
			}
			rc.Metadata[metaMonitorType] = monitorTypeToString(r.MonitorType)
		}
	}

	return &rc, nil
}

type recordType int

const (
	recordTypeA        recordType = 0
	recordTypeAAAA     recordType = 1
	recordTypeCNAME    recordType = 2
	recordTypeTXT      recordType = 3
	recordTypeMX       recordType = 4
	recordTypeRedirect recordType = 5
	recordTypeFlatten  recordType = 6
	recordTypePullZone recordType = 7
	recordTypeSRV      recordType = 8
	recordTypeCAA      recordType = 9
	recordTypePTR      recordType = 10
	recordTypeScript   recordType = 11
	recordTypeNS       recordType = 12
	recordTypeSVCB     recordType = 13
	recordTypeHTTPS    recordType = 14
	recordTypeTLSA     recordType = 15
)

func recordTypeFromString(t string) recordType {
	switch t {
	case "A":
		return recordTypeA
	case "AAAA":
		return recordTypeAAAA
	case "CNAME":
		return recordTypeCNAME
	case "TXT":
		return recordTypeTXT
	case "MX":
		return recordTypeMX
	case "FLATTEN":
		return recordTypeFlatten
	case "BUNNY_DNS_PZ":
		return recordTypePullZone
	case "SRV":
		return recordTypeSRV
	case "CAA":
		return recordTypeCAA
	case "PTR":
		return recordTypePTR
	case "SCRIPT":
		return recordTypeScript
	case "NS":
		return recordTypeNS
	case "SVCB":
		return recordTypeSVCB
	case "HTTPS":
		return recordTypeHTTPS
	case "TLSA":
		return recordTypeTLSA
	case "BUNNY_DNS_RDR":
		return recordTypeRedirect
	default:
		panic(fmt.Errorf("BUNNY_DNS: rtype %v unimplemented", t))
	}
}

func recordTypeToString(t recordType) string {
	switch t {
	case recordTypeA:
		return "A"
	case recordTypeAAAA:
		return "AAAA"
	case recordTypeCNAME:
		return "CNAME"
	case recordTypeTXT:
		return "TXT"
	case recordTypeMX:
		return "MX"
	case recordTypeRedirect:
		return "BUNNY_DNS_RDR"
	case recordTypeFlatten:
		return "FLATTEN"
	case recordTypePullZone:
		return "BUNNY_DNS_PZ"
	case recordTypeSRV:
		return "SRV"
	case recordTypeCAA:
		return "CAA"
	case recordTypePTR:
		return "PTR"
	case recordTypeScript:
		return "SCRIPT"
	case recordTypeNS:
		return "NS"
	case recordTypeSVCB:
		return "SVCB"
	case recordTypeHTTPS:
		return "HTTPS"
	case recordTypeTLSA:
		return "TLSA"
	default:
		panic(fmt.Errorf("BUNNY_DNS: native rtype %v unimplemented", t))
	}
}

var errInvalidSmartRoutingType = fmt.Errorf("invalid %s: valid values are 'latency' and 'geographic'", metaSmartRoutingType)

func parseSmartRoutingType(s string) (smartRoutingType, error) {
	switch strings.ToLower(s) {
	case "", "none":
		return smartRoutingNone, nil
	case "latency":
		return smartRoutingLatency, nil
	case "geographic", "geo":
		return smartRoutingGeographic, nil
	default:
		return smartRoutingNone, errInvalidSmartRoutingType
	}
}

func smartRoutingTypeToString(srt smartRoutingType) string {
	switch srt {
	case smartRoutingLatency:
		return "latency"
	case smartRoutingGeographic:
		return "geographic"
	default:
		return "none"
	}
}

var errInvalidMonitorType = fmt.Errorf("invalid %s: valid values are 'ping' and 'http'", metaMonitorType)

func parseMonitorType(s string) (monitorType, error) {
	switch strings.ToLower(s) {
	case "", "none":
		return monitorNone, nil
	case "ping":
		return monitorPing, nil
	case "http":
		return monitorHTTP, nil
	case "monitor":
		return monitorCustom, nil
	default:
		return monitorNone, errInvalidMonitorType
	}
}

func monitorTypeToString(mt monitorType) string {
	switch mt {
	case monitorPing:
		return "ping"
	case monitorHTTP:
		return "http"
	case monitorCustom:
		return "monitor"
	default:
		return "none"
	}
}
