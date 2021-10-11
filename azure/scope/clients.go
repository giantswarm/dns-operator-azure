package scope

import (
	"os"
	"strings"

	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/giantswarm/microerror"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
)

const (
	ManagementClusterRegion = "MANAGEMENT_CLUSTER_AZURE_REGION"
)

func NewManagementClusterAzureClients() (*capzscope.AzureClients, error) {
	settings, err := auth.GetSettingsFromEnvironment()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	if v := os.Getenv(ManagementClusterRegion); v != "" {
		settings.Values[ManagementClusterRegion] = v
	}

	c := &capzscope.AzureClients{}
	c.EnvironmentSettings = settings
	c.ResourceManagerEndpoint = settings.Environment.ResourceManagerEndpoint
	c.ResourceManagerVMDNSSuffix = settings.Environment.ResourceManagerVMDNSSuffix
	c.Values[ManagementClusterRegion] = strings.TrimSuffix(c.Values[ManagementClusterRegion], "\n")
	c.Values[auth.TenantID] = strings.TrimSuffix(c.Values[auth.TenantID], "\n")
	c.Values[auth.SubscriptionID] = strings.TrimSuffix(c.Values[auth.SubscriptionID], "\n")
	c.Values[auth.ClientID] = strings.TrimSuffix(c.Values[auth.ClientID], "\n")
	c.Values[auth.ClientSecret] = strings.TrimSuffix(c.Values[auth.ClientSecret], "\n")

	c.Authorizer, err = c.GetAuthorizer()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return c, nil
}
