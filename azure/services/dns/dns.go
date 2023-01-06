package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/dns-operator-azure/azure"
	"github.com/giantswarm/dns-operator-azure/azure/scope"
)

const (
	RecordSetTypePrefix = "Microsoft.Network/dnszones/"
	RecordSetTypeA      = RecordSetTypePrefix + string(armdns.RecordTypeA)
	RecordSetTypeCNAME  = RecordSetTypePrefix + string(armdns.RecordTypeCNAME)
	RecordSetTypeNS     = RecordSetTypePrefix + string(armdns.RecordTypeNS)
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
	logger := log.FromContext(ctx).WithName("azure-dns-create")
	clusterZoneName := s.scope.ClusterDomain()
	logger.Info("Reconcile DNS", "DNSZone", clusterZoneName)

	currentRecordSets, err := s.azureClient.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		logger.Info("DNS zone not found", "DNSZone", clusterZoneName)

		_, rErr := s.createClusterDNSZone(ctx, logger)
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

	logger.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

func (s *Service) ReconcileDelete(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("azure-dns-delete")
	clusterZoneName := s.scope.ClusterDomain()
	logger.Info("Reconcile DNS deletion", "DNSZone", clusterZoneName)

	currentRecordSets, err := s.azureClient.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		// Zone doesn't exist already, nothing to do
		logger.Info("DNS zone not found", "DNSZone", clusterZoneName)
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
	logger.Info("Deleting cluster DNS zone", "DNSZone", zoneName)

	err = s.azureClient.DeleteZone(ctx, s.scope.ResourceGroup(), zoneName)
	if azure.IsParentResourceNotFound(err) {
		logger.Info("Cannot delete DNS zone in workload cluster, resource group not found", "resourceGroup", s.scope.ResourceGroup(), "DNSZone", zoneName, "error", err.Error())
	} else if capzazure.ResourceNotFound(err) {
		logger.Info("Azure DNS zone resource has already been deleted")
	} else if err != nil {
		return microerror.Mask(err)
	}

	logger.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

func (s *Service) createClusterDNSZone(ctx context.Context, logger logr.Logger) (armdns.Zone, error) {
	var dnsZone armdns.Zone
	var err error
	zoneName := s.scope.ClusterDomain()
	logger.Info("Creating DNS zone", "DNSZone", zoneName)

	// DNS zone not found, let's create it.
	dnsZoneParams := armdns.Zone{
		Name:     &zoneName,
		Type:     to.StringPtr(string(armdns.ZoneTypePublic)),
		Location: to.StringPtr(capzazure.Global),
	}
	dnsZone, err = s.azureClient.CreateOrUpdateZone(ctx, s.scope.ResourceGroup(), zoneName, dnsZoneParams)
	if err != nil {
		return armdns.Zone{}, microerror.Mask(err)
	}
	logger.Info("Successfully created DNS zone", "DNSZone", zoneName)

	return dnsZone, nil
}
