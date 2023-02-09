package dns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/giantswarm/microerror"
	"sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"

	"github.com/giantswarm/dns-operator-azure/azure/scope"
)

type client interface {
	GetZone(ctx context.Context, resourceGroupName string, zoneName string) (armdns.Zone, error)
	CreateOrUpdateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armdns.Zone) (armdns.Zone, error)
	DeleteZone(ctx context.Context, resourceGroupName string, zoneName string) error
	CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, name string, recordSet armdns.RecordSet) (armdns.RecordSet, error)
	DeleteRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, recordSetName string) error
	ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armdns.RecordSet, error)
}

type azureClient struct {
	zones      *armdns.ZonesClient
	recordSets *armdns.RecordSetsClient
}

var _ client = (*azureClient)(nil)

func newAzureClient(azureCluster *v1beta1.AzureCluster) (*azureClient, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	subscriptionID := azureCluster.Spec.SubscriptionID

	zonesClient, err := newZonesClient(subscriptionID, cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	recordSetsClient, err := newRecordSetsClient(subscriptionID, cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return &azureClient{
		zones:      zonesClient,
		recordSets: recordSetsClient,
	}, nil
}

func newBaseZoneClient(credentials scope.BaseZoneCredentials) (*azureClient, error) {
	cred, err := azidentity.NewClientSecretCredential(credentials.TenantID, credentials.ClientID, credentials.ClientSecret, nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	zonesClient, err := newZonesClient(credentials.SubscriptionID, cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	recordSetsClient, err := newRecordSetsClient(credentials.SubscriptionID, cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return &azureClient{
		zones:      zonesClient,
		recordSets: recordSetsClient,
	}, nil
}

func newZonesClient(subscriptionID string, cred azcore.TokenCredential) (*armdns.ZonesClient, error) {
	return armdns.NewZonesClient(subscriptionID, cred, nil)
}

func newRecordSetsClient(subscriptionID string, cred azcore.TokenCredential) (*armdns.RecordSetsClient, error) {
	return armdns.NewRecordSetsClient(subscriptionID, cred, nil)
}

func (ac *azureClient) GetZone(ctx context.Context, resourceGroupName string, zoneName string) (armdns.Zone, error) {
	resp, err := ac.zones.Get(ctx, resourceGroupName, zoneName, nil)
	if err != nil {
		return armdns.Zone{}, microerror.Mask(err)
	}

	return resp.Zone, nil
}

func (ac *azureClient) CreateOrUpdateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armdns.Zone) (armdns.Zone, error) {
	zoneResult, err := ac.zones.CreateOrUpdate(ctx, resourceGroupName, zoneName, zone, nil)
	if err != nil {
		fmt.Printf("%+v\n", err)
		return armdns.Zone{}, microerror.Mask(err)
	}

	return zoneResult.Zone, nil
}

func (ac *azureClient) DeleteZone(ctx context.Context, resourceGroupName string, zoneName string) error {
	poller, err := ac.zones.BeginDelete(ctx, resourceGroupName, zoneName, nil)
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (ac *azureClient) ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armdns.RecordSet, error) {
	recordsSetsResultPager := ac.recordSets.NewListByDNSZonePager(resourceGroupName, zoneName, nil)

	var recordSets []*armdns.RecordSet
	for recordsSetsResultPager.More() {
		nextPage, err := recordsSetsResultPager.NextPage(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		recordSets = append(recordSets, nextPage.RecordSetListResult.Value...)
	}

	return recordSets, nil
}

func (ac *azureClient) CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, recordSetName string, recordSet armdns.RecordSet) (armdns.RecordSet, error) {
	resp, err := ac.recordSets.CreateOrUpdate(ctx, resourceGroupName, zoneName, recordSetName, recordType, recordSet, nil)

	if err != nil {
		return armdns.RecordSet{}, microerror.Mask(err)
	}

	return resp.RecordSet, nil
}

func (ac *azureClient) DeleteRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, recordSetName string) error {
	_, err := ac.recordSets.Delete(ctx, resourceGroupName, zoneName, recordSetName, recordType, nil)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
