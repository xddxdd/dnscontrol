package bunnydns

import (
	"testing"

	"github.com/DNSControl/dnscontrol/v5/models"
	"github.com/DNSControl/dnscontrol/v5/pkg/providergolden"
)

func TestToRecordConfigGolden(t *testing.T) {
	providergolden.CheckToRC(t, "toRecordConfig",
		func(dc *models.DomainConfig, native record) (models.Records, error) {
			// BUNNY_DNS_SCRIPT records resolve their code from the script API via
			// the script name in LinkName. The recording pins the same script at
			// two points in time (before and after its code changed); the TTL
			// distinguishes the two states, so the caches below must stay in
			// sync with the test_data recording.
			b := &bunnydnsProvider{
				scripts: map[int64]*script{
					1: {ID: 1, Name: "dnscontrol-fn.example.com", ScriptType: scriptTypeDNS},
				},
			}
			if native.Type == recordTypeScript {
				if native.TTL == 1 {
					b.codes = map[int64]string{1: "export default { async fetch() { return new Response('hello'); } };"}
				} else {
					b.codes = map[int64]string{1: "export default { async fetch() { return new Response('world'); } };"}
				}
			}
			rc, err := b.toRecordConfig(dc, &native)
			return models.Records{rc}, err
		})
}

func TestFromRecordConfigGolden(t *testing.T) {
	providergolden.CheckToNative(t, "fromRecordConfig",
		func(_ *models.DomainConfig, records models.Records) (*record, error) {
			return fromRecordConfig(records[0])
		})
}
