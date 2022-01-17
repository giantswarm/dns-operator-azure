module github.com/giantswarm/dns-operator-azure

go 1.13

require (
	github.com/Azure/aad-pod-identity v1.8.0
	github.com/Azure/azure-sdk-for-go v55.8.0+incompatible
	github.com/Azure/go-autorest/autorest v0.11.24
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.11
	github.com/Azure/go-autorest/autorest/to v0.4.0
	github.com/giantswarm/microerror v0.4.0
	github.com/giantswarm/micrologger v0.6.0
	github.com/go-logr/logr v1.2.2
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/client-go v0.21.3
	sigs.k8s.io/cluster-api v0.4.2
	sigs.k8s.io/cluster-api-provider-azure v0.5.2
	sigs.k8s.io/controller-runtime v0.9.6
)

replace (
	github.com/Microsoft/hcsshim v0.8.7 => github.com/Microsoft/hcsshim v0.8.21
	github.com/coreos/etcd v3.3.13+incompatible => github.com/coreos/etcd v3.3.24+incompatible
	github.com/dgrijalva/jwt-go => github.com/dgrijalva/jwt-go/v4 v4.0.0-preview1
	sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v0.4.2
)
