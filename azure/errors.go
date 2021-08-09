package azure

import (
	"errors"

	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
)

const (
	ParentResourceNotFoundErrorCode = "ParentResourceNotFound"
)

// ResourceNotFound parses the error to check if it's a resource not found error.
func IsParentResourceNotFound(err error) bool {
	if !capzazure.ResourceNotFound(err) {
		return false
	}

	detailedErr := autorest.DetailedError{}
	if !errors.As(err, &detailedErr) {
		return false
	}

	azureRequestErrorPtr, ok := detailedErr.Original.(*azure.RequestError)
	if ok {
		return azureRequestErrorPtr.ServiceError != nil &&
			azureRequestErrorPtr.ServiceError.Code == ParentResourceNotFoundErrorCode
	}

	azureRequestError, ok := detailedErr.Original.(azure.RequestError)
	if ok {
		return azureRequestError.ServiceError.Code == ParentResourceNotFoundErrorCode
	}

	azureServiceErrorPtr, ok := detailedErr.Original.(*azure.ServiceError)
	if ok {
		return azureServiceErrorPtr.Code == ParentResourceNotFoundErrorCode
	}

	azureServiceError, ok := detailedErr.Original.(azure.ServiceError)
	if ok {
		return azureServiceError.Code == ParentResourceNotFoundErrorCode
	}

	return false
}
