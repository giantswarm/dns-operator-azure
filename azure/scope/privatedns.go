package scope

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/v3/pkg/errors"
)

const (
	apiPrivateLinkSuffix = "-api-privatelink-privateendpoint"
)

// ClusterScopeParams defines the input parameters used to create a new ClusterScope.
type PrivateDNSScopeParams struct {
	BaseDomain  string
	ClusterName string
	APIServerIP string
	MCIngressIP string

	VirtualNetworkIDToAttachPrivateDNS string

	ClusterAzureIdentityToAttachPrivateDNS          infrav1.AzureClusterIdentity
	ClusterServicePrincipalSecretToAttachPrivateDNS corev1.Secret

	ClusterSpecToAttachPrivateDNS infrav1.AzureClusterSpec
}

// DNSScope defines the basic context for an actuator to operate upon.
type PrivateDNSScope struct {
	baseDomain  string
	clusterName string
	apiServerIP string
	mcIngressIP string

	virtualNetworkID string

	managementClusterIdentity identity

	managementClusterSpec infrav1.AzureClusterSpec
}

type identity struct {
	clusterIdentity infrav1.AzureClusterIdentity
	secret          corev1.Secret
}

func NewPrivateDNSScope(_ context.Context, params PrivateDNSScopeParams) (*PrivateDNSScope, error) {
	if params.BaseDomain == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.BaseDomain must not be nil", params)
	}

	scope := &PrivateDNSScope{
		baseDomain:  params.BaseDomain,
		clusterName: params.ClusterName,
		managementClusterIdentity: identity{
			clusterIdentity: params.ClusterAzureIdentityToAttachPrivateDNS,
			secret:          params.ClusterServicePrincipalSecretToAttachPrivateDNS,
		},
		managementClusterSpec: params.ClusterSpecToAttachPrivateDNS,
		apiServerIP:           params.APIServerIP,
		mcIngressIP:           params.MCIngressIP,
		virtualNetworkID:      params.VirtualNetworkIDToAttachPrivateDNS,
	}

	return scope, nil
}

func (s *PrivateDNSScope) ClusterDomain() string {
	return fmt.Sprintf("%s.%s", s.clusterName, s.baseDomain)
}

func (s *PrivateDNSScope) ClusterName() string {
	return s.clusterName
}

func (s *PrivateDNSScope) ManagementClusterVnetID() string {
	return s.virtualNetworkID
}

func (s *PrivateDNSScope) ManagementClusterResourceGroup() string {
	return s.managementClusterSpec.ResourceGroup
}

func (s *PrivateDNSScope) ManagementClusterAzureIdentity() infrav1.AzureClusterIdentity {
	return s.managementClusterIdentity.clusterIdentity
}

func (s *PrivateDNSScope) ManagementClusterAzureClientSecret() string {
	return string(s.managementClusterIdentity.secret.Data[clientSecretKeyName])
}

func (s *PrivateDNSScope) ManagementClusterSubscriptionID() string {
	return s.managementClusterSpec.SubscriptionID
}

func (s *PrivateDNSScope) ManagementClusterTenantID() string {
	return s.managementClusterIdentity.clusterIdentity.Spec.TenantID
}

func (s *PrivateDNSScope) ManagementClusterClientID() string {
	return s.managementClusterIdentity.clusterIdentity.Spec.ClientID
}

func (s *PrivateDNSScope) PrivateLinkedAPIServerIP() string {

	// the IP in the azureCluster CR takes precedence over the
	// IP from the managementCluster azureCluster CR

	var privateLinkedAPIServerIP string

	switch {
	case len(s.privateLinkedAPIServerIPFromClusterAnnotation()) > 0:
		privateLinkedAPIServerIP = s.privateLinkedAPIServerIPFromClusterAnnotation()
	case len(s.privateLinkedAPIServerIPFromManagementCluster()) > 0:
		privateLinkedAPIServerIP = s.privateLinkedAPIServerIPFromManagementCluster()
	}

	return privateLinkedAPIServerIP
}

func (s *PrivateDNSScope) PrivateLinkedMcIngressIP() string {
	return s.privateLinkedMcIngressIPFromClusterAnnotation()
}

func (s *PrivateDNSScope) privateLinkedAPIServerIPFromManagementCluster() string {

	var privateLinkedAPIServerIP string

	for _, subnet := range s.managementClusterSpec.NetworkSpec.Subnets {
		for _, privateEndpoint := range subnet.PrivateEndpoints {
			if privateEndpoint.Name == s.clusterName+apiPrivateLinkSuffix {
				if len(privateEndpoint.PrivateIPAddresses) > 0 {
					privateLinkedAPIServerIP = privateEndpoint.PrivateIPAddresses[0]
				}
			}
		}
	}
	return privateLinkedAPIServerIP
}

func (s *PrivateDNSScope) privateLinkedAPIServerIPFromClusterAnnotation() string {
	return s.apiServerIP
}

func (s *PrivateDNSScope) privateLinkedMcIngressIPFromClusterAnnotation() string {
	return s.mcIngressIP
}
