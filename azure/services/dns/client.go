package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
)

type client interface {
	GetZone(ctx context.Context, resourceGroupName string, zoneName string) (*dns.Zone, error)
	CreateOrUpdateZone(ctx context.Context, resourceGroupName string, zoneName string, zone dns.Zone) (*dns.Zone, error)
	DeleteZone(ctx context.Context, resourceGroupName string, zoneName string) error
	CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType dns.RecordType, name string, recordSet dns.RecordSet) (*dns.RecordSet, error)
	DeleteRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType dns.RecordType, recordSetName string) error
	ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]dns.RecordSet, error)
}

type azureClient struct {
	zones      dns.ZonesClient
	recordSets dns.RecordSetsClient
}

var _ client = (*azureClient)(nil)

func newClient(auth capzazure.Authorizer) *azureClient {
	zonesClient := newZonesClient(auth.SubscriptionID(), auth.Authorizer())
	recordSetsClient := newRecordSetsClient(auth.SubscriptionID(), auth.Authorizer())
	return &azureClient{
		zones:      zonesClient,
		recordSets: recordSetsClient,
	}
}

func newZonesClient(subscriptionID string, authorizer autorest.Authorizer) dns.ZonesClient {
	zonesClient := dns.NewZonesClient(subscriptionID)
	capzazure.SetAutoRestClientDefaults(&zonesClient.Client, authorizer)
	return zonesClient
}

func newRecordSetsClient(subscriptionID string, authorizer autorest.Authorizer) dns.RecordSetsClient {
	recordSetsClient := dns.NewRecordSetsClient(subscriptionID)
	capzazure.SetAutoRestClientDefaults(&recordSetsClient.Client, authorizer)
	return recordSetsClient
}

func (ac *azureClient) GetZone(ctx context.Context, resourceGroupName string, zoneName string) (*dns.Zone, error) {
	zoneResult, err := ac.zones.Get(ctx, resourceGroupName, zoneName)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &zoneResult, nil
}

func (ac *azureClient) CreateOrUpdateZone(ctx context.Context, resourceGroupName string, zoneName string, zone dns.Zone) (*dns.Zone, error) {
	zoneResult, err := ac.zones.CreateOrUpdate(ctx, resourceGroupName, zoneName, zone, "", "")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &zoneResult, nil
}

func (ac *azureClient) DeleteZone(ctx context.Context, resourceGroupName string, zoneName string) error {
	future, err := ac.zones.Delete(ctx, resourceGroupName, zoneName, "")
	if err != nil {
		return microerror.Mask(err)
	}

	err = future.WaitForCompletionRef(ctx, ac.zones.Client)
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = future.Result(ac.zones)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (ac *azureClient) ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]dns.RecordSet, error) {
	recordsSetsResultPage, err := ac.recordSets.ListByDNSZone(ctx, resourceGroupName, zoneName, nil, "")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var recordSets []dns.RecordSet
	for recordsSetsResultPage.NotDone() {
		recordSets = append(recordSets, recordsSetsResultPage.Values()...)
		err = recordsSetsResultPage.NextWithContext(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	return recordSets, nil
}

func (ac *azureClient) CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType dns.RecordType, recordSetName string, recordSet dns.RecordSet) (*dns.RecordSet, error) {
	recordSet, err := ac.recordSets.CreateOrUpdate(ctx, resourceGroupName, zoneName, recordSetName, recordType, recordSet, "", "")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &recordSet, nil
}

func (ac *azureClient) DeleteRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType dns.RecordType, recordSetName string) error {
	_, err := ac.recordSets.Delete(ctx, resourceGroupName, zoneName, recordSetName, recordType, "")
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
