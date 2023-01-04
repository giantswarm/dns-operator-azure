package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
)

// func (s *Service) calculateMissingNSRecords(ctx context.Context, currentRecordSets []dns.RecordSet) []azure.NSRecordSetSpec {
// 	desiredRecordSet := s.getDesiredNSRecords(currentRecordSets)

// 	var nsRecordsToCreate []azure.NSRecordSetSpec

// 	for _, nsRecordSet := range desiredRecordSet {
// 		for _, recordSet := range currentRecordSets {
// 			if recordSet.Type != nil && *recordSet.Type == RecordSetTypeNS &&
// 				recordSet.Name != nil && *recordSet.Name == nsRecordSet.Name {
// 				s.scope.V(2).Info(
// 					fmt.Sprintf("DNS NS record '%s' found", nsRecordSet.Name),
// 					"DNSZone", s.scope.ClusterZoneName,
// 					"name", nsRecordSet.Name,
// 				)
// 				continue
// 			}

// 			nsRecordsToCreate = append(nsRecordsToCreate, nsRecordSet)
// 			s.scope.V(2).Info(
// 				fmt.Sprintf("DNS NS record '%s' is missing, it will be created", nsRecordSet.Name),
// 				"DNSZone", s.scope.ClusterZoneName,
// 				"name", nsRecordSet.Name)
// 		}
// 	}

// 	return nsRecordsToCreate

// }

func (s *Service) updateNSRecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	recordsToCreate := filterAndGetNSRecords(currentRecordSets)
	// recordsToCreate := s.calculateMissingNSRecords(ctx, currentRecordSets)

	zoneName := s.scope.BaseDomain()

	if len(recordsToCreate) == 0 {
		s.scope.V(2).Info(
			"All DNS NS records have already been created",
			"DNSZone", zoneName)
		return nil
	}

	for _, nsRecord := range recordsToCreate {
		var allNSDomainNames string
		var nsRecords []*armdns.NsRecord
		for i, nsdn := range nsRecord.Properties.NsRecords {
			if i > 0 {
				allNSDomainNames += ", " + *nsdn.Nsdname
			} else {
				allNSDomainNames += *nsdn.Nsdname
			}

			nsDomainName := *nsdn.Nsdname
			nsRecord := &armdns.NsRecord{
				Nsdname: &nsDomainName,
			}
			nsRecords = append(nsRecords, nsRecord)
		}

		s.scope.V(2).Info(
			"Creating DNS NS record",
			"DNSZone", zoneName,
			"name", nsRecord.Name,
			"NSDomainNames", allNSDomainNames)

		recordSet := armdns.RecordSet{
			Type: to.StringPtr(string(dns.NS)),
			Properties: &armdns.RecordSetProperties{
				NsRecords: nsRecords,
				TTL:       nsRecord.Properties.TTL,
			},
		}
		_, err := s.azureBaseZoneClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			zoneName,
			armdns.RecordTypeNS,
			*nsRecord.Name,
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

// func (s *Service) getDesiredNSRecords(currentRecordSets []dns.RecordSet) []azure.NSRecordSetSpec {
// 	// We update only NS records, since NS records from workload cluster are required to create
// 	return filterAndGetNSRecordSetSpecs(currentRecordSets)
// }

func (s *Service) deleteNSRecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	recordsToDelete := filterAndGetNSRecords(currentRecordSets)
	zoneName := s.scope.BaseDomain()

	for _, nsRecord := range recordsToDelete {
		s.scope.V(2).Info(
			"Delete DNS NS record",
			"DNSZone", zoneName,
			"name", nsRecord.Name,
		)

		if err := s.azureBaseZoneClient.DeleteRecordSet(ctx, s.scope.ResourceGroup(), zoneName, armdns.RecordTypeNS, *nsRecord.Name); err != nil {
			s.scope.V(2).Info("DNS zone not found",
				"DNSZone", zoneName,
				"error", err.Error(),
			)
		} else if capzazure.ResourceNotFound(err) {
			s.scope.V(2).Info("Azure NS record not found",
				"DNSZone", zoneName,
				"NSRecord", nsRecord.Name,
				"error", err.Error(),
			)
		} else if err != nil {
			return microerror.Mask(err)
		}

		s.scope.V(2).Info(
			"Successfully deleted DNS NS record",
			"DNSZone", zoneName,
			"name", nsRecord.Name,
		)
	}

	return nil
}
