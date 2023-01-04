package scope

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"

	"github.com/giantswarm/dns-operator-azure/pkg/errors"
)

const (
	ManagementClusterName = "MANAGEMENT_CLUSTER_NAME"
)

type BaseZoneCredentials struct {
	ClientID       string
	ClientSecret   string
	SubscriptionID string
	TenantID       string
}

// ClusterScopeParams defines the input parameters used to create a new ClusterScope.
type DNSScopeParams struct {
	ClusterScope scope.ClusterScope

	BaseDomain          string
	BaseZoneCredentials BaseZoneCredentials
}

// DNSScope defines the basic context for an actuator to operate upon.
type DNSScope struct {
	scope.ClusterScope

	baseDomain          string
	baseZoneCredentials BaseZoneCredentials
}

func NewDNSScope(_ context.Context, params DNSScopeParams) (*DNSScope, error) {
	if params.BaseDomain == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.BaseDomain must not be nil", params)
	}

	if (params.BaseZoneCredentials == BaseZoneCredentials{}) {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.BaseZoneCredentials must not be nil", params)
	}

	scope := &DNSScope{
		ClusterScope:        params.ClusterScope,
		baseDomain:          params.BaseDomain,
		baseZoneCredentials: params.BaseZoneCredentials,
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

func (s *DNSScope) BaseZoneCredentials() BaseZoneCredentials {
	return s.baseZoneCredentials
}
