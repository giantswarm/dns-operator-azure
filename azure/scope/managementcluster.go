package scope

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/cloud/scope"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/giantswarm/dns-operator-azure/azure"
	"github.com/giantswarm/dns-operator-azure/pkg/errors"
)

const (
	ManagementClusterName = "MANAGEMENT_CLUSTER_NAME"
)

// ManagementClusterScopeParams defines the input parameters used to create a new ManagementClusterScope.
type ManagementClusterScopeParams struct {
	Client                       client.Client
	Logger                       logr.Logger
	WorkloadClusterName          string
	WorkloadClusterNSDomainNames []azure.NSDomainNameSpec
}

// ManagementClusterScope defines the basic context for an actuator to operate upon.
type ManagementClusterScope struct {
	capzscope.AzureClients
	Client client.Client
	logr.Logger

	managementClusterName        string
	workloadClusterName          string
	workloadClusterNSDomainNames []azure.NSDomainNameSpec
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
	if params.WorkloadClusterNSDomainNames == nil {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.WorkloadClusterNSDomainNames must not be nil", params)
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
		Logger:                       params.Logger,
		Client:                       params.Client,
		AzureClients:                 *azureClients,
		managementClusterName:        managementClusterName,
		workloadClusterName:          params.WorkloadClusterName,
		workloadClusterNSDomainNames: params.WorkloadClusterNSDomainNames,
	}

	return scope, nil
}

func (mcs *ManagementClusterScope) DNSSpec() azure.DNSSpec {
	zoneName := fmt.Sprintf("%s.%s.azure.gigantic.io",
		mcs.managementClusterName,
		mcs.Location())

	return azure.DNSSpec{
		ZoneName: zoneName,
		NSRecordSets: []azure.NSRecordSetSpec{
			{
				Name:          fmt.Sprintf("%s.k8s", mcs.workloadClusterName),
				NSDomainNames: mcs.workloadClusterNSDomainNames,
				TTL:           300,
			},
		},
	}
}

func (mcs *ManagementClusterScope) ResourceGroup() string {
	return mcs.managementClusterName
}

// ClusterName is a placeholder func that that just returns an empty string. It
// is need here so that ManagementClusterScope implements dns.Scope interface.
func (mcs *ManagementClusterScope) ClusterName() string {
	return ""
}

func (mcs *ManagementClusterScope) Location() string {
	return mcs.AzureClients.Values[ManagementClusterRegion]
}

func (mcs *ManagementClusterScope) AdditionalTags() capz.Tags {
	// No tags here, as that is used for workload clusters only
	return capz.Tags{}
}

// AvailabilitySetEnabled is a placeholder func that that just returns false.
// It is need here so that ManagementClusterScope implements dns.Scope interface.
func (mcs *ManagementClusterScope) AvailabilitySetEnabled() bool {
	// Dummy func, not used anywhere in dns-operator-azure.
	return false
}

// missing funcs so it implements capzazure.Authorizer

// BaseURI returns the Azure ResourceManagerEndpoint.
func (mcs *ManagementClusterScope) BaseURI() string {
	return mcs.ResourceManagerEndpoint
}

// Authorizer returns the Azure client Authorizer.
func (mcs *ManagementClusterScope) Authorizer() autorest.Authorizer {
	return mcs.AzureClients.Authorizer
}
