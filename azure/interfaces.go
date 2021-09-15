package azure

import (
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
)

type ResourceGroupDescriber interface {
	capzazure.Authorizer
	ResourceGroup() string
}
