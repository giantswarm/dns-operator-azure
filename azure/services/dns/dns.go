package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/giantswarm/microerror"
	"k8s.io/utils/pointer"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/async"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/dns-operator-azure/v2/azure"
	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v2/pkg/metrics"
)

type client interface {
	GetZone(ctx context.Context, resourceGroupName string, zoneName string) (armdns.Zone, error)
	CreateOrUpdateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armdns.Zone) (armdns.Zone, error)
	DeleteZone(ctx context.Context, resourceGroupName string, zoneName string) error
	CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, name string, recordSet armdns.RecordSet) (armdns.RecordSet, error)
	DeleteRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, recordSetName string) error
	ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armdns.RecordSet, error)
	GetResourceGroup(ctx context.Context, resourceGroupName string) (armresources.ResourceGroup, error)
	CreateOrUpdateResourceGroup(ctx context.Context, resourceGroupName string, resourceGroup armresources.ResourceGroup) (armresources.ResourceGroup, error)
	DeleteResourceGroup(ctx context.Context, resourceGroupName string) error
}

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

	publicIPsService async.Getter
}

// New creates a new dns service.
func New(scope scope.DNSScope, publicIPsService async.Getter) (*Service, error) {
	azureClient, err := newAzureClient(scope)

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

	log.V(1).Info("client information for base Zone",
		"clientID", s.scope.BaseZoneCredentials().ClientID,
		"tenantID", s.scope.BaseZoneCredentials().TenantID,
		"subscriptionID", s.scope.BaseZoneCredentials().SubscriptionID,
	)
	log.V(1).Info("client information for cluster Zone",
		"clientID", s.scope.Patcher.ClientID(),
		"tenantID", s.scope.Patcher.TenantID(),
		"subscriptionID", s.scope.Patcher.SubscriptionID(),
	)

	// create resource group for non-Azure clusters
	if !s.scope.IsAzureCluster() {
		_, err := s.createClusterResourceGroup(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// create info metric
	// dns_operator_cluster_zone_info{controller="dns-operator-azure",resource_group="glippy",subscription_id="6b1f6e4a-6d0e-4aa4-9a5a-fbaca65a23b3",tenant_id="31f75bf9-3d8c-4691-95c0-83dd71613db8",zone="glippy.azuretest.gigantic.io"} 1
	// dns_operator_cluster_zone_info{controller="dns-operator-azure",resource_group="np1014",subscription_id="6b1f6e4a-6d0e-4aa4-9a5a-fbaca65a23b3",tenant_id="31f75bf9-3d8c-4691-95c0-83dd71613db8",zone="np1014.azuretest.gigantic.io"} 1
	metrics.ZoneInfo.WithLabelValues(
		s.scope.ClusterDomain(),          // label: zone
		metrics.ZoneTypePublic,           // label: type
		s.scope.ResourceGroup(),          // label: resource_group
		s.scope.Patcher.TenantID(),       // label: tenant_id
		s.scope.Patcher.SubscriptionID(), // label: subscription_id
	).Set(1)

	// create DNS Zone
	clusterRecordSets, err := s.azureClient.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil && !azure.IsParentResourceNotFound(err) {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="recordSets.NewListByDNSZonePager"}
		metrics.AzureRequestError.WithLabelValues("recordSets.NewListByDNSZonePager").Inc()

		return microerror.Mask(err)
	} else if azure.IsParentResourceNotFound(err) {
		log.V(1).Info("cluster specific DNS zone not found", "error", err.Error())
		_, err = s.createClusterDNSZone(ctx)
		if err != nil {
			log.V(1).Info("zone creation failed", "error", err.Error())
			return microerror.Mask(err)
		}
	}

	// get cluster specific zone information
	log.V(1).Info("get cluster specific zone information")
	clusterZone, err := s.azureClient.GetZone(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil {
		return microerror.Mask(err)
	}

	// dns_operator_zone_records_sum{controller="dns-operator-azure",zone="glippy.azuretest.gigantic.io"} 30
	metrics.ClusterZoneRecords.WithLabelValues(
		s.scope.ClusterDomain(),
		metrics.ZoneTypePublic,
	).Set(float64(*clusterZone.Properties.NumberOfRecordSets))

	// create NS Record in base zone
	log.V(1).Info("list NS records in basedomain", "resourcegroup", s.scope.BaseDomainResourceGroup(), "dns zone", s.scope.BaseDomain())
	basedomainRecordSets, err := s.azureBaseZoneClient.ListRecordSets(ctx, s.scope.BaseDomainResourceGroup(), s.scope.BaseDomain())
	if err != nil {
		return microerror.Mask(err)
	}

	// dns_operator_zone_records_sum{controller="dns-operator-azure",zone="azuretest.gigantic.io"} 7
	metrics.ClusterZoneRecords.WithLabelValues(
		s.scope.BaseDomain(),
		metrics.ZoneTypePublic,
	).Set(float64(len(basedomainRecordSets)))

	log.V(1).Info("range over received NS records")
	clusterNSRecordExists := false
	for _, basedomainRecordSet := range basedomainRecordSets {
		log.V(1).Info("basedomainRecordSet", "name", basedomainRecordSet.Name)
		if basedomainRecordSet.Name == &s.scope.Cluster.Name {
			if len(basedomainRecordSet.Properties.NsRecords) > 0 {
				clusterNSRecordExists = true
			}
		}
	}

	log.V(1).Info("range over clusterZone name servers")
	clusterZoneNameServers := []*armdns.NsRecord{}
	for _, nameServer := range clusterZone.Properties.NameServers {
		log.V(1).Info("clusterZone name servers", "name server", nameServer)
		clusterZoneNameServers = append(clusterZoneNameServers, &armdns.NsRecord{
			Nsdname: nameServer,
		})
	}

	if !clusterNSRecordExists {
		log.Info("Creating NS records", "NSrecord", s.scope.Patcher.ClusterName(), "DNS zone", s.scope.BaseDomain())
		if err := s.createClusterNSRecord(ctx, clusterZoneNameServers); err != nil {
			return microerror.Mask(err)
		}
		log.Info("Successfully created NS records", "NSrecord", s.scope.Patcher.ClusterName(), "DNS zone", s.scope.BaseDomain())
	}

	// Create required A records.
	if err := s.updateARecords(ctx, clusterRecordSets); err != nil {
		return microerror.Mask(err)
	}

	// Create required CNAME records
	if err = s.updateCnameRecords(ctx, clusterRecordSets); err != nil {
		return microerror.Mask(err)
	}

	log.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

func (s *Service) ReconcileDelete(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("azure-dns-delete")
	clusterZoneName := s.scope.ClusterDomain()
	log.Info("Reconcile DNS deletion", "DNSZone", clusterZoneName)

	log.Info("Deleting NS record", "NSrecord", s.scope.Patcher.ClusterName(), "DNS zone", s.scope.BaseDomain())

	// delete cluster NS records
	if err := s.deleteClusterNSRecords(ctx); err != nil {
		return microerror.Mask(err)
	}

	log.Info("Successfully deleted NS record", "NSrecord", s.scope.Patcher.ClusterName(), "DNS zone", s.scope.BaseDomain())

	// delete non-Azure cluster's resource group
	if !s.scope.IsAzureCluster() {
		err := s.deleteClusterResourceGroup(ctx)
		if err != nil {
			return microerror.Mask(err)
		}
		log.Info("Successfully deleted resource group", "ResourceGroup", s.scope.ResourceGroup())
	}

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
		Location: pointer.String(capzazure.Global),
	}
	dnsZone, err := s.azureClient.CreateOrUpdateZone(ctx, s.scope.ResourceGroup(), zoneName, dnsZoneParams)
	if err != nil {
		return armdns.Zone{}, microerror.Mask(err)
	}
	log.Info("Successfully created DNS zone", "zone", zoneName)

	return dnsZone, nil
}
