package privatedns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"k8s.io/utils/pointer"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/v3/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v3/pkg/metrics"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
)

type Client interface {
	CreateOrUpdatePrivateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armprivatedns.PrivateZone) error
	GetPrivateZone(ctx context.Context, resourceGroupName string, zoneName string) (armprivatedns.PrivateZone, error)
	DeletePrivateZone(ctx context.Context, resourceGroupName string, zoneName string) error
	ListPrivateRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error)

	CreateOrUpdateVirtualNetworkLink(ctx context.Context, resourceGroupName, zoneName, workloadClusterName, vnetID, vnetLinkName string) error
	ListVirtualNetworkLink(ctx context.Context, resourceGroupName, zoneName string) ([]*armprivatedns.VirtualNetworkLink, error)
	DeleteVirtualNetworkLink(ctx context.Context, resourceGroupName, zoneName, virtualNetworkLinkName string) error

	ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error)
	CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armprivatedns.RecordType, recordSetName string, recordSet armprivatedns.RecordSet) (armprivatedns.RecordSet, error)
}

type azureClient struct {
	privateZones             *armprivatedns.PrivateZonesClient
	privateRecordSets        *armprivatedns.RecordSetsClient
	virtualNetworkLinkClient *armprivatedns.VirtualNetworkLinksClient
}

func newPrivateDNSClient(scope scope.PrivateDNSScope) (*azureClient, error) {

	managementClusterIdentity := scope.ManagementClusterAzureIdentity()

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
		secret := scope.ManagementClusterAzureClientSecret()

		cred, err = azidentity.NewClientSecretCredential(managementClusterIdentity.Spec.TenantID, managementClusterIdentity.Spec.ClientID, secret, nil)
		if err != nil {
			return nil, err
		}
	case infrav1.WorkloadIdentity:
		cred, err = azidentity.NewWorkloadIdentityCredential(&azidentity.WorkloadIdentityCredentialOptions{
			TenantID: managementClusterIdentity.Spec.TenantID,
			ClientID: managementClusterIdentity.Spec.ClientID,
		})
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	privateZonesClient, err := newPrivateZonesClient(scope.ManagementClusterSubscriptionID(), cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	privateRecordSetsClient, err := newPrivateRecordSetsClient(scope.ManagementClusterSubscriptionID(), cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	virtualNetworkLinkClient, err := newVirtualNetworkLinkClient(scope.ManagementClusterSubscriptionID(), cred)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &azureClient{
		privateZones:             privateZonesClient,
		privateRecordSets:        privateRecordSetsClient,
		virtualNetworkLinkClient: virtualNetworkLinkClient,
	}, nil
}

func newPrivateZonesClient(subscriptionID string, cred azcore.TokenCredential) (*armprivatedns.PrivateZonesClient, error) {
	return armprivatedns.NewPrivateZonesClient(subscriptionID, cred, nil)
}

func newPrivateRecordSetsClient(subscriptionID string, cred azcore.TokenCredential) (*armprivatedns.RecordSetsClient, error) {
	return armprivatedns.NewRecordSetsClient(subscriptionID, cred, nil)
}

func newVirtualNetworkLinkClient(subscriptionID string, cred azcore.TokenCredential) (*armprivatedns.VirtualNetworkLinksClient, error) {
	return armprivatedns.NewVirtualNetworkLinksClient(subscriptionID, cred, nil)
}

func (ac *azureClient) ListPrivateRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error) {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="privateRecordSets.NewListByTypePager"}
	metrics.AzureRequest.WithLabelValues("privateRecordSets.NewListByTypePager").Inc()

	recordsSetsResultPager := ac.privateRecordSets.NewListByTypePager(resourceGroupName, zoneName, armprivatedns.RecordTypeA, nil)
	var recordSets []*armprivatedns.RecordSet
	for recordsSetsResultPager.More() {
		nextPage, err := recordsSetsResultPager.NextPage(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		recordSets = append(recordSets, nextPage.Value...)
	}

	return recordSets, nil
}

func (ac *azureClient) CreateOrUpdatePrivateZone(ctx context.Context, resourceGroupName string, zoneName string, zone armprivatedns.PrivateZone) error {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="privateZones.BeginCreateOrUpdate"}
	metrics.AzureRequest.WithLabelValues("privateZones.BeginCreateOrUpdate").Inc()

	poller, err := ac.privateZones.BeginCreateOrUpdate(ctx, resourceGroupName, zoneName, zone, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="privateZones.BeginCreateOrUpdate"}
		metrics.AzureRequestError.WithLabelValues("privateZones.BeginCreateOrUpdate").Inc()
		fmt.Printf("%+v\n", err)
		return microerror.Mask(err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	// dns_operator_api_request_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
	metrics.AzureRequest.WithLabelValues("poller.PollUntilDone").Inc()
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
		metrics.AzureRequestError.WithLabelValues("poller.PollUntilDone").Inc()
		return microerror.Mask(err)
	}

	return nil
}

func (ac *azureClient) CreateOrUpdateVirtualNetworkLink(ctx context.Context, resourceGroupName, zoneName, workloadClusterName, vnetID, vnetLinkName string) error {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="virtualNetworkLinkClient.BeginCreateOrUpdate"}
	metrics.AzureRequest.WithLabelValues("virtualNetworkLinkClient.BeginCreateOrUpdate").Inc()

	poller, err := ac.virtualNetworkLinkClient.BeginCreateOrUpdate(
		ctx,
		resourceGroupName,
		zoneName,
		vnetLinkName,
		armprivatedns.VirtualNetworkLink{
			Location: pointer.String(capzazure.Global),
			Properties: &armprivatedns.VirtualNetworkLinkProperties{
				RegistrationEnabled: pointer.Bool(false),
				VirtualNetwork: &armprivatedns.SubResource{
					ID: pointer.String(vnetID),
				},
			},
		}, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="virtualNetworkLinkClient.BeginCreateOrUpdate"}
		metrics.AzureRequestError.WithLabelValues("virtualNetworkLinkClient.BeginCreateOrUpdate").Inc()
		return microerror.Mask(err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	// dns_operator_api_request_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
	metrics.AzureRequest.WithLabelValues("poller.PollUntilDone").Inc()
	if err != nil {
		// dns_operator_api_request_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
		metrics.AzureRequestError.WithLabelValues("poller.PollUntilDone").Inc()
		return microerror.Mask(err)
	}

	return nil
}

