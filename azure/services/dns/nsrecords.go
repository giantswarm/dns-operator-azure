package dns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/azure"
)

func (s *Service) calculateMissingNSRecords(ctx context.Context, currentRecordSets []dns.RecordSet) []azure.NSRecordSetSpec {
	desiredRecordSet := s.getDesiredNSRecords(currentRecordSets)

	var nsRecordsToCreate []azure.NSRecordSetSpec

	for _, nsRecordSet := range desiredRecordSet {
		for _, recordSet := range currentRecordSets {
			if recordSet.Type != nil && *recordSet.Type == RecordSetTypeNS &&
				recordSet.Name != nil && *recordSet.Name == nsRecordSet.Name {
				s.scope.V(2).Info(
					fmt.Sprintf("DNS NS record '%s' found", nsRecordSet.Name),
					"DNSZone", s.scope.ClusterZoneName,
					"name", nsRecordSet.Name,
				)
				continue
			}

			nsRecordsToCreate = append(nsRecordsToCreate, nsRecordSet)
			s.scope.V(2).Info(
				fmt.Sprintf("DNS NS record '%s' is missing, it will be created", nsRecordSet.Name),
				"DNSZone", s.scope.ClusterZoneName,
				"name", nsRecordSet.Name)
		}
	}

	return nsRecordsToCreate

}

func (s *Service) updateNSRecords(ctx context.Context, currentRecordSets []dns.RecordSet) error {
	recordsToCreate := s.calculateMissingNSRecords(ctx, currentRecordSets)

	zoneName := s.scope.BaseDomain()

	if len(recordsToCreate) == 0 {
		s.scope.V(2).Info(
			"All DNS NS records have already been created",
			"DNSZone", zoneName)
		return nil
	}

	for _, nsRecord := range recordsToCreate {
		var allNSDomainNames string
		var nsRecords []dns.NsRecord
		for i, nsdn := range nsRecord.NSDomainNames {
			if i > 0 {
				allNSDomainNames += ", " + nsdn.NSDomainName
			} else {
				allNSDomainNames += nsdn.NSDomainName
			}

			nsDomainName := nsdn.NSDomainName
			nsRecord := dns.NsRecord{
				Nsdname: &nsDomainName,
			}
			nsRecords = append(nsRecords, nsRecord)
		}

		s.scope.V(2).Info(
			"Creating DNS NS record",
			"DNSZone", zoneName,
			"name", nsRecord.Name,
			"NSDomainNames", allNSDomainNames)

		recordSet := dns.RecordSet{
			Type: to.StringPtr(string(dns.NS)),
			RecordSetProperties: &dns.RecordSetProperties{
				NsRecords: &nsRecords,
				TTL:       to.Int64Ptr(nsRecord.TTL),
			},
		}
		_, err := s.client.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			zoneName,
			dns.NS,
			nsRecord.Name,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.scope.V(2).Info(
			"Successfully created DNS NS record",
			"DNSZone", zoneName,
			"name", nsRecord.Name,
			"NSDomainNames", allNSDomainNames)
	}

	return nil
}

func (s *Service) getDesiredNSRecords(currentRecordSets []dns.RecordSet) []azure.NSRecordSetSpec {
	// We update only NS records, since NS records from workload cluster are required to create
	return filterAndGetNSRecordSetSpecs(currentRecordSets)
}
