package bunnydns

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/DNSControl/dnscontrol/v5/models"
	"github.com/DNSControl/dnscontrol/v5/pkg/diff2"
	"github.com/DNSControl/dnscontrol/v5/pkg/printer"
)

func (b *bunnydnsProvider) GetZoneRecords(dc *models.DomainConfig) (models.Records, error) {
	domain := dc.Name

	zone, err := b.findZoneByDomain(domain)
	if err != nil {
		return nil, err
	}

	nativeRecs, err := b.getAllRecords(zone.ID)
	if err != nil {
		return nil, err
	}

	recs := make(models.Records, 0, len(nativeRecs))

	// Define a list of record types that are currently not supported by this provider.
	unsupportedTypes := []recordType{
		recordTypeFlatten,
	}

	// Loop through all native records and convert them to standardized RecordConfigs
	// Unsupported record types are ignored with a warning and will remain untouched in the zone.
	for _, nativeRec := range nativeRecs {
		if slices.Contains(unsupportedTypes, nativeRec.Type) {
			printer.Warnf("BUNNY_DNS: ignoring unsupported record type %s\n", recordTypeToString(nativeRec.Type))
			continue
		}

		rc, err := b.toRecordConfig(dc, nativeRec)
		if err != nil {
			return nil, err
		}
		recs = append(recs, rc)
	}

	return recs, nil
}

func (b *bunnydnsProvider) GetZoneRecordsCorrections(dc *models.DomainConfig, existing models.Records) ([]*models.Correction, int, error) {
	// As no TTL can be configured or retrieved for these NS records, we set it to 0 to avoid unnecessary updates.
	for _, rc := range dc.Records {
		if rc.Name == "@" && rc.Type == "NS" {
			rc.TTL = 0
		}

		if rc.Type == "BUNNY_DNS_RDR" {
			rc.TTL = 0
		}

		if rc.Type == "ALIAS" {
			rc.ChangeTypeToCNAME(dc, rc.AsALIAS().Target)
		}
	}

	zone, err := b.findZoneByDomain(dc.Name)
	if err != nil {
		return nil, 0, err
	}

	instructions, actualChangeCount, err := diff2.ByRecord(existing, dc, comparableFunc)
	if err != nil {
		return nil, 0, err
	}

	var corrections []*models.Correction
	for _, inst := range instructions {
		switch inst.Type {
		case diff2.REPORT:
			corrections = append(corrections, &models.Correction{
				Msg: inst.MsgsJoined,
			})
		case diff2.CREATE:
			corrections = append(corrections, b.mkCreateCorrection(
				zone.ID, inst.New[0], inst.Msgs[0],
			))
		case diff2.CHANGE:
			corrections = append(corrections, b.mkChangeCorrection(
				zone.ID, inst.Old[0], inst.New[0], inst.Msgs[0],
			))
		case diff2.DELETE:
			corrections = append(corrections, b.mkDeleteCorrection(
				zone.ID, inst.Old[0], inst.Msgs[0],
			))
		default:
			panic(fmt.Sprintf("unhandled inst.Type %s", inst.Type))
		}
	}

	dnssecCorrections, err := b.getDNSSECCorrections(dc, zone)
	if err != nil {
		return nil, 0, err
	}
	corrections = append(corrections, dnssecCorrections...)

	return corrections, actualChangeCount, nil
}

// recordForDeployment converts a RecordConfig into a native record for deployment.
// Managed scripts (BUNNY_DNS_SCRIPT) are referenced by name and code only; their
// script ID is resolved here, fetching or creating the script as needed. This must
// only be called while actually applying corrections, never while generating records.
func (b *bunnydnsProvider) recordForDeployment(rc *models.RecordConfig) (*record, error) {
	if rc.Type == "BUNNY_DNS_SCRIPT" {
		scriptID, err := b.deployScript(scriptNameForRecord(rc), rc.GetTargetField())
		if err != nil {
			return nil, err
		}
		r, err := fromRecordConfig(rc)
		if err != nil {
			return nil, err
		}
		r.ScriptID = scriptID
		return r, nil
	}
	return fromRecordConfig(rc)
}

func (b *bunnydnsProvider) mkCreateCorrection(zoneID int64, newRec *models.RecordConfig, msg string) *models.Correction {
	return &models.Correction{
		Msg: msg,
		F: func() error {
			desired, err := b.recordForDeployment(newRec)
			if err != nil {
				return err
			}

			return b.createRecord(zoneID, desired)
		},
	}
}

func (b *bunnydnsProvider) mkChangeCorrection(zoneID int64, oldRec, newRec *models.RecordConfig, msg string) *models.Correction {
	return &models.Correction{
		Msg: msg,
		F: func() error {
			existingID := oldRec.Original.(int64)
			desired, err := b.recordForDeployment(newRec)
			if err != nil {
				return err
			}

			return b.modifyRecord(zoneID, existingID, desired)
		},
	}
}

func (b *bunnydnsProvider) mkDeleteCorrection(zoneID int64, oldRec *models.RecordConfig, msg string) *models.Correction {
	return &models.Correction{
		Msg: msg,
		F: func() error {
			existingID := oldRec.Original.(int64)
			return b.deleteRecord(zoneID, existingID)
		},
	}
}

// comparableFunc feeds the provider-managed metadata (the bunny_* keys set by
// this provider) into diff2's comparison. Other metadata keys are excluded:
// in particular normalize stamps "orig_custom_type" onto every custom record
// type, which would otherwise make desired and existing records compare
// unequal on every run.
func comparableFunc(rec *models.RecordConfig) string {
	var managed map[string]string
	for k, v := range rec.Metadata {
		if strings.HasPrefix(k, "bunny_") {
			if managed == nil {
				managed = make(map[string]string)
			}
			managed[k] = v
		}
	}
	if len(managed) == 0 {
		return ""
	}

	result, err := json.Marshal(managed)
	if err != nil {
		printer.Warnf("BUNNY_DNS: Cannot serialize metadata of record %s", rec)
		return ""
	}

	return string(result)
}
