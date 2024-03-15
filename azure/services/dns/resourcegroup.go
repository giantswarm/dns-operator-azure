package dns

import (
	"context"

	"github.com/giantswarm/microerror"
	"k8s.io/utils/pointer"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

func (s *Service) createClusterResourceGroup(ctx context.Context) (armresources.ResourceGroup, error) {
	logger := log.FromContext(ctx)

	resourceGroupName := s.scope.ResourceGroup()

	resourceGroupParams := armresources.ResourceGroup{
		Name:     &resourceGroupName,
		Location: pointer.String(capzazure.Global),
	}

	resourceGroup, err := s.azureClient.CreateOrUpdateResourceGroup(ctx, resourceGroupName, resourceGroupParams)
	if err != nil {
		return armresources.ResourceGroup{}, microerror.Mask(err)
	}
	logger.Info("Successfully created resource group", "resource group", resourceGroupName)

	return resourceGroup, nil
}

func (s *Service) deleteClusterResourceGroup(ctx context.Context) error {
	logger := log.FromContext(ctx)

	resourceGroupName := s.scope.ResourceGroup()

	err := s.azureClient.DeleteResourceGroup(ctx, resourceGroupName)
	if err != nil {
		return microerror.Mask(err)
	}

	logger.Info("Successfully deleted resource group", "resource group", resourceGroupName)
	return nil
}
