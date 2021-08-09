package dns

import (
	azuredns "github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"

	"github.com/giantswarm/dns-operator-azure/azure"
)

func filterAndGetNSRecordSetSpecs(records []azuredns.RecordSet) []azure.NSRecordSetSpec {
	var nsRecordsSpecs []azure.NSRecordSetSpec
	for _, nsRecord := range filterRecords(records, RecordSetTypeNS) {
		if nsRecord.Name == nil {
			continue
		}

		var nsDomainNameSpecs []azure.NSDomainNameSpec
		if nsRecord.NsRecords != nil {
			for _, nsDomainName := range *nsRecord.NsRecords {
				if nsDomainName.Nsdname == nil {
					continue
				}
				nsDomainNameSpec := azure.NSDomainNameSpec{
					NSDomainName: *nsDomainName.Nsdname,
				}
				nsDomainNameSpecs = append(nsDomainNameSpecs, nsDomainNameSpec)
			}
		}

		var ttl int64
		if nsRecord.TTL != nil {
			ttl = *nsRecord.TTL
		}
		nsRecordSpec := azure.NSRecordSetSpec{
			Name:          *nsRecord.Name,
			NSDomainNames: nsDomainNameSpecs,
			TTL:           ttl,
		}

		nsRecordsSpecs = append(nsRecordsSpecs, nsRecordSpec)
	}

	return nsRecordsSpecs
}

func filterRecords(currentRecordSets []azuredns.RecordSet, recordType string) []azuredns.RecordSet {
	var filteredRecords []azuredns.RecordSet

	for _, recordSet := range currentRecordSets {
		isDesiredType := recordSet.Type != nil && *recordSet.Type == recordType
		if !isDesiredType {
			continue
		}

		filteredRecords = append(filteredRecords, recordSet)
	}

	return filteredRecords
}
