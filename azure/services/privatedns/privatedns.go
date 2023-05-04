package privatedns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/dns-operator-azure/v2/azure"
	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"

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

	log.Info("Reconcile privateDNS", "privateDNSZone", clusterZoneName)

	privateClusterRecordSets, err := s.privateDNSClient.ListPrivateRecordSets(ctx, managementClusterResourceGroup, clusterZoneName)
	if err != nil && !azure.IsParentResourceNotFound(err) {
		return microerror.Mask(err)
	} else if azure.IsParentResourceNotFound(err) {
		log.V(1).Info("cluster specific private DNS zone not found, creating a new one")
		err = s.privateDNSClient.CreateOrUpdatePrivateZone(ctx, managementClusterResourceGroup, clusterZoneName, armprivatedns.PrivateZone{
			Name:     &clusterZoneName,
			Location: to.StringPtr(capzazure.Global),
		})
		if err != nil {
			return microerror.Mask(err)
		}

		// TODO: move this into an on condition to be independent of the IsParentResourceNotFound
		log.V(1).Info("cluster specific private DNS zone not found, creating a new one")
		err = s.privateDNSClient.CreateOrUpdateVirtualNetworkLink(
			ctx,
			managementClusterResourceGroup,
			clusterZoneName,
			s.scope.ClusterName(),
			s.scope.GetManagementClusterVnetID(),
		)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// TODO: check what's to do with the privateZones here - monitoring?
	log.V(1).Info("get privateDNSZone Object", "privateDNSZone", clusterZoneName)
	privateZones, err := s.privateDNSClient.GetPrivateZone(ctx, s.scope.GetManagementClusterResourceGroup(), clusterZoneName)
	if err != nil {
		log.V(1).Info("new error", "error", err.Error())
	}

	log.V(1).Info("current known private Zones in management cluster", "privateZones", privateZones)

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
