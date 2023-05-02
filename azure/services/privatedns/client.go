package privatedns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v2/pkg/metrics"
	"github.com/giantswarm/microerror"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
)

type Client interface {
	CreateOrUpdatePrivateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armprivatedns.PrivateZone) error
	GetPrivateZone(ctx context.Context, resourceGroupName string, zoneName string) (armprivatedns.PrivateZone, error)
	DeletePrivateZone(ctx context.Context, resourceGroupName string, zoneName string) error
	ListPrivateRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error)

	ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error)
	CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armprivatedns.RecordType, recordSetName string, recordSet armprivatedns.RecordSet) (armprivatedns.RecordSet, error)
}

type azureClient struct {
	privateZones      *armprivatedns.PrivateZonesClient
	privateRecordSets *armprivatedns.RecordSetsClient
}

func newPrivateDNSClient(scope scope.PrivateDNSScope) (*azureClient, error) {

	managementClusterIdentity := scope.GetManagementClusterAzureIdentity()

	var cred azcore.TokenCredential
	var err error

	switch managementClusterIdentity.Spec.Type {
	case infrav1.UserAssignedMSI:
		cred, err = azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
			ID: azidentity.ClientID(managementClusterIdentity.Spec.ClientID),
		})
		if err != nil {
			return nil, microerror.Mask(err)
		}

	case infrav1.ManualServicePrincipal:
		secret := scope.GetManagementClusterAzureClientSecret()

		cred, err = azidentity.NewClientSecretCredential(managementClusterIdentity.Spec.TenantID, managementClusterIdentity.Spec.ClientID, secret, nil)
		if err != nil {
			return nil, err
		}
	}

	privateZonesClient, err := newPrivateZonesClient(scope.GetManagementClusterSubscriptionID(), cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	privateRecordSetsClient, err := newPrivateRecordSetsClient(scope.GetManagementClusterSubscriptionID(), cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &azureClient{
		privateZones:      privateZonesClient,
		privateRecordSets: privateRecordSetsClient,
	}, nil

}

func newPrivateZonesClient(subscriptionID string, cred azcore.TokenCredential) (*armprivatedns.PrivateZonesClient, error) {
	return armprivatedns.NewPrivateZonesClient(subscriptionID, cred, nil)
}

func newPrivateRecordSetsClient(subscriptionID string, cred azcore.TokenCredential) (*armprivatedns.RecordSetsClient, error) {
	return armprivatedns.NewRecordSetsClient(subscriptionID, cred, nil)
}

func (ac *azureClient) ListPrivateRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error) {

	recordsSetsResultPager := ac.privateRecordSets.NewListByTypePager(resourceGroupName, zoneName, armprivatedns.RecordTypeA, nil)

	var recordSets []*armprivatedns.RecordSet
	for recordsSetsResultPager.More() {
		nextPage, err := recordsSetsResultPager.NextPage(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		recordSets = append(recordSets, nextPage.RecordSetListResult.Value...)
	}

	return recordSets, nil

}

func (ac *azureClient) CreateOrUpdatePrivateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armprivatedns.PrivateZone) error {

	poller, err := ac.privateZones.BeginCreateOrUpdate(ctx, resourceGroupName, zoneName, zone, nil)

	// dns_operator_api_request_total{controller="dns-operator-azure",method="zones.CreateOrUpdate"}
	// metrics.AzureRequest.WithLabelValues("zones.CreateOrUpdate").Inc()
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="zones.CreateOrUpdate"}
		// metrics.AzureRequestError.WithLabelValues("zones.CreateOrUpdate").Inc()
		fmt.Printf("%+v\n", err)
		return microerror.Mask(err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="zones.CreateOrUpdate"}
		// metrics.AzureRequestError.WithLabelValues("zones.CreateOrUpdate").Inc()
		fmt.Printf("%+v\n", err)
		return microerror.Mask(err)
	}

	return nil
}

func (ac *azureClient) GetPrivateZone(ctx context.Context, resourceGroupName string, zoneName string) (armprivatedns.PrivateZone, error) {
	resp, err := ac.privateZones.Get(ctx, resourceGroupName, zoneName, nil)

	if err != nil {
		return armprivatedns.PrivateZone{}, microerror.Mask(err)
	}

	return resp.PrivateZone, nil

}

func (ac *azureClient) DeletePrivateZone(ctx context.Context, resourceGroupName string, zoneName string) error {
	poller, err := ac.privateZones.BeginDelete(ctx, resourceGroupName, zoneName, nil)
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}

func (ac *azureClient) ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error) {
	recordsSetsResultPager := ac.privateRecordSets.NewListPager(resourceGroupName, zoneName, nil)

	var recordSets []*armprivatedns.RecordSet
	for recordsSetsResultPager.More() {
		nextPage, err := recordsSetsResultPager.NextPage(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		recordSets = append(recordSets, nextPage.RecordSetListResult.Value...)
	}

	return recordSets, nil
}

func (ac *azureClient) CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armprivatedns.RecordType, recordSetName string, recordSet armprivatedns.RecordSet) (armprivatedns.RecordSet, error) {
	resp, err := ac.privateRecordSets.CreateOrUpdate(ctx, resourceGroupName, zoneName, recordType, recordSetName, recordSet, nil)

	if err != nil {
		metrics.AzureRequestError.WithLabelValues("recordSets.CreateOrUpdate").Inc()
		return armprivatedns.RecordSet{}, microerror.Mask(err)
	}

	return resp.RecordSet, nil
}
