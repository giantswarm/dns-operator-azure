package scope

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/v3/pkg/errors"
	"github.com/giantswarm/dns-operator-azure/v3/pkg/infracluster"
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
	ClusterScope *infracluster.Scope

	BaseDomain              string
	BaseDomainResourceGroup string
	BaseZoneCredentials     BaseZoneCredentials

	AzureClusterIdentity               infrav1.AzureClusterIdentity
	AzureClusterServicePrincipalSecret corev1.Secret

	ManagementClusterAzureIdentity          infrav1.AzureClusterIdentity
	ManagementClusterServicePrincipalSecret corev1.Secret

	ManagementClusterSpec infrav1.AzureClusterSpec

	ResourceTags map[string]*string
}

// DNSScope defines the basic context for an actuator to operate upon.
type DNSScope struct {
	infracluster.Scope

	baseDomain              string
	baseDomainResourceGroup string
	baseZoneCredentials     BaseZoneCredentials

	identity                  Identity
	managementClusterIdentity Identity

	managementClusterSpec infrav1.AzureClusterSpec

	resourceTags map[string]*string
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
		Scope:                   *params.ClusterScope,
		baseDomain:              params.BaseDomain,
		baseDomainResourceGroup: params.BaseDomainResourceGroup,
		baseZoneCredentials:     params.BaseZoneCredentials,
		identity: Identity{
			clusterIdentity: params.AzureClusterIdentity,
			secret:          params.AzureClusterServicePrincipalSecret,
		},
		managementClusterIdentity: Identity{
			clusterIdentity: params.ManagementClusterAzureIdentity,
			secret:          params.ManagementClusterServicePrincipalSecret,
		},
		managementClusterSpec: params.ManagementClusterSpec,
		resourceTags:          params.ResourceTags,
	}

	return scope, nil
}

func (s *DNSScope) APIEndpoint() string {
	return s.Patcher.APIServerPublicIP().Name
}

func (s *DNSScope) BaseDomain() string {
	return s.baseDomain
}

func (s *DNSScope) ClusterDomain() string {
	return fmt.Sprintf("%s.%s", s.Patcher.ClusterName(), s.baseDomain)
}

func (s *DNSScope) ResourceGroup() string {
	return s.Patcher.ClusterName()
}

func (s *DNSScope) BaseDomainResourceGroup() string {
	return s.baseDomainResourceGroup
}

func (s *DNSScope) BaseZoneCredentials() BaseZoneCredentials {
	return s.baseZoneCredentials
}

func (s *DNSScope) AzureClusterIdentity() infrav1.AzureClusterIdentity {
	return s.identity.clusterIdentity
}

func (s *DNSScope) AzureClientSecret() string {
	return string(s.identity.secret.Data[clientSecretKeyName])
}

func (s *DNSScope) ResourceTags() map[string]*string {
	return s.resourceTags
}
