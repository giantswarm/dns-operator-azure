package privatedns

import (
	"context"
	"fmt"
	"reflect"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"golang.org/x/exp/slices"
	"k8s.io/utils/pointer"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/v3/pkg/metrics"
)

const (
	apiserverRecordName = "apiserver"
	mcIngressRecordName = "ingress"

	apiRecordTTL = 300
)

func (s *Service) updateARecords(ctx context.Context, currentRecordSets []*armprivatedns.RecordSet) error {

	logger := log.FromContext(ctx).WithName("private-records")

	logger.V(1).Info("update A records", "current record sets", currentRecordSets)

	recordsToCreate := s.calculateMissingARecords(ctx, logger, currentRecordSets)

	logger.V(1).Info("update A records", "records to create", recordsToCreate)

	if len(recordsToCreate) == 0 {
		logger.Info(
			"All DNS A records have already been created",
			"DNSZone", s.scope.ClusterDomain())
		return nil
	}

	for _, aRecord := range recordsToCreate {

		logger.Info(
			fmt.Sprintf("DNS A record %s is missing, it will be created", *aRecord.Name),
			"DNSZone", s.scope.ClusterDomain(),
			"FQDN", fmt.Sprintf("%s.%s", *aRecord.Name, s.scope.ClusterDomain()))

		logger.Info(
			"Creating DNS A record",
			"DNSZone", s.scope.ClusterDomain(),
			"hostname", aRecord.Name,
			"ipv4", aRecord.Properties.ARecords)

		createdRecordSet, err := s.privateDNSClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ManagementClusterResourceGroup(),
			s.scope.ClusterDomain(),
			armprivatedns.RecordTypeA,
			*aRecord.Name,
			*aRecord)
		if err != nil {
			return microerror.Mask(err)
		}

		logger.Info(
			"Successfully created DNS A record",
			"DNSZone", s.scope.ClusterDomain(),
			"hostname", aRecord.Name,
			"id", createdRecordSet.ID)
	}

	return nil
}

func (s *Service) calculateMissingARecords(ctx context.Context, logger logr.Logger, currentRecordSets []*armprivatedns.RecordSet) []*armprivatedns.RecordSet {
	desiredRecordSets := s.getDesiredPrivateARecords(ctx)

	var recordsToCreate []*armprivatedns.RecordSet

	for _, desiredRecordSet := range desiredRecordSets {
		logger.V(1).Info(fmt.Sprintf("compare entries individually - %s", *desiredRecordSet.Name))

		currentRecordSetIndex := slices.IndexFunc(currentRecordSets, func(recordSet *armprivatedns.RecordSet) bool { return *recordSet.Name == *desiredRecordSet.Name })
		if currentRecordSetIndex == -1 {
			recordsToCreate = append(recordsToCreate, desiredRecordSet)
		} else {
			// compare ARecords[].IPv4Address
			switch {
			case !reflect.DeepEqual(
				desiredRecordSet.Properties.ARecords,
				currentRecordSets[currentRecordSetIndex].Properties.ARecords,
			):
				logger.V(1).Info(fmt.Sprintf("A Records for %s are not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)
			case !reflect.DeepEqual(
				desiredRecordSet.Properties.TTL,
				currentRecordSets[currentRecordSetIndex].Properties.TTL,
			):
				logger.V(1).Info(fmt.Sprintf("TTL for %s is not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)
			}

			for _, ip := range currentRecordSets[currentRecordSetIndex].Properties.ARecords {
				// dns_operator_azure_record_set_info{controller="dns-operator-azure",fqdn="api.glippy.azuretest.gigantic.io",ip="20.4.101.180",ttl="300",type="private"} 1
				metrics.RecordInfo.WithLabelValues(
					s.scope.ClusterDomain(), // label: zone
					metrics.ZoneTypePrivate, // label: type
					fmt.Sprintf("%s.%s", *currentRecordSets[currentRecordSetIndex].Name, s.scope.ClusterDomain()), // label: fqdn
					*ip.IPv4Address, // label: ip
					fmt.Sprint(*currentRecordSets[currentRecordSetIndex].Properties.TTL), // label: ttl
				).Set(1)
			}
		}
	}

	return recordsToCreate
}

func (s *Service) getDesiredPrivateARecords(ctx context.Context) []*armprivatedns.RecordSet {

	var armprivatednsRecordSet []*armprivatedns.RecordSet

	if len(s.scope.PrivateLinkedAPIServerIP()) > 0 {

		armprivatednsRecordSet = append(armprivatednsRecordSet,

			&armprivatedns.RecordSet{
				Name: pointer.String(apiserverRecordName),
				Type: pointer.String(string(armprivatedns.RecordTypeA)),
				Properties: &armprivatedns.RecordSetProperties{
					TTL: pointer.Int64(apiRecordTTL),
				},
			},
		)

		privateAPIIndex := slices.IndexFunc(armprivatednsRecordSet, func(recordSet *armprivatedns.RecordSet) bool { return *recordSet.Name == apiserverRecordName })

		armprivatednsRecordSet[privateAPIIndex].Properties.ARecords = append(armprivatednsRecordSet[privateAPIIndex].Properties.ARecords, &armprivatedns.ARecord{
			IPv4Address: pointer.String(s.scope.PrivateLinkedAPIServerIP()),
		})

	}

	if len(s.scope.PrivateLinkedMcIngressIP()) > 0 {

		armprivatednsRecordSet = append(armprivatednsRecordSet,

			&armprivatedns.RecordSet{
				Name: pointer.String(mcIngressRecordName),
				Type: pointer.String(string(armprivatedns.RecordTypeA)),
				Properties: &armprivatedns.RecordSetProperties{
					TTL: pointer.Int64(apiRecordTTL),
				},
			},
		)

		privateAPIIndex := slices.IndexFunc(armprivatednsRecordSet, func(recordSet *armprivatedns.RecordSet) bool { return *recordSet.Name == mcIngressRecordName })

		armprivatednsRecordSet[privateAPIIndex].Properties.ARecords = append(armprivatednsRecordSet[privateAPIIndex].Properties.ARecords, &armprivatedns.ARecord{
			IPv4Address: pointer.String(s.scope.PrivateLinkedMcIngressIP()),
		})

	}

	return armprivatednsRecordSet
}
