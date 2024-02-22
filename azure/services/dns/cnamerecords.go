package dns

import (
	"context"
	"fmt"
	"reflect"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	cnameRecordTTL = 300
)

func (s *Service) updateCnameRecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	logger := log.FromContext(ctx).WithName("cnamerecords")

	logger.V(1).Info("update CNAME records", "current record sets", currentRecordSets)

	recordsToCreate := s.calculateMissingCnameRecords(logger, currentRecordSets)

	if len(recordsToCreate) == 0 {
		logger.Info(
			"All DNS A records have already been created",
			"DNSZone", s.scope.ClusterDomain())
		return nil
	}

	for _, cnameRecord := range recordsToCreate {
		logger.Info(
			fmt.Sprintf("DNS CNAME record %s is missing, it will be created", *cnameRecord.Name),
			"DNSZone", s.scope.ClusterDomain(),
			"FQDN", fmt.Sprintf("%s.%s", *cnameRecord.Name, s.scope.ClusterDomain()))

		logger.Info(
			"Creating DNS A record",
			"DNSZone", s.scope.ClusterDomain(),
			"name", cnameRecord.Name,
			"value", cnameRecord.Properties.CnameRecord)

		createdRecordSet, err := s.azureClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			s.scope.ClusterDomain(),
			armdns.RecordTypeCNAME,
			*cnameRecord.Name,
			*cnameRecord)
		if err != nil {
			return err
		}

		logger.Info(
			"Successfully created DNS A record",
			"DNSZone", s.scope.ClusterDomain(),
			"hostname", cnameRecord.Name,
			"id", createdRecordSet.ID)
	}

	return nil
}

func (s *Service) calculateMissingCnameRecords(logger logr.Logger, currentRecordSets []*armdns.RecordSet) []*armdns.RecordSet {

	clusterZoneName := s.scope.ClusterDomain()
	desiredRecords := desiredCnameRecords(clusterZoneName)

	var recordsToCreate []*armdns.RecordSet

	for _, desiredRecordSet := range desiredRecords {

		logger.V(1).Info(fmt.Sprintf("compare entries individually - %s", *desiredRecordSet.Name))

		currentRecordSetIndex := slices.IndexFunc(currentRecordSets, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == *desiredRecordSet.Name })
		if currentRecordSetIndex < 0 {
			recordsToCreate = append(recordsToCreate, desiredRecordSet)
		} else {
			currentRecordSet := currentRecordSets[currentRecordSetIndex]
			switch {
			case !reflect.DeepEqual(currentRecordSet.Properties.CnameRecord, desiredRecordSet.Properties.CnameRecord):
				logger.V(1).Info(fmt.Sprintf("A Records for %s are not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)
			case !reflect.DeepEqual(currentRecordSet.Properties.TTL, desiredRecordSet.Properties.TTL):
				logger.V(1).Info(fmt.Sprintf("TTL for %s is not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)
			}
		}
	}

	return recordsToCreate
}

func desiredCnameRecords(clusterZoneName string) []*armdns.RecordSet {
	return []*armdns.RecordSet{
		{
			Name: pointer.String("*"),
			Type: pointer.String(string(armdns.RecordTypeCNAME)),
			Properties: &armdns.RecordSetProperties{
				TTL: pointer.Int64(cnameRecordTTL),
				CnameRecord: &armdns.CnameRecord{
					Cname: pointer.String(fmt.Sprintf("ingress.%s", clusterZoneName)),
				},
			},
		},
	}
}
