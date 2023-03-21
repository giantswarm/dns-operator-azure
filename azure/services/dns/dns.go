package dns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/dns-operator-azure/v2/azure"
	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"
)

const (
	RecordSetTypePrefix = "Microsoft.Network/dnszones/"
	RecordSetTypeA      = RecordSetTypePrefix + string(armdns.RecordTypeA)
	RecordSetTypeCNAME  = RecordSetTypePrefix + string(armdns.RecordTypeCNAME)
	RecordSetTypeNS     = RecordSetTypePrefix + string(armdns.RecordTypeNS)
)

// Service provides operations on Azure resources.
type Service struct {
	scope scope.DNSScope
	// azureClient is used as client for all CAPI-Cluster related operations
	azureClient client
	// azureBaseZoneClient is used as client for all baseDomain operations
	azureBaseZoneClient client

	publicIPsService *capzpublicips.Service
}

// New creates a new dns service.
func New(scope scope.DNSScope, publicIPsService *capzpublicips.Service) (*Service, error) {
	azureClient, err := newAzureClient(scope.AzureCluster)

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
	log := log.FromContext(ctx).WithName("azure-dns-create")

	clusterZoneName := s.scope.ClusterDomain()
	log.Info("Reconcile DNS", "DNSZone", clusterZoneName)

	// create DNS Zone
	log.Info(fmt.Sprintf("ClusterRecordSet fetching %s/%s", s.scope.ResourceGroup(), clusterZoneName))
	log.Info(fmt.Sprintf("ClusterRecordSet Credentials %s", s.azureClient))
	clusterRecordSets, err := s.azureClient.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil && !azure.IsParentResourceNotFound(err) {
		return microerror.Mask(err)
	} else if azure.IsParentResourceNotFound(err) {
		_, err = s.createClusterDNSZone(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
	}
	log.Info("ClusterRecordSet Fetched")

	// get cluster specific zone information
	clusterZone, err := s.azureClient.GetZone(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil {
		return microerror.Mask(err)
	}

	// create NS Record in base zone
	basedomainRecordSets, err := s.azureBaseZoneClient.ListRecordSets(ctx, s.scope.BaseDomainResourceGroup(), s.scope.BaseDomain())
	if err != nil {
		return microerror.Mask(err)
	}

	clusterNSRecordExists := false
	for _, basedomainRecordSet := range basedomainRecordSets {
		if basedomainRecordSet.Name == &s.scope.Cluster.Name {
			if len(basedomainRecordSet.Properties.NsRecords) > 0 {
				clusterNSRecordExists = true
			}
		}
	}

	clusterZoneNameServers := []*armdns.NsRecord{}
	for _, nameServer := range clusterZone.Properties.NameServers {
		clusterZoneNameServers = append(clusterZoneNameServers, &armdns.NsRecord{
			Nsdname: nameServer,
		})
	}

	if !clusterNSRecordExists {
		log.Info("Creating NS records", "NSrecord", s.scope.ClusterName(), "DNS zone", s.scope.BaseDomain())
		if err := s.createClusterNSRecord(ctx, clusterZoneNameServers); err != nil {
			return microerror.Mask(err)
		}
		log.Info("Successfully created NS records", "NSrecord", s.scope.ClusterName(), "DNS zone", s.scope.BaseDomain())
	}

	// Create required A records.
	if err := s.updateARecords(ctx, clusterRecordSets); err != nil {
		return microerror.Mask(err)
	}

	log.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

func (s *Service) ReconcileDelete(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("azure-dns-delete")
	clusterZoneName := s.scope.ClusterDomain()
	log.Info("Reconcile DNS deletion", "DNSZone", clusterZoneName)

	log.Info("Deleting NS record", "NSrecord", s.scope.ClusterName(), "DNS zone", s.scope.BaseDomain())

	// Create required NS records.
	if err := s.deleteClusterNSRecords(ctx); err != nil {
		return microerror.Mask(err)
	}

	log.Info("Successfully deleted NS record", "NSrecord", s.scope.ClusterName(), "DNS zone", s.scope.BaseDomain())

	log.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

// createClusterDNSZone create a DNS Zone
func (s *Service) createClusterDNSZone(ctx context.Context) (armdns.Zone, error) {
	log := log.FromContext(ctx)
	zoneName := s.scope.ClusterDomain()
	log.Info("Creating DNS zone", "zone", zoneName)

	// DNS zone not found, let's create it.
	// Type is Public if not specified
	dnsZoneParams := armdns.Zone{
		Name:     &zoneName,
		Location: to.StringPtr(capzazure.Global),
	}
	dnsZone, err := s.azureClient.CreateOrUpdateZone(ctx, s.scope.ResourceGroup(), zoneName, dnsZoneParams)
	if err != nil {
		return armdns.Zone{}, microerror.Mask(err)
	}
	log.Info("Successfully created DNS zone", "zone", zoneName)

	return dnsZone, nil
}
