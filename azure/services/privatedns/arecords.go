package privatedns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"golang.org/x/exp/slices"

	// Latest capz controller still depends on this library
	// https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/v1.6.0/azure/services/publicips/client.go#L56
	//nolint
	"github.com/Azure/go-autorest/autorest/to"

	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	bastionRecordName   = "bastion"
	bastion1RecordName  = "bastion1"
	apiRecordName       = "api"
	apiserverRecordName = "apiserver"

	apiRecordTTL     = 300
	bastionRecordTTL = 300
)

func (s *Service) calculateMissingARecords(ctx context.Context, logger logr.Logger, currentRecordSets []*armprivatedns.RecordSet) ([]*armprivatedns.RecordSet, error) {
	desiredRecordSet, err := s.getDesiredPrivateARecords(ctx)
	if err != nil {
		return nil, err
	}

	return desiredRecordSet, nil
}

func (s *Service) updateARecords(ctx context.Context, currentRecordSets []*armprivatedns.RecordSet) error {

	logger := log.FromContext(ctx).WithName("private-records")

	logger.V(1).Info("update A records", "current record sets", currentRecordSets)

	recordsToCreate, err := s.calculateMissingARecords(ctx, logger, currentRecordSets)
	if err != nil {
		return err
	}

	logger.V(1).Info("update A records", "records to create", recordsToCreate)

	if len(recordsToCreate) == 0 {
		logger.Info(
			"All DNS A records have already been created",
			"DNSZone", s.scope.ClusterZoneName())
		return nil
	}

	for _, aRecord := range recordsToCreate {

		logger.Info(
			fmt.Sprintf("DNS A record %s is missing, it will be created", to.String(aRecord.Name)),
			"DNSZone", s.scope.ClusterZoneName(),
			"FQDN", fmt.Sprintf("%s.%s", *aRecord.Name, s.scope.ClusterZoneName()))

		logger.Info(
			"Creating DNS A record",
			"DNSZone", s.scope.ClusterZoneName(),
			"hostname", aRecord.Name,
			"ipv4", aRecord.Properties.ARecords)

		createdRecordSet, err := s.privateDNSClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.GetManagementClusterResourceGroup(),
			s.scope.ClusterZoneName(),
			armprivatedns.RecordTypeA,
			*aRecord.Name,
			*aRecord)
		if err != nil {
			return microerror.Mask(err)
		}

		logger.Info(
			"Successfully created DNS A record",
			"DNSZone", s.scope.ClusterZoneName(),
			"hostname", aRecord.Name,
			"id", createdRecordSet.ID)
	}

	return nil
}

func (s *Service) getDesiredPrivateARecords(ctx context.Context) ([]*armprivatedns.RecordSet, error) {

	var armprivatednsRecordSet []*armprivatedns.RecordSet

	if len(s.scope.GetPrivateLinkedAPIServerIP()) > 0 {

		armprivatednsRecordSet = append(armprivatednsRecordSet,

			&armprivatedns.RecordSet{
				Name: to.StringPtr(apiserverRecordName),
				Type: to.StringPtr(string(armprivatedns.RecordTypeA)),
				Properties: &armprivatedns.RecordSetProperties{
					TTL: to.Int64Ptr(apiRecordTTL),
				},
			},
		)

		privateAPIIndex := slices.IndexFunc(armprivatednsRecordSet, func(recordSet *armprivatedns.RecordSet) bool { return *recordSet.Name == apiserverRecordName })

		armprivatednsRecordSet[privateAPIIndex].Properties.ARecords = append(armprivatednsRecordSet[privateAPIIndex].Properties.ARecords, &armprivatedns.ARecord{
			IPv4Address: to.StringPtr(s.scope.GetPrivateLinkedAPIServerIP()),
		})

	}

	return armprivatednsRecordSet, nil
}
