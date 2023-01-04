package dns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"

	"github.com/giantswarm/dns-operator-azure/azure"
)

func (s *Service) calculateMissingARecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) []azure.ARecordSetSpec {
	desiredRecordSet := s.getDesiredARecords()

	var aRecordsToCreate []azure.ARecordSetSpec

	for _, desiredARecordSet := range desiredRecordSet {
		for _, recordSet := range currentRecordSets {
			if recordSet.Type != nil && *recordSet.Type == RecordSetTypeA &&
				recordSet.Name != nil && *recordSet.Name == desiredARecordSet.Hostname {
				s.scope.V(2).Info(
					fmt.Sprintf("DNS A record '%s' found", desiredARecordSet.Hostname),
					"DNSZone", s.scope.ClusterDomain(),
					"hostname", desiredARecordSet.Hostname,
				)
				continue
			}

			aRecordsToCreate = append(aRecordsToCreate, desiredARecordSet)
			s.scope.V(2).Info(
				fmt.Sprintf("DNS A record '%s' is missing, it will be created", desiredARecordSet.Hostname),
				"DNSZone", "",
				"hostname", desiredARecordSet.Hostname)
		}

	}

	return aRecordsToCreate

}

func (s *Service) updateARecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	recordsToCreate := s.calculateMissingARecords(ctx, currentRecordSets)

	zoneName := s.scope.ClusterZoneName()

	if len(recordsToCreate) == 0 {
		s.scope.V(2).Info(
			"All DNS A records have already been created",
			"DNSZone", zoneName)
		return nil
	}

	for _, aRecord := range recordsToCreate {
		ipAddressObject, err := s.publicIPsService.Get(ctx, s.scope.ResourceGroup(), aRecord.PublicIPName)
		if capzazure.ResourceNotFound(err) {
			s.scope.V(2).Info(
				"Cannot create DNS A record, public IP still not deployed",
				"DNSZone", zoneName,
				"hostname", aRecord.Hostname,
				"IP resource name", aRecord.PublicIPName)
			continue
		} else if err != nil {
			return microerror.Mask(err)
		}

		if ipAddressObject.IPAddress == nil {
			s.scope.V(2).Info(
				"Cannot create DNS A record, public Azure IP object does not have IP address set",
				"DNSZone", zoneName,
				"hostname", aRecord.Hostname,
				"IP resource name", aRecord.PublicIPName)
			continue
		}

		s.scope.V(2).Info(
			"Creating DNS A record",
			"DNSZone", zoneName,
			"hostname", aRecord.Hostname,
			"ipv4", *ipAddressObject.IPAddress)

		recordSet := armdns.RecordSet{
			Type: to.StringPtr(string(armdns.RecordTypeA)),
			Properties: &armdns.RecordSetProperties{
				ARecords: []*armdns.ARecord{
					{
						IPv4Address: ipAddressObject.IPAddress,
					},
				},
				TTL: to.Int64Ptr(aRecord.TTL),
			},
		}
		_, err = s.azureClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			s.scope.ClusterZoneName(),
			armdns.RecordTypeA,
			aRecord.Hostname,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.scope.V(2).Info(
			"Successfully created DNS A record",
			"DNSZone", zoneName,
			"hostname", aRecord.Hostname,
			"ipv4", aRecord.PublicIPName)
	}

	return nil
}

func (s *Service) getDesiredARecords() []azure.ARecordSetSpec {
	return []azure.ARecordSetSpec{
		{
			Hostname:     "api",
			PublicIPName: s.scope.APIServerPublicIP().Name,
			TTL:          3600,
		},
	}
}
