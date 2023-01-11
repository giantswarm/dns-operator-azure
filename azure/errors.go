package azure

import (
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

const (
	ParentResourceNotFoundErrorCode = "ParentResourceNotFound"
)

// IsParentResourceNotFound parses the error to check if it's a resource not found error.
func IsParentResourceNotFound(err error) bool {
	rerr := &azcore.ResponseError{}
	errors.As(err, &rerr)

	return errors.As(err, &rerr) && rerr.ErrorCode == ParentResourceNotFoundErrorCode
}
