package azure

import (
	capzazure "sigs.k8s.io/cluster-api-provider-azure/cloud"
)

type ResourceGroupDescriber interface {
	capzazure.Authorizer
	ResourceGroup() string
}
