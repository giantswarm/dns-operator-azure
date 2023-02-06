package dns

import (
	"context"
	"fmt"
	"net"
	"sort"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	// Latest capz controller still depends on this library
	// https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/v1.6.0/azure/services/publicips/client.go#L56
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-08-01/network" //nolint
	"github.com/Azure/go-autorest/autorest/to"

	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"golang.org/x/exp/slices"

	"github.com/giantswarm/dns-operator-azure/azure"
)

func (s *Service) calculateMissingARecords(ctx context.Context, logger logr.Logger, currentRecordSets []*armdns.RecordSet) []azure.ARecordSetSpec {
	desiredRecordSet := s.getDesiredARecords()

	var aRecordsToCreate []azure.ARecordSetSpec

	for _, desiredARecordSet := range desiredRecordSet {
		recordSetCreationNeeded := true
		for _, recordSet := range currentRecordSets {
			if recordSet.Type != nil && *recordSet.Type == RecordSetTypeA &&
				recordSet.Name != nil && *recordSet.Name == desiredARecordSet.Hostname {
				logger.Info(
					fmt.Sprintf("DNS A record '%s' found", desiredARecordSet.Hostname),
					"DNSZone", s.scope.ClusterDomain(),
					"hostname", desiredARecordSet.Hostname,
				)
				recordSetCreationNeeded = false
			}
		}

		if recordSetCreationNeeded {
			aRecordsToCreate = append(aRecordsToCreate, desiredARecordSet)
		}
	}

	// remove duplicate entries
	sort.SliceStable(aRecordsToCreate, func(i, j int) bool {
		return aRecordsToCreate[i].Hostname < aRecordsToCreate[j].Hostname
	})
	aRecordsToCreate = slices.Compact(aRecordsToCreate)

	return aRecordsToCreate
}

func (s *Service) updateARecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	logger := log.FromContext(ctx).WithName("arecords")
	recordsToCreate := s.calculateMissingARecords(ctx, logger, currentRecordSets)

	zoneName := s.scope.ClusterZoneName()

	if len(recordsToCreate) == 0 {
		logger.Info(
			"All DNS A records have already been created",
			"DNSZone", zoneName)
		return nil
	}

	for _, aRecord := range recordsToCreate {

		logger.Info(
			fmt.Sprintf("DNS A record %s is missing, it will be created", aRecord.Hostname),
			"DNSZone", zoneName,
			"FQDN", fmt.Sprintf("%s.%s", aRecord.Hostname, s.scope.ClusterDomain()))

		var ipAddress *string

		// if the IP isn't a valid IP it's an indicator
		// that we got a public DNS name to a public IP
		if net.ParseIP(aRecord.PublicIPName) == nil {
			ipAddressObject, err := s.publicIPsService.Get(ctx, &capzpublicips.PublicIPSpec{
				Name:          aRecord.PublicIPName,
				ResourceGroup: s.scope.ResourceGroup(),
			})
			if err != nil {
				return microerror.Mask(err)
			}

			ipAddress = ipAddressObject.(network.PublicIPAddress).IPAddress

			if ipAddress == nil {
				logger.Info(
					"Cannot create DNS A record, public Azure IP object does not have IP address set",
					"DNSZone", zoneName,
					"hostname", aRecord.Hostname,
					"IP resource name", aRecord.PublicIPName)
				continue
			}
		} else {
			ipAddress = to.StringPtr(aRecord.PublicIPName)
		}

		logger.Info(
			"Creating DNS A record",
			"DNSZone", zoneName,
			"hostname", aRecord.Hostname,
			"ipv4", *ipAddress)

		recordSet := armdns.RecordSet{
			Type: to.StringPtr(string(armdns.RecordTypeA)),
			Properties: &armdns.RecordSetProperties{
				ARecords: []*armdns.ARecord{
					{
						IPv4Address: ipAddress,
					},
				},
				TTL: to.Int64Ptr(aRecord.TTL),
			},
		}
		_, err := s.azureClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			s.scope.ClusterZoneName(),
			armdns.RecordTypeA,
			aRecord.Hostname,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		logger.Info(
			"Successfully created DNS A record",
			"DNSZone", zoneName,
			"hostname", aRecord.Hostname,
			"ipv4", aRecord.PublicIPName)
	}

	return nil
}

func (s *Service) getDesiredARecords() []azure.ARecordSetSpec {

	aRecordSetSpec := []azure.ARecordSetSpec{}

	switch {
	case s.scope.IsAPIServerPrivate():
		aRecordSetSpec = append(aRecordSetSpec, azure.ARecordSetSpec{
			Hostname:     "api",
			PublicIPName: s.scope.APIServerPrivateIP(),
			TTL:          3600,
		},
		)
	case !s.scope.IsAPIServerPrivate():
		aRecordSetSpec = append(aRecordSetSpec, azure.ARecordSetSpec{
			Hostname:     "api",
			PublicIPName: s.scope.APIServerPublicIP().Name,
			TTL:          3600,
		},
		)
	}

	return aRecordSetSpec
}
