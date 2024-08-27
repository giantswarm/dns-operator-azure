package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/giantswarm/microerror"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (s *Service) createClusterResourceGroup(ctx context.Context) (armresources.ResourceGroup, error) {
	logger := log.FromContext(ctx)
	resourceGroupName := s.scope.ResourceGroup()

	existingResourceGroup, err := s.azureClient.GetResourceGroup(ctx, resourceGroupName)

	if err == nil {
		return existingResourceGroup, nil
	} else if !IsResourceNotFoundError(err) {
		return armresources.ResourceGroup{}, microerror.Mask(err)
	}

	logger.V(1).Info("creating resource group for infra cluster",
		"resource group", resourceGroupName,
	)

	location := pointer.String(s.scope.Scope.AzureLocation)
	if location == nil || *location == "" {
		logger.V(1).Info("retrieving resource group location from management cluster")

		managementCluster, err := s.scope.Scope.ManagementCluster(ctx)
		if err != nil {
			return armresources.ResourceGroup{}, microerror.Mask(err)
		}

		managementClusterResourceGroup, err := s.azureClient.GetResourceGroup(ctx, managementCluster.Spec.ResourceGroup)
		if err != nil {
			return armresources.ResourceGroup{}, microerror.Mask(err)
		}

		location = managementClusterResourceGroup.Location
	}

	resourceGroupParams := armresources.ResourceGroup{
		Name:     &resourceGroupName,
		Location: location,
	}
	if tags := s.scope.ResourceTags(); tags != nil {
		resourceGroupParams.Tags = tags
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
	if err != nil && !IsResourceNotFoundError(err) {
		return microerror.Mask(err)
	}

	logger.Info("Successfully deleted resource group", "resource group", resourceGroupName)
	return nil
}
