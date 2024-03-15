package infracluster

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/async"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Patcher interface {
	ClientID() string
	SubscriptionID() string
	TenantID() string
	ClusterName() string
	PatchObject(ctx context.Context) error
	IsAPIServerPrivate() bool
	APIServerPrivateIP() string
	APIServerPublicIP() *infrav1.PublicIPSpec
}

type ScopeParams struct {
	ClientID       string
	SubscriptionID string
	TenantID       string
	Client         client.Client
	Cluster        *capi.Cluster
	InfraCluster   *unstructured.Unstructured
	Cache          *capzscope.ClusterCache
}

type Scope struct {
	Client          client.Client
	Cluster         *capi.Cluster
	InfraCluster    *unstructured.Unstructured
	cache           *capzscope.ClusterCache
	Patcher         Patcher
	publicIPService async.Getter
}

func (s *Scope) AzureClusterSpec() *infrav1.AzureClusterSpec {
	return azureClusterSpec(s.InfraCluster)
}

func (s *Scope) IsAzureCluster() bool {
	return isAzureCluster(s.InfraCluster)
}

func (s *Scope) PublicIPsService() async.Getter {
	return s.publicIPService
}

func NewScope(ctx context.Context, params ScopeParams) (*Scope, error) {
	if isAzureCluster(params.InfraCluster) {
		azureCluster := &infrav1.AzureCluster{}
		err := params.Client.Get(ctx, types.NamespacedName{
			Name:      params.Cluster.Spec.InfrastructureRef.Name,
			Namespace: params.Cluster.Spec.InfrastructureRef.Namespace,
		}, azureCluster)

		if err != nil {
			return nil, err
		}

		clusterScope, err := capzscope.NewClusterScope(ctx, capzscope.ClusterScopeParams{
			Client:       params.Client,
			Cluster:      params.Cluster,
			AzureCluster: azureCluster,
		})

		if err != nil {
			return nil, err
		}

		return &Scope{
			Client:          params.Client,
			Cluster:         params.Cluster,
			InfraCluster:    params.InfraCluster,
			Patcher:         clusterScope,
			cache:           params.Cache,
			publicIPService: publicips.New(clusterScope),
		}, nil
	}

	clusterScope, err := NewCommonPatcher(ctx, CommonPatcherParams{
		ClientID:       params.ClientID,
		SubscriptionID: params.SubscriptionID,
		TenantID:       params.TenantID,
		K8sClient:      params.Client,
		Cluster:        params.Cluster,
		InfraCluster:   params.InfraCluster,
	})

	if err != nil {
		return nil, err
	}

	return &Scope{
		Client:          params.Client,
		Cluster:         params.Cluster,
		InfraCluster:    params.InfraCluster,
		Patcher:         clusterScope,
		cache:           params.Cache,
		publicIPService: NewPublicIPService(params.Cluster),
	}, nil
}

func azureClusterSpec(infraCluster *unstructured.Unstructured) *infrav1.AzureClusterSpec {
	if clusterSpec, ok := infraCluster.Object["Spec"]; ok {
		if azureClusterSpec, ok := clusterSpec.(infrav1.AzureClusterSpec); ok {
			return &azureClusterSpec
		}
	}
	return nil
}

func isAzureCluster(infraCluster *unstructured.Unstructured) bool {
	return azureClusterSpec(infraCluster) != nil
}
