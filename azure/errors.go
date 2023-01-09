package azure

import (
	"errors"

	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
)

const (
	ParentResourceNotFoundErrorCode = "ParentResourceNotFound"
)

// ResourceNotFound parses the error to check if it's a resource not found error.
func IsParentResourceNotFound(err error) bool {
	derr := autorest.DetailedError{}
	serr := &azure.ServiceError{}
	return errors.As(err, &derr) && errors.As(derr.Original, &serr) && serr.Code == ParentResourceNotFoundErrorCode
}
