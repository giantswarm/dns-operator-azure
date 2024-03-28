package infracluster

import (
	"context"
	"errors"
	"strings"

	"github.com/giantswarm/microerror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/async"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	unstructuredKeySpec = "Spec"
	kindAzureCluster    = "AzureCluster"
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
	Close(ctx context.Context) error
}

type ScopeParams struct {
	Client                  client.Client
	Cluster                 *capi.Cluster
	InfraCluster            *unstructured.Unstructured
	Cache                   *capzscope.ClusterCache
	ManagementClusterConfig ManagementClusterConfig
	ClusterIdentityRef      *corev1.ObjectReference
	ClusterZoneAzureConfig  ClusterZoneAzureConfig
}

type ManagementClusterConfig struct {
	Name      string
	Namespace string
}

type ClusterZoneAzureConfig struct {
	SubscriptionID string
	ClientID       string
	TenantID       string
	ClientSecret   string
	Location       string
}

type Scope struct {
	Client                    client.Client
	Cluster                   *capi.Cluster
	InfraCluster              *unstructured.Unstructured
	cache                     *capzscope.ClusterCache
	Patcher                   Patcher
	publicIPService           async.Getter
	managementClusterConfig   ManagementClusterConfig
	managementCluster         *infrav1.AzureCluster
	managementClusterIdentity *infrav1.AzureClusterIdentity
	clusterIdentityRef        *corev1.ObjectReference
	AzureLocation             string
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

func (s *Scope) ManagementCluster(ctx context.Context) (*infrav1.AzureCluster, error) {
	if s.managementCluster == nil {
		managementCluster, err := managementAzureCluster(ctx, s.Client, s.managementClusterConfig.Name, s.managementClusterConfig.Namespace)
		if err != nil {
			return nil, err
		}
		s.managementCluster = managementCluster
	}
	return s.managementCluster, nil
}

func (s *Scope) ManagementClusterIdentity(ctx context.Context) (*infrav1.AzureClusterIdentity, error) {
	if s.managementClusterIdentity == nil {
		managementCluster, err := s.ManagementCluster(ctx)
		if err != nil {
			return nil, err
		}

		managementClusterIdentity, err := azureClusterIdentity(ctx, s.Client, managementCluster.Spec.IdentityRef)
		if err != nil {
			return nil, err
		}

		s.managementClusterIdentity = managementClusterIdentity
	}
	return s.managementClusterIdentity, nil
}

func (s *Scope) InfraClusterIdentity(ctx context.Context) (*infrav1.AzureClusterIdentity, error) {
	azureInfraClusterSpec := s.AzureClusterSpec()
	if azureInfraClusterSpec != nil {
		identity, err := azureClusterIdentity(ctx, s.Client, azureInfraClusterSpec.IdentityRef)
		if err == nil {
			return identity, nil
		}
	}

	if s.clusterIdentityRef != nil {
		identity, err := azureClusterIdentity(ctx, s.Client, s.clusterIdentityRef)
		if err == nil {
			return identity, nil
		}
	}

	return s.ManagementClusterIdentity(ctx)
}

func NewScope(ctx context.Context, params ScopeParams) (*Scope, error) {
	var err error

	if isAzureCluster(params.InfraCluster) {
		azureCluster := &infrav1.AzureCluster{}
		err = params.Client.Get(ctx, types.NamespacedName{
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
			Client:                  params.Client,
			Cluster:                 params.Cluster,
			InfraCluster:            params.InfraCluster,
			Patcher:                 clusterScope,
			cache:                   params.Cache,
			publicIPService:         publicips.New(clusterScope),
			managementClusterConfig: params.ManagementClusterConfig,
		}, nil
	}

	var managementCluster *infrav1.AzureCluster
	var managementClusterIdentity *infrav1.AzureClusterIdentity

	subscriptionID := params.ClusterZoneAzureConfig.SubscriptionID
	clientID := params.ClusterZoneAzureConfig.ClientID
	tenantID := params.ClusterZoneAzureConfig.TenantID

	if subscriptionID == "" || clientID == "" || tenantID == "" {
		managementCluster, err = managementAzureCluster(ctx, params.Client, params.ManagementClusterConfig.Name, params.ManagementClusterConfig.Namespace)
		if err != nil {
			return nil, err
		}

		managementClusterIdentity, err = azureClusterIdentity(ctx, params.Client, managementCluster.Spec.IdentityRef)
		if err != nil {
			return nil, err
		}

		subscriptionID = managementCluster.Spec.SubscriptionID
		clientID = managementClusterIdentity.Spec.ClientID
		tenantID = managementClusterIdentity.Spec.TenantID
	}

	clusterScope, err := NewCommonPatcher(ctx, CommonPatcherParams{
		ClientID:       clientID,
		SubscriptionID: subscriptionID,
		TenantID:       tenantID,
		K8sClient:      params.Client,
		Cluster:        params.Cluster,
		InfraCluster:   params.InfraCluster,
	})

	if err != nil {
		return nil, err
	}

	scope := &Scope{
		Client:                    params.Client,
		Cluster:                   params.Cluster,
		InfraCluster:              params.InfraCluster,
		Patcher:                   clusterScope,
		AzureLocation:             params.ClusterZoneAzureConfig.Location,
		cache:                     params.Cache,
		publicIPService:           NewPublicIPService(params.Cluster),
		managementClusterConfig:   params.ManagementClusterConfig,
		managementCluster:         managementCluster,
		managementClusterIdentity: managementClusterIdentity,
		clusterIdentityRef:        params.ClusterIdentityRef,
	}

	return scope, nil
}

func azureClusterSpec(infraCluster *unstructured.Unstructured) *infrav1.AzureClusterSpec {
	if !isAzureCluster(infraCluster) {
		return nil
	}
	if clusterSpec, clusterSpecOk := infraCluster.Object[unstructuredKeySpec]; clusterSpecOk {
		if infraClusterSpec, infraClusterSpecOk := clusterSpec.(infrav1.AzureClusterSpec); infraClusterSpecOk {
			return &infraClusterSpec
		}
	}
	if rawClusterSpec, rawClusterSpecOk := infraCluster.Object[strings.ToLower(unstructuredKeySpec)]; rawClusterSpecOk {
		clusterSpecJson, err := json.Marshal(rawClusterSpec)
		if err != nil {
			return nil
		}
		clusterSpec := &infrav1.AzureClusterSpec{}
		err = json.Unmarshal(clusterSpecJson, clusterSpec)
		if err != nil {
			return nil
		}
		return clusterSpec
	}
	return nil
}

func isAzureCluster(infraCluster *unstructured.Unstructured) bool {
	return infraCluster.GetKind() == kindAzureCluster
}

func managementAzureCluster(ctx context.Context, client client.Client, name, namespace string) (*infrav1.AzureCluster, error) {
	managementCluster := &infrav1.AzureCluster{}
	err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, managementCluster)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return managementCluster, nil
		}
		return managementCluster, microerror.Mask(err)
	}

	return managementCluster, nil
}

func azureClusterIdentity(ctx context.Context, client client.Client, identityRef *corev1.ObjectReference) (*infrav1.AzureClusterIdentity, error) {
	if identityRef == nil {
		return nil, errors.New("azure cluster or identity reference does not exist")
	}

	identity := &infrav1.AzureClusterIdentity{}

	err := client.Get(ctx, types.NamespacedName{
		Name:      identityRef.Name,
		Namespace: identityRef.Namespace,
	}, identity)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return identity, nil
		}
		return identity, microerror.Mask(err)
	}

	return identity, nil
}
