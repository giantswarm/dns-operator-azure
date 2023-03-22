package scope

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure/scope"

	"github.com/giantswarm/dns-operator-azure/v2/pkg/errors"

	corev1 "k8s.io/api/core/v1"
)

const (
	clientSecretKeyName = "clientSecret"
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

	BaseDomain              string
	BaseDomainResourceGroup string
	BaseZoneCredentials     BaseZoneCredentials

	BastionIP string

	AzureClusterIdentity               infrav1.AzureClusterIdentity
	AzureClusterServicePrincipalSecret corev1.Secret
}

// DNSScope defines the basic context for an actuator to operate upon.
type DNSScope struct {
	scope.ClusterScope

	baseDomain              string
	baseDomainResourceGroup string
	baseZoneCredentials     BaseZoneCredentials
	bastionIP               string

	identity Identity
}

type Identity struct {
	clusterIdentity infrav1.AzureClusterIdentity
	secret          corev1.Secret
}

func NewDNSScope(_ context.Context, params DNSScopeParams) (*DNSScope, error) {
	if params.BaseDomain == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.BaseDomain must not be nil", params)
	}

	if params.BaseDomainResourceGroup == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.BaseDomainResourceGroup must not be nil", params)
	}

	if (params.BaseZoneCredentials == BaseZoneCredentials{}) {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.BaseZoneCredentials must not be nil", params)
	}

	scope := &DNSScope{
		ClusterScope:            params.ClusterScope,
		baseDomain:              params.BaseDomain,
		baseDomainResourceGroup: params.BaseDomainResourceGroup,
		baseZoneCredentials:     params.BaseZoneCredentials,
		bastionIP:               params.BastionIP,
		identity: Identity{
			clusterIdentity: params.AzureClusterIdentity,
			secret:          params.AzureClusterServicePrincipalSecret,
		},
	}

	return scope, nil
}

func (s *DNSScope) APIEndpoint() string {
	return s.APIServerPublicIP().Name
}

func (s *DNSScope) BastionIPList() string {
	return s.bastionIP
}

func (s *DNSScope) BastionIP() []string {
	return strings.Split(s.bastionIP, ",")
}

func (s *DNSScope) BaseDomain() string {
	return s.baseDomain
}

func (s *DNSScope) ClusterDomain() string {
	return fmt.Sprintf("%s.%s", s.ClusterName(), s.baseDomain)
}

func (s *DNSScope) ClusterZoneName() string {
	return fmt.Sprintf("%s.%s", s.ClusterName(), s.baseDomain)
}

func (s *DNSScope) ResourceGroup() string {
	return s.ClusterName()
}

func (s *DNSScope) BaseDomainResourceGroup() string {
	return s.baseDomainResourceGroup
}

func (s *DNSScope) BaseZoneCredentials() BaseZoneCredentials {
	return s.baseZoneCredentials
}

func (s *DNSScope) GetAzureClusterIdentity() infrav1.AzureClusterIdentity {
	return s.identity.clusterIdentity
}

func (s *DNSScope) GetAzureClientSecret() string {
	return string(s.identity.secret.Data[clientSecretKeyName])
}
