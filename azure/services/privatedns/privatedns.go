package privatedns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"golang.org/x/exp/slices"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/v3/azure"
	"github.com/giantswarm/dns-operator-azure/v3/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v3/pkg/metrics"

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

	clusterZoneName := s.scope.ClusterDomain()
	managementClusterResourceGroup := s.scope.ManagementClusterResourceGroup()

	log.Info("Reconcile privateDNS", "privateDNSZone", clusterZoneName)

	metrics.ZoneInfo.WithLabelValues(
		clusterZoneName,                           // label: zone
		metrics.ZoneTypePrivate,                   // label: type
		managementClusterResourceGroup,            // label: resource_group
		s.scope.ManagementClusterTenantID(),       // label: tenant_id
		s.scope.ManagementClusterSubscriptionID(), // label: subscription_id
	).Set(1)

	privateClusterRecordSets, err := s.privateDNSClient.ListPrivateRecordSets(ctx, managementClusterResourceGroup, clusterZoneName)
	if err != nil && !azure.IsParentResourceNotFound(err) {
		return microerror.Mask(err)
	} else if azure.IsParentResourceNotFound(err) {
		log.V(1).Info("cluster specific private DNS zone not found, creating a new one")
		err = s.privateDNSClient.CreateOrUpdatePrivateZone(ctx, managementClusterResourceGroup, clusterZoneName, armprivatedns.PrivateZone{
			Name:     &clusterZoneName,
			Location: pointer.String(capzazure.Global),
		})
		if err != nil {
			return microerror.Mask(err)
		}
	}

	log.Info("list virtualNetworkLinks")
	networkLinks, err := s.privateDNSClient.ListVirtualNetworkLink(ctx, managementClusterResourceGroup, clusterZoneName)
	if err != nil {
		return microerror.Mask(err)
	}
	log.V(1).Info("list of all network links", "virtualNetworkLinks", networkLinks)

	vnetLinkName := virtualNetworkLinkName(managementClusterResourceGroup)
	operatorGeneratedVirtualNetworkLinkIndex := slices.IndexFunc(networkLinks, func(virtualNetworkLink *armprivatedns.VirtualNetworkLink) bool {
		return *virtualNetworkLink.Name == *pointer.String(vnetLinkName) ||
			*virtualNetworkLink.ID == s.scope.ManagementClusterClientID()
	})

	if operatorGeneratedVirtualNetworkLinkIndex == -1 {
		// Check if there is an existing link to the virtual network
		existingVirtualNetworkLinkIndex := slices.IndexFunc(networkLinks, func(virtualNetworkLink *armprivatedns.VirtualNetworkLink) bool {
			return *virtualNetworkLink.Properties.VirtualNetwork.ID == s.scope.ManagementClusterVnetID()
		})
		if existingVirtualNetworkLinkIndex >= 0 {
			log.V(1).Info("found a link to the same virtual network with a different name, deleting it")

			existingVirtualNetworkLink := networkLinks[existingVirtualNetworkLinkIndex]
			err = s.privateDNSClient.DeleteVirtualNetworkLink(ctx, managementClusterResourceGroup, clusterZoneName, *existingVirtualNetworkLink.Name)
			if err != nil {
				return microerror.Mask(err)
			}
		}

		log.V(1).Info("virtual network link not found, creating a new one")

		err = s.privateDNSClient.CreateOrUpdateVirtualNetworkLink(
			ctx,
			managementClusterResourceGroup,
			clusterZoneName,
			s.scope.ClusterName(),
			s.scope.ManagementClusterVnetID(),
			vnetLinkName,
		)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	log.V(1).Info("get privateDNSZone Object", "privateDNSZone", clusterZoneName)
	privateZones, err := s.privateDNSClient.GetPrivateZone(ctx, s.scope.ManagementClusterResourceGroup(), clusterZoneName)
	if err != nil {
		log.V(1).Info("new error", "error", err.Error())
	}

	// dns_operator_zone_records_sum
	metrics.ClusterZoneRecords.WithLabelValues(
		clusterZoneName,
		metrics.ZoneTypePrivate,
	).Set(float64(*privateZones.Properties.NumberOfRecordSets))

	log.V(1).Info("current known private Zones in management cluster", "privateZones", privateZones)

	if err := s.updateARecords(ctx, privateClusterRecordSets); err != nil {
		return microerror.Mask(err)
	}

	if err = s.updateCnameRecords(ctx, privateClusterRecordSets); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (s *Service) ReconcileDelete(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("azure-private-dns-delete")
	clusterZoneName := s.scope.ClusterDomain()
	log.Info("Reconcile DNS deletion", "privateDNSZone", clusterZoneName)

	mcResourceGroup := s.scope.ManagementClusterResourceGroup()
	vnetLinkName := virtualNetworkLinkName(s.scope.ManagementClusterResourceGroup())
	if err := s.privateDNSClient.DeleteVirtualNetworkLink(ctx, mcResourceGroup, clusterZoneName, vnetLinkName); err != nil {
		return microerror.Mask(err)
	}

	if err := s.privateDNSClient.DeletePrivateZone(ctx, s.scope.ManagementClusterResourceGroup(), clusterZoneName); err != nil {
		return microerror.Mask(err)
	}

	log.Info("Successfully reconciled DNS", "privateDNSZone", clusterZoneName)

	return nil
}
