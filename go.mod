module github.com/giantswarm/dns-operator-azure

go 1.13

require (
	k8s.io/apimachinery v0.17.17
	k8s.io/client-go v0.17.17
	sigs.k8s.io/controller-runtime v0.5.14
)

replace (
	github.com/coreos/etcd v3.3.10+incompatible => github.com/coreos/etcd v3.3.25+incompatible
	github.com/gorilla/websocket v1.4.0+incompatible => github.com/gorilla/websocket v1.4.2+incompatible
)
