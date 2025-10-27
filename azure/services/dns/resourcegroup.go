package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/v3"
	"github.com/giantswarm/microerror"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (s *Service) createClusterResourceGroup(ctx context.Context) (armresources.ResourceGroup, error) {
	logger := log.FromContext(ctx)
	resourceGroupName := s.scope.ResourceGroup()

	existingResourceGroup, err := s.azureClient.GetResourceGroup(ctx, resourceGroupName)
	if err == nil {
		return s.updateClusterResourceGroup(ctx, existingResourceGroup)
	} else if !IsResourceNotFoundError(err) {
		return armresources.ResourceGroup{}, microerror.Mask(err)
	}

	logger.V(1).Info("creating resource group for infra cluster",
		"resource group", resourceGroupName,
	)

	location := pointer.String(s.scope.AzureLocation)
	if location == nil || *location == "" {
		logger.V(1).Info("retrieving resource group location from management cluster")

		managementCluster, err := s.scope.ManagementCluster(ctx)
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

func (s *Service) updateClusterResourceGroup(ctx context.Context, existingResourceGroup armresources.ResourceGroup) (armresources.ResourceGroup, error) {
	logger := log.FromContext(ctx)
	resourceGroupName := s.scope.ResourceGroup()

	// check whether tags need to be updated
	tags := s.scope.ResourceTags()
	if !resourceGroupTagsEqual(existingResourceGroup.Tags, tags) {
		logger.V(1).Info("updating resource group tags",
			"resource group", resourceGroupName,
			"tags", tags,
		)

		existingResourceGroup.Tags = mergeResourceTags(existingResourceGroup.Tags, tags)
		existingResourceGroup.Properties.ProvisioningState = nil
		_, err := s.azureClient.CreateOrUpdateResourceGroup(ctx, resourceGroupName, existingResourceGroup)
		if err != nil {
			return armresources.ResourceGroup{}, microerror.Mask(err)
		}
		logger.Info("Successfully updated resource group tags", "resource group", resourceGroupName)
	}

	return existingResourceGroup, nil
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

func resourceGroupTagsEqual(existingTags map[string]*string, newTags map[string]*string) bool {
	if len(existingTags) != len(newTags) {
		return false
	}

	for key, value := range newTags {
		existingValue, ok := existingTags[key]
		if !ok || *existingValue != *value {
			return false
		}
	}

	return true
}

func mergeResourceTags(existingTags map[string]*string, newTags map[string]*string) map[string]*string {
	mergedTags := map[string]*string{}

	for key, value := range existingTags {
		mergedTags[key] = value
	}

	for key, value := range newTags {
		mergedTags[key] = value
	}

	return mergedTags
}
