package privatedns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/dns-operator-azure/v2/azure"
	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"
	"github.com/giantswarm/microerror"
	"sigs.k8s.io/controller-runtime/pkg/log"

	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
)

type Service struct {
	scope scope.PrivateDNSScope

	privateDNSClient Client
}

func New(scope scope.PrivateDNSScope) (*Service, error) {

	privateDNSClient, err := newPrivateDNSClient(scope)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &Service{
		scope:            scope,
		privateDNSClient: privateDNSClient,
	}, nil
}

func (s *Service) Reconcile(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("azure-private-dns-create")

	clusterZoneName := s.scope.ClusterZoneName()
	managementClusterResourceGroup := s.scope.GetManagementClusterResourceGroup()

	log.V(1).Info("mario/CURRENT_DEVELOPMENT - get zone", "clusterZoneName", clusterZoneName)

	privateClusterRecordSets, err := s.privateDNSClient.ListPrivateRecordSets(ctx, managementClusterResourceGroup, clusterZoneName)
	if err != nil && !azure.IsParentResourceNotFound(err) {
		log.V(1).Info("new error - mario/CURRENT_DEVELOPMENT", "error", err.Error())

		return microerror.Mask(err)
	} else if azure.IsParentResourceNotFound(err) {
		log.V(1).Info("mario/CURRENT_DEVELOPMENT - cluster specific private DNS zone not found", "error", err.Error())
		err = s.privateDNSClient.CreateOrUpdatePrivateZone(ctx, managementClusterResourceGroup, clusterZoneName, armprivatedns.PrivateZone{
			Name:     &clusterZoneName,
			Location: to.StringPtr(capzazure.Global),
		})
		if err != nil {
			log.V(1).Info("mario/CURRENT_DEVELOPMENT - private zone creation failed", "error", err.Error())
			return microerror.Mask(err)
		}
	}

	log.V(1).Info("mario/CURRENT_DEVELOPMENT - privateclusterRecords", "privateClusterRecordSets", privateClusterRecordSets)

	privateZones, err := s.privateDNSClient.GetPrivateZone(ctx, s.scope.GetManagementClusterResourceGroup(), clusterZoneName)
	if err != nil {
		log.V(1).Info("new error", "error", err.Error())
	}

	log.V(1).Info("mario/CURRENT_DEVELOPMENT", "privateZones", privateZones)

	if err := s.updateARecords(ctx, privateClusterRecordSets); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (s *Service) ReconcileDelete(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("azure-private-dns-delete")
	clusterZoneName := s.scope.ClusterZoneName()
	log.Info("Reconcile DNS deletion", "privateDNSZone", clusterZoneName)

	if err := s.privateDNSClient.DeletePrivateZone(ctx, s.scope.GetManagementClusterResourceGroup(), clusterZoneName); err != nil {
		return microerror.Mask(err)
	}

	log.Info("Successfully reconciled DNS", "privateDNSZone", clusterZoneName)

	return nil

}
