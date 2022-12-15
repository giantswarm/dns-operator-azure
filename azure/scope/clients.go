package scope

import (
	// TODO this is deprecated
	// https://github.com/Azure/go-autorest/tree/main/autorest/azure/auth
	// "github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const (
	ClusterRegion = "CLUSTER_AZURE_REGION"
)

func NewClusterAzureCreds() (azcore.TokenCredential, error) {
	return azidentity.NewDefaultAzureCredential(nil)
}

func NewBaseZoneAzureCreds() (azcore.TokenCredential, error) {
	return azidentity.NewClientSecretCredential("tenantID", "clientID", "clientSecret", nil)
}
