package scope

import (
	"os"
	"strings"

	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/giantswarm/microerror"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
)

const (
	ManagementClusterRegion         = "MANAGEMENT_CLUSTER_AZURE_REGION"
	ManagementClusterTenantID       = "MANAGEMENT_CLUSTER_AZURE_TENANT_ID"
	ManagementClusterSubscriptionID = "MANAGEMENT_CLUSTER_AZURE_SUBSCRIPTION_ID"
	ManagementClusterClientID       = "MANAGEMENT_CLUSTER_AZURE_CLIENT_ID"
	ManagementClusterClientSecret   = "MANAGEMENT_CLUSTER_AZURE_CLIENT_SECRET"
)

func NewManagementClusterAzureClients() (*capzscope.AzureClients, error) {
	settings, err := auth.GetSettingsFromEnvironment()
	if err != nil {
		return nil, microerror.Mask(err)
	}
	settings = overwriteWithManagementClusterEnvVars(settings)

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

func overwriteWithManagementClusterEnvVars(settings auth.EnvironmentSettings) auth.EnvironmentSettings {
	if v := os.Getenv(ManagementClusterRegion); v != "" {
		settings.Values[ManagementClusterRegion] = v
	}
	if v := os.Getenv(ManagementClusterTenantID); v != "" {
		settings.Values[auth.TenantID] = v
	}
	if v := os.Getenv(ManagementClusterSubscriptionID); v != "" {
		settings.Values[auth.SubscriptionID] = v
	}
	if v := os.Getenv(ManagementClusterClientID); v != "" {
		settings.Values[auth.ClientID] = v
	}
	if v := os.Getenv(ManagementClusterClientSecret); v != "" {
		settings.Values[auth.ClientSecret] = v
	}

	return settings
}
