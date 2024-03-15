package infracluster

import (
	"context"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"net"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CommonPatcherParams struct {
	ClientID       string
	SubscriptionID string
	TenantID       string
	Cluster        *capi.Cluster
	InfraCluster   *unstructured.Unstructured
	K8sClient      client.Client
}

type CommonPatcher struct {
	Cluster        *capi.Cluster
	InfraCluster   *unstructured.Unstructured
	clientID       string
	subscriptionID string
	tenantID       string
	k8sClient      client.Client
	ip             net.IP
}

func NewCommonPatcher(ctx context.Context, params CommonPatcherParams) (*CommonPatcher, error) {
	return &CommonPatcher{
		Cluster:        params.Cluster,
		InfraCluster:   params.InfraCluster,
		clientID:       params.ClientID,
		subscriptionID: params.SubscriptionID,
		tenantID:       params.TenantID,
		k8sClient:      params.K8sClient,
		ip:             net.ParseIP(params.Cluster.Spec.ControlPlaneEndpoint.Host),
	}, nil
}

func (s *CommonPatcher) ClientID() string {
	return s.clientID
}

func (s *CommonPatcher) SubscriptionID() string {
	return s.subscriptionID
}

func (s *CommonPatcher) TenantID() string {
	return s.tenantID
}

func (s *CommonPatcher) ClusterName() string {
	return s.Cluster.Name
}

func (s *CommonPatcher) PatchObject(ctx context.Context) error {
	return s.k8sClient.Update(ctx, s.InfraCluster)
}

func (s *CommonPatcher) IsAPIServerPrivate() bool {
	return s.ip != nil && s.ip.IsPrivate()
}

func (s *CommonPatcher) APIServerPrivateIP() string {
	if s.ip.IsPrivate() {
		return s.ip.String()
	}
	return ""
}

func (s *CommonPatcher) APIServerPublicIP() *infrav1.PublicIPSpec {
	if s.ip.IsPrivate() {
		return nil
	}
	return &infrav1.PublicIPSpec{
		Name: s.ip.String(),
	}
}
