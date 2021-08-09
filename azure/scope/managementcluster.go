package scope

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/giantswarm/dns-operator-azure/azure"
	"github.com/giantswarm/dns-operator-azure/pkg/errors"
)

const (
	ManagementClusterName = "MANAGEMENT_CLUSTER_NAME"
)

// ManagementClusterScopeParams defines the input parameters used to create a new ManagementClusterScope.
type ManagementClusterScopeParams struct {
	Client                          client.Client
	Logger                          logr.Logger
	WorkloadClusterName             string
	WorkloadClusterNSRecordSetSpecs []azure.NSRecordSetSpec
}

// ManagementClusterScope defines the basic context for an actuator to operate upon.
type ManagementClusterScope struct {
	capzscope.AzureClients
	Client client.Client
	logr.Logger

	nsRecordSetSpecs                []azure.NSRecordSetSpec
	managementClusterName           string
	workloadClusterName             string
	workloadClusterNSRecordSetSpecs []azure.NSRecordSetSpec
}

func NewManagementClusterScope(_ context.Context, params ManagementClusterScopeParams) (*ManagementClusterScope, error) {
	if params.Client == nil {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.Client must not be empty", params)
	}
	if params.Logger == nil {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.Logger must not be empty", params)
	}
	if params.WorkloadClusterName == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.WorkloadClusterName must not be empty", params)
	}
	if params.WorkloadClusterNSRecordSetSpecs == nil {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.WorkloadClusterNSRecordSetSpecs must not be nil", params)
	}

	managementClusterName := os.Getenv(ManagementClusterName)
	if managementClusterName == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%s environment variable is not set", ManagementClusterName)
	}

	azureClients, err := NewManagementClusterAzureClients()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	scope := &ManagementClusterScope{
		AzureClients:                    *azureClients,
		Client:                          params.Client,
		Logger:                          params.Logger,
		managementClusterName:           managementClusterName,
		workloadClusterName:             params.WorkloadClusterName,
		workloadClusterNSRecordSetSpecs: params.WorkloadClusterNSRecordSetSpecs,
	}

	return scope, nil
}

func (s *ManagementClusterScope) DNSSpec() azure.DNSSpec {
	zoneName := fmt.Sprintf("%s.%s.azure.gigantic.io",
		s.managementClusterName,
		s.AzureClients.Values[ManagementClusterRegion])

	var nsDomainNames []azure.NSDomainNameSpec
	for _, nsRecordSpec := range s.workloadClusterNSRecordSetSpecs {
		if nsRecordSpec.Name == "@" {
			nsDomainNames = nsRecordSpec.NSDomainNames
			break
		}
	}

	dnsSpec := azure.DNSSpec{
		ZoneName: zoneName,
		NSRecordSets: []azure.NSRecordSetSpec{
			{
				Name:          fmt.Sprintf("%s.k8s", s.workloadClusterName),
				NSDomainNames: nsDomainNames,
				TTL:           300,
			},
		},
	}

	return dnsSpec
}

func (s *ManagementClusterScope) SetNSRecordSetSpecs(nsRecordSetSpecs []azure.NSRecordSetSpec) {
	s.nsRecordSetSpecs = nsRecordSetSpecs
}

func (s *ManagementClusterScope) ResourceGroup() string {
	return s.managementClusterName
}

// BaseURI returns the Azure ResourceManagerEndpoint.
func (s *ManagementClusterScope) BaseURI() string {
	return s.ResourceManagerEndpoint
}

// Authorizer returns the Azure client Authorizer.
func (s *ManagementClusterScope) Authorizer() autorest.Authorizer {
	return s.AzureClients.Authorizer
}
