package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"k8s.io/utils/pointer"
)

const (
	zoneRecordTTL = 3600
)

func (s *Service) deleteClusterNSRecords(ctx context.Context) error {

	err := s.azureBaseZoneClient.DeleteRecordSet(
		ctx,
		s.scope.BaseDomainResourceGroup(),
		s.scope.BaseDomain(),
		armdns.RecordTypeNS,
		s.scope.Patcher.ClusterName(),
	)
	if err != nil {
		return err
	}

	return nil
}

// createClusterNSRecord create a NS record in the basedomain
// for zone delegation
func (s *Service) createClusterNSRecord(ctx context.Context, nameServerRecords []*armdns.NsRecord) error {

	_, err := s.azureBaseZoneClient.CreateOrUpdateRecordSet(
		ctx,
		s.scope.BaseDomainResourceGroup(),
		s.scope.BaseDomain(),
		armdns.RecordTypeNS,
		s.scope.Patcher.ClusterName(),
		armdns.RecordSet{
			Properties: &armdns.RecordSetProperties{
				TTL:       pointer.Int64(zoneRecordTTL),
				NsRecords: nameServerRecords,
			},
		},
	)
	if err != nil {
		return err
	}

	return nil
}
