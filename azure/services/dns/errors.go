package dns

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/giantswarm/microerror"
)

const (
	resourceGroupNotFoundErrorMessage = "ResourceGroupNotFound"
)

// IsIngressNotRead asserts ingressNotReadyError.
func IsIngressNotReady(err error) bool {
	return microerror.Cause(err) == ingressNotReadyError
}

var ingressNotReadyError = &microerror.Error{
	Kind: "ingressNotReadyError",
}

// IsTooManyICServices asserts tooManyICServicesError.
func IsTooManyICServices(err error) bool {
	return microerror.Cause(err) == tooManyICServicesError
}

var tooManyICServicesError = &microerror.Error{
	Kind: "tooManyICServicesError",
}

// IsResourceNotFoundError asserts resourceNotFoundError.
func IsResourceNotFoundError(err error) bool {
	if microerror.Cause(err) == resourceNotFoundError {
		return true
	}
	if responseErr, ok := err.(*azcore.ResponseError); ok {
		switch responseErr.ErrorCode {
		case resourceGroupNotFoundErrorMessage:
			return true
		}
	}
	return false
}

var resourceNotFoundError = &microerror.Error{
	Kind: "resourceNotFoundError",
}
