package scope

import (
	"context"
	"fmt"

	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"

	"github.com/giantswarm/dns-operator-azure/azure"
	"github.com/giantswarm/dns-operator-azure/pkg/errors"
)

const (
	ManagementClusterName = "MANAGEMENT_CLUSTER_NAME"
)

// ClusterScopeParams defines the input parameters used to create a new ClusterScope.
type DNSScopeParams struct {
	// Client client.Client
	// Logger logr.Logger
	ClusterScope scope.ClusterScope

	BaseDomain string
}

// ClusterScope defines the basic context for an actuator to operate upon.
type DNSScope struct {
	capzscope.AzureClients
	// Client client.Client
	// logr.Logger
	scope.ClusterScope

	baseDomain string
	// clusterName string
}

func NewDNSScope(_ context.Context, params DNSScopeParams) (*DNSScope, error) {
	if params.BaseDomain == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.BaseDomain must not be nil", params)
	}

	azureClients, err := NewClusterAzureClients()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	scope := &DNSScope{
		AzureClients: *azureClients,
		ClusterScope: params.ClusterScope,
		baseDomain:   params.BaseDomain,
		// clusterName:  params.ClusterName,
	}

	return scope, nil
}

// func (s *ClusterScope) DNSSpec() azure.DNSSpec {
// 	zoneName := fmt.Sprintf("%s.%s.azure.gigantic.io",
// 		s.clusterName,
// 		s.AzureClients.Values[ManagementClusterRegion])

// 	var nsDomainNames []azure.NSDomainNameSpec
// 	for _, nsRecordSpec := range s.workloadClusterNSRecordSetSpecs {
// 		if nsRecordSpec.Name == "@" {
// 			nsDomainNames = nsRecordSpec.NSDomainNames
// 			break
// 		}
// 	}

// 	dnsSpec := azure.DNSSpec{
// 		ZoneName: zoneName,
// 		NSRecordSets: []azure.NSRecordSetSpec{
// 			{
// 				Name:          fmt.Sprintf("%s.k8s", s.workloadClusterName),
// 				NSDomainNames: nsDomainNames,
// 				TTL:           300,
// 			},
// 		},
// 	}

// 	return dnsSpec
// }
func (s *DNSScope) DNSSpec() azure.DNSSpec {
	// zoneName := fmt.Sprintf("%s.%s",
	// 	s.ClusterName(),
	// 	s.baseDomain,
	// )

	dnsSpec := azure.DNSSpec{
		// ZoneName: zoneName,
		// ARecordSets: []azure.ARecordSetSpec{
		// 	{
		// 		Hostname:     "api",
		// 		PublicIPName: s.APIServerPublicIP().Name,
		// 		TTL:          3600,
		// 	},
		// },
		// CNameRecordSets: []azure.CNameRecordSetSpec{
		// 	{
		// 		Alias: "*",
		// 		CName: fmt.Sprintf("ingress.%s", zoneName),
		// 		TTL:   3600,
		// 	},
		// },
		// NSRecordSets: s.nsRecordSetSpecs,
	}

	return dnsSpec
}

func (s *DNSScope) APIEndpoint() string {
	return s.APIServerPublicIP().Name
}

func (s *DNSScope) BaseDomain() string {
	return s.baseDomain
}

func (s *DNSScope) ClusterDomain() string {
	return fmt.Sprintf("%s.%s", s.baseDomain, s.ClusterName())
}

func (s *DNSScope) ClusterZoneName() string {
	return fmt.Sprintf("%s.%s", s.baseDomain, s.ClusterName())
}

// func (s *ClusterScope) SetNSRecordSetSpecs(nsRecordSetSpecs []azure.NSRecordSetSpec) {
// 	s.nsRecordSetSpecs = nsRecordSetSpecs
// }

func (s *DNSScope) ResourceGroup() string {
	return s.ClusterName()
}

// BaseURI returns the Azure ResourceManagerEndpoint.
func (s *DNSScope) BaseURI() string {
	return s.ResourceManagerEndpoint
}

// Authorizer returns the Azure client Authorizer.
func (s *DNSScope) Authorizer() autorest.Authorizer {
	return s.AzureClients.Authorizer
}
