module github.com/giantswarm/dns-operator-azure

go 1.13

require (
	github.com/Azure/aad-pod-identity v1.8.0
	github.com/Azure/azure-sdk-for-go v55.2.0+incompatible
	github.com/Azure/go-autorest/autorest v0.11.18
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.3
	github.com/Azure/go-autorest/autorest/to v0.4.0
	github.com/giantswarm/microerror v0.3.0
	github.com/giantswarm/micrologger v0.5.0
	github.com/go-logr/logr v0.4.0
	k8s.io/api v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.21.2
	sigs.k8s.io/cluster-api v0.4.0
	sigs.k8s.io/cluster-api-provider-azure v0.5.1
	sigs.k8s.io/controller-runtime v0.9.1
)

replace sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v0.4.0
