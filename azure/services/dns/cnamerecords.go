package dns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/azure"
)

func (s *Service) calculateMissingCNameRecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) []azure.CNameRecordSetSpec {
	desiredRecordSet := s.getDesiredCNameRecords()

	var cnameRecordsToCreate []azure.CNameRecordSetSpec

	for _, desiredCName := range desiredRecordSet {
		for _, recordSet := range currentRecordSets {
			if recordSet.Type != nil && *recordSet.Type == RecordSetTypeCNAME &&
				recordSet.Name != nil && *recordSet.Name == desiredCName.Alias {
				s.scope.V(2).Info(
					fmt.Sprintf("DNS CNAME record '%s' found", desiredCName.Alias),
					"DNSZone", s.scope.ClusterZoneName(),
					"alias", desiredCName.Alias,
					"cname", desiredCName.CName,
				)
				continue
			}

			cnameRecordsToCreate = append(cnameRecordsToCreate, desiredCName)
			s.scope.V(2).Info(
				fmt.Sprintf("DNS CNAME record '%s' is missing, it will be created", desiredCName.Alias),
				"DNSZone", s.scope.ClusterZoneName(),
				"alias", desiredCName.Alias,
				"cname", desiredCName.CName)
		}
	}

	return cnameRecordsToCreate

}

func (s *Service) updateCNameRecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	recordsToCreate := s.calculateMissingCNameRecords(ctx, currentRecordSets)

	zoneName := s.scope.ClusterZoneName()

	if len(recordsToCreate) == 0 {
		s.scope.V(2).Info(
			"All DNS CNAME records have already been created",
			"DNSZone", zoneName)
		return nil
	}

	for _, cnameRecord := range recordsToCreate {
		s.scope.V(2).Info(
			"Creating DNS CNAME record",
			"DNSZone", zoneName,
			"alias", cnameRecord.Alias,
			"cname", cnameRecord.CName)

		recordSet := armdns.RecordSet{
			Type: to.StringPtr(string(dns.CNAME)),
			Properties: &armdns.RecordSetProperties{
				CnameRecord: &armdns.CnameRecord{
					Cname: to.StringPtr(cnameRecord.CName),
				},
				TTL: to.Int64Ptr(cnameRecord.TTL),
			},
		}
		_, err := s.azureClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			zoneName,
			armdns.RecordTypeCNAME,
			cnameRecord.Alias,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.scope.V(2).Info(
			"Successfully created DNS CNAME record",
			"DNSZone", zoneName,
			"alias", cnameRecord.Alias,
			"cname", cnameRecord.CName)
	}

	return nil
}

func (s *Service) getDesiredCNameRecords() []azure.CNameRecordSetSpec {
	return []azure.CNameRecordSetSpec{
		{
			Alias: "*",
			CName: fmt.Sprintf("ingress.%s", s.scope.ClusterZoneName()),
			TTL:   3600,
		},
	}
}
