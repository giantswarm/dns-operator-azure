package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	azuredns "github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"

	"github.com/giantswarm/dns-operator-azure/azure"
	"github.com/giantswarm/dns-operator-azure/azure/scope"
)

const (
	RecordSetTypePrefix = "Microsoft.Network/dnszones/"
	RecordSetTypeA      = RecordSetTypePrefix + string(azuredns.A)
	RecordSetTypeCNAME  = RecordSetTypePrefix + string(azuredns.CNAME)
	RecordSetTypeNS     = RecordSetTypePrefix + string(azuredns.NS)
)

// Service provides operations on Azure resources.
type Service struct {
	scope               scope.DNSScope
	azureClient         client
	azureBaseZoneClient client

	publicIPsService *capzpublicips.Service
}

// New creates a new dns service.
func New(scope scope.DNSScope, publicIPsService *capzpublicips.Service) (*Service, error) {
	azureClient, err := newAzureClient()

	if err != nil {
		return nil, microerror.Mask(err)
	}

	azureBaseZoneClient, err := newBaseZoneClient(scope.BaseZoneCredentials())
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &Service{
		scope:               scope,
		azureClient:         azureClient,
		azureBaseZoneClient: azureBaseZoneClient,
		publicIPsService:    publicIPsService,
	}, nil
}

// Reconcile creates or updates the DNS zone, and creates DNS A and CNAME records.
func (s *Service) Reconcile(ctx context.Context) error {
	clusterZoneName := s.scope.ClusterDomain()
	s.scope.Info("Reconcile DNS", "DNSZone", clusterZoneName)

	currentRecordSets, err := s.azureClient.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		s.scope.V(2).Info("DNS zone not found", "DNSZone", clusterZoneName)

		_, rErr := s.createClusterDNSZone(ctx)
		if rErr != nil {
			return microerror.Mask(err)
		}

		currentRecordSets, err = s.azureClient.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
		if err != nil {
			return microerror.Mask(err)
		}

	} else if err != nil {
		return microerror.Mask(err)
	}

	// Create required A records.
	err = s.updateARecords(ctx, currentRecordSets)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create required CName records.
	err = s.updateCNameRecords(ctx, currentRecordSets)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create required NS records.
	err = s.updateNSRecords(ctx, currentRecordSets)
	if err != nil {
		return microerror.Mask(err)
	}

	s.scope.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

func (s *Service) ReconcileDelete(ctx context.Context) error {
	clusterZoneName := s.scope.ClusterDomain()
	s.scope.Info("Reconcile DNS deletion", "DNSZone", clusterZoneName)

	currentRecordSets, err := s.azureClient.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		// Zone doesn't exist already, nothing to do
		s.scope.V(2).Info("DNS zone not found", "DNSZone", clusterZoneName)
		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	nsRecords := filterAndGetNSRecords(currentRecordSets)
	// Create required NS records.
	err = s.deleteNSRecords(ctx, nsRecords)
	if err != nil {
		return microerror.Mask(err)
	}

	zoneName := s.scope.ClusterDomain()
	s.scope.V(2).Info("Deleting cluster DNS zone", "DNSZone", zoneName)

	err = s.azureClient.DeleteZone(ctx, s.scope.ResourceGroup(), zoneName)
	if azure.IsParentResourceNotFound(err) {
		s.scope.Info("Cannot delete DNS zone in workload cluster, resource group not found", "resourceGroup", s.scope.ResourceGroup(), "DNSZone", zoneName, "error", err.Error())
	} else if capzazure.ResourceNotFound(err) {
		s.scope.Info("Azure DNS zone resource has already been deleted")
	} else if err != nil {
		return microerror.Mask(err)
	}

	s.scope.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

func (s *Service) createClusterDNSZone(ctx context.Context) (armdns.Zone, error) {
	var dnsZone armdns.Zone
	var err error
	zoneName := s.scope.ClusterDomain()
	s.scope.V(2).Info("Creating DNS zone", "DNSZone", zoneName)

	// DNS zone not found, let's create it.
	dnsZoneParams := armdns.Zone{
		Name:     &zoneName,
		Type:     to.StringPtr(string(azuredns.Public)),
		Location: to.StringPtr(capzazure.Global),
	}
	dnsZone, err = s.azureClient.CreateOrUpdateZone(ctx, s.scope.ResourceGroup(), zoneName, dnsZoneParams)
	if err != nil {
		return armdns.Zone{}, microerror.Mask(err)
	}
	s.scope.V(2).Info("Successfully created DNS zone", "DNSZone", zoneName)

	return dnsZone, nil
}

// func (s *Service) deleteClusterRecords(ctx context.Context, hostedZoneID string) error

// func (r *AzureClusterReconciler) reconcileDeleteWorkloadClusterRecords(ctx context.Context, clusterScope *capzscope.ClusterScope) error {
// 	clusterScopeWrapper, err := scope.NewClusterScopeWrapper(*clusterScope)
// 	if err != nil {
// 		return microerror.Mask(err)
// 	}

// }
