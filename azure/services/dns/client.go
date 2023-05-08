package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"

	"github.com/giantswarm/dns-operator-azure/v2/pkg/metrics"
)

//type client interface {
//	GetZone(ctx context.Context, resourceGroupName string, zoneName string) (armdns.Zone, error)
//	CreateOrUpdateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armdns.Zone) (armdns.Zone, error)
//	DeleteZone(ctx context.Context, resourceGroupName string, zoneName string) error
//	CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, name string, recordSet armdns.RecordSet) (armdns.RecordSet, error)
//	DeleteRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, recordSetName string) error
//	ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armdns.RecordSet, error)
//}

type azureClient struct {
	zones      *armdns.ZonesClient
	recordSets *armdns.RecordSetsClient
}

var _ client = (*azureClient)(nil)

func newAzureClient(scope scope.DNSScope) (*azureClient, error) {

	clusterIdentity := scope.GetAzureClusterIdentity()

	var cred azcore.TokenCredential
	var err error

	switch clusterIdentity.Spec.Type {
	case infrav1.UserAssignedMSI:
		cred, err = azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
			ID: azidentity.ClientID(scope.ClientID()),
		})
		if err != nil {
			return nil, microerror.Mask(err)
		}

	case infrav1.ManualServicePrincipal:
		secret := scope.GetAzureClientSecret()

		cred, err = azidentity.NewClientSecretCredential(clusterIdentity.Spec.TenantID, clusterIdentity.Spec.ClientID, secret, nil)
		if err != nil {
			return nil, err
		}
	}

	zonesClient, err := newZonesClient(scope.SubscriptionID(), cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	recordSetsClient, err := newRecordSetsClient(scope.SubscriptionID(), cred)
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

	// dns_operator_api_request_total{controller="dns-operator-azure",method="zones.Get"}
	metrics.AzureRequest.WithLabelValues("zones.Get").Inc()

	resp, err := ac.zones.Get(ctx, resourceGroupName, zoneName, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="zones.Get"}
		metrics.AzureRequestError.WithLabelValues("zones.Get").Inc()
		return armdns.Zone{}, microerror.Mask(err)
	}

	return resp.Zone, nil
}

func (ac *azureClient) CreateOrUpdateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armdns.Zone) (armdns.Zone, error) {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="zones.CreateOrUpdate"}
	metrics.AzureRequest.WithLabelValues("zones.CreateOrUpdate").Inc()

	zoneResult, err := ac.zones.CreateOrUpdate(ctx, resourceGroupName, zoneName, zone, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="zones.CreateOrUpdate"}
		metrics.AzureRequestError.WithLabelValues("zones.CreateOrUpdate").Inc()
		return armdns.Zone{}, microerror.Mask(err)
	}

	return zoneResult.Zone, nil
}

func (ac *azureClient) DeleteZone(ctx context.Context, resourceGroupName string, zoneName string) error {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="zones.Get"}
	metrics.AzureRequest.WithLabelValues("zones.BeginDelete").Inc()

	poller, err := ac.zones.BeginDelete(ctx, resourceGroupName, zoneName, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="zones.Get"}
		metrics.AzureRequestError.WithLabelValues("zones.BeginDelete").Inc()
		return microerror.Mask(err)
	}

	// dns_operator_api_request_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
	metrics.AzureRequest.WithLabelValues("poller.PollUntilDone").Inc()

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
		metrics.AzureRequestError.WithLabelValues("poller.PollUntilDone").Inc()
		return microerror.Mask(err)
	}

	return nil
}

func (ac *azureClient) ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armdns.RecordSet, error) {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="recordSets.NewListByDNSZonePager"}
	metrics.AzureRequest.WithLabelValues("recordSets.NewListByDNSZonePager").Inc()

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

	// dns_operator_api_request_total{controller="dns-operator-azure",method="recordSets.CreateOrUpdate"}
	metrics.AzureRequest.WithLabelValues("recordSets.CreateOrUpdate").Inc()

	resp, err := ac.recordSets.CreateOrUpdate(ctx, resourceGroupName, zoneName, recordSetName, recordType, recordSet, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="recordSets.CreateOrUpdate"}
		metrics.AzureRequestError.WithLabelValues("recordSets.CreateOrUpdate").Inc()
		return armdns.RecordSet{}, microerror.Mask(err)
	}

	return resp.RecordSet, nil
}

func (ac *azureClient) DeleteRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armdns.RecordType, recordSetName string) error {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="recordSets.Delete"}
	metrics.AzureRequest.WithLabelValues("recordSets.Delete").Inc()

	_, err := ac.recordSets.Delete(ctx, resourceGroupName, zoneName, recordSetName, recordType, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="recordSets.Delete"}
		metrics.AzureRequestError.WithLabelValues("recordSets.Delete").Inc()
		return microerror.Mask(err)
	}

	return nil
}