func (ac *azureClient) ListVirtualNetworkLink(ctx context.Context, resourceGroupName, zoneName string) ([]*armprivatedns.VirtualNetworkLink, error) {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="virtualNetworkLinkClient.NewListPager"}
	metrics.AzureRequest.WithLabelValues("virtualNetworkLinkClient.NewListPager").Inc()

	networkLinkPager := ac.virtualNetworkLinkClient.NewListPager(resourceGroupName, zoneName, nil)

	var networkLinks []*armprivatedns.VirtualNetworkLink
	for networkLinkPager.More() {
		nextPage, err := networkLinkPager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		networkLinks = append(networkLinks, nextPage.Value...)
	}

	return networkLinks, nil
}

func (ac *azureClient) DeleteVirtualNetworkLink(ctx context.Context, resourceGroupName, zoneName, virtualNetworkLinkName string) error {

	poller, err := ac.virtualNetworkLinkClient.BeginDelete(ctx, resourceGroupName, zoneName, virtualNetworkLinkName, nil)

	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="virtualNetworkLinkClient.BeginDelete"}
		metrics.AzureRequestError.WithLabelValues("virtualNetworkLinkClient.BeginDelete").Inc()
		return microerror.Mask(err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	// dns_operator_api_request_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
	metrics.AzureRequest.WithLabelValues("poller.PollUntilDone").Inc()
	if err != nil {
		// dns_operator_api_request_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
		metrics.AzureRequestError.WithLabelValues("poller.PollUntilDone").Inc()
		return microerror.Mask(err)
	}

	return nil
}

func (ac *azureClient) GetPrivateZone(ctx context.Context, resourceGroupName string, zoneName string) (armprivatedns.PrivateZone, error) {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="privateZones.Get"}
	metrics.AzureRequest.WithLabelValues("privateZones.Get").Inc()

	resp, err := ac.privateZones.Get(ctx, resourceGroupName, zoneName, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="privateZones.Get"}
		metrics.AzureRequestError.WithLabelValues("privateZones.Get").Inc()
		return armprivatedns.PrivateZone{}, microerror.Mask(err)
	}

	return resp.PrivateZone, nil
}

func (ac *azureClient) DeletePrivateZone(ctx context.Context, resourceGroupName string, zoneName string) error {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="privateZones.BeginDelete"}
	metrics.AzureRequest.WithLabelValues("privateZones.BeginDelete").Inc()

	poller, err := ac.privateZones.BeginDelete(ctx, resourceGroupName, zoneName, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="privateZones.BeginDelete"}
		metrics.AzureRequestError.WithLabelValues("privateZones.BeginDelete").Inc()
		return microerror.Mask(err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	// dns_operator_api_request_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
	metrics.AzureRequest.WithLabelValues("poller.PollUntilDone").Inc()
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="poller.PollUntilDone"}
		metrics.AzureRequestError.WithLabelValues("poller.PollUntilDone").Inc()
		return microerror.Mask(err)
	}
	return nil
}

func (ac *azureClient) ListRecordSets(ctx context.Context, resourceGroupName string, zoneName string) ([]*armprivatedns.RecordSet, error) {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="recordSets.NewListByDNSZonePager"}
	metrics.AzureRequest.WithLabelValues("privateRecordSets.NewListPager").Inc()

	recordsSetsResultPager := ac.privateRecordSets.NewListPager(resourceGroupName, zoneName, nil)

	var recordSets []*armprivatedns.RecordSet
	for recordsSetsResultPager.More() {
		nextPage, err := recordsSetsResultPager.NextPage(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		recordSets = append(recordSets, nextPage.Value...)
	}

	return recordSets, nil
}

func (ac *azureClient) CreateOrUpdateRecordSet(ctx context.Context, resourceGroupName string, zoneName string, recordType armprivatedns.RecordType, recordSetName string, recordSet armprivatedns.RecordSet) (armprivatedns.RecordSet, error) {

	// dns_operator_api_request_total{controller="dns-operator-azure",method="recordSets.CreateOrUpdate"}
	metrics.AzureRequest.WithLabelValues("privateRecordSets.CreateOrUpdate").Inc()

	resp, err := ac.privateRecordSets.CreateOrUpdate(ctx, resourceGroupName, zoneName, recordType, recordSetName, recordSet, nil)
	if err != nil {
		// dns_operator_api_request_errors_total{controller="dns-operator-azure",method="privateRecordSets.CreateOrUpdate"}
		metrics.AzureRequestError.WithLabelValues("privateRecordSets.CreateOrUpdate").Inc()
		return armprivatedns.RecordSet{}, microerror.Mask(err)
	}

	return resp.RecordSet, nil
}

func virtualNetworkLinkName(resourceGroupName string) string {
	// The name of the linked VNET is "resourceGroupName-vnet"
	// This is how CAPZ names the VNET links.
	// We use the same naming convention to avoid conflicts for private MCs.
	return fmt.Sprintf("%s-vnet-link", resourceGroupName)
}
