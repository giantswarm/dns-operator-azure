package scope

import (
	"context"
	"fmt"

	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"

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

// DNSScope defines the basic context for an actuator to operate upon.
type DNSScope struct {
	capzscope.AzureClients
	scope.ClusterScope

	baseDomain string
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
	}

	return scope, nil
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

func (s *DNSScope) ResourceGroup() string {
	return s.ClusterName()
}

// Authorizer returns the Azure client Authorizer.
func (s *DNSScope) Authorizer() autorest.Authorizer {
	return s.AzureClients.Authorizer
}
