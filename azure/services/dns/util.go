package dns

import (
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
)

func filterAndGetNSRecords(records []*armdns.RecordSet) []*armdns.RecordSet {
	return filterRecords(records, RecordSetTypeNS)
}

func filterRecords(currentRecordSets []*armdns.RecordSet, recordType string) []*armdns.RecordSet {
	var filteredRecords []*armdns.RecordSet

	for _, recordSet := range currentRecordSets {
		isDesiredType := recordSet.Type != nil && *recordSet.Type == recordType
		if !isDesiredType {
			continue
		}

		filteredRecords = append(filteredRecords, recordSet)
	}

	return filteredRecords
}
