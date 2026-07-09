package infracluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/giantswarm/k8sclient/v8/pkg/k8sclient"
	"github.com/giantswarm/k8sclient/v8/pkg/k8srestconfig"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/async"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/cluster-api-provider-azure/util/reconciler"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	capi "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kindAzureCluster           = "AzureCluster"
	kindAzureASOManagedCluster = infrav1.AzureASOManagedClusterKind
	kubeConfigSecretSuffix     = "-kubeconfig" //nolint
	kubeConfigSecretKey        = "value"

	// AnnotationAzureSubscriptionID is the annotation on the Cluster object that
	// holds the ID of the Azure subscription the cluster lives in. It is used for
	// AKS (AzureASOManagedCluster) clusters, whose subscription cannot be derived
	// from an AzureCluster spec.
	AnnotationAzureSubscriptionID = "giantswarm.io/azure-subscription-id"

	// AnnotationAzureClusterIdentityName and AnnotationAzureClusterIdentityNamespace
	// are the annotations on the AzureASOManagedCluster object that reference the
	// AzureClusterIdentity to authenticate with. AKS clusters have no AzureCluster
	// spec to carry an IdentityRef, so the reference is provided via annotations.
	AnnotationAzureClusterIdentityName      = "azure.giantswarm.io/azure-cluster-identity"
	AnnotationAzureClusterIdentityNamespace = "azure.giantswarm.io/azure-cluster-identity-namespace"
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
	clusterK8sClient          client.Client
	AzureLocation             string
}

func (s *Scope) AzureClusterSpec() *infrav1.AzureClusterSpec {
	return azureClusterSpec(s.InfraCluster)
}

func (s *Scope) AzureClusterStatus() *infrav1.AzureClusterStatus {
	return azureClusterStatus(s.InfraCluster)
}

func (s *Scope) IsAzureCluster() bool {
	return isAzureCluster(s.InfraCluster)
}

// IsASOManagedCluster reports whether the infrastructure cluster is an
// AzureASOManagedCluster, i.e. an AKS cluster managed via Azure Service Operator.
func (s *Scope) IsASOManagedCluster() bool {
	return isASOManagedCluster(s.InfraCluster)
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

	// AKS (AzureASOManagedCluster) clusters reference their AzureClusterIdentity
	// via annotations on the infrastructure cluster, since they have no
	// AzureCluster spec to carry an IdentityRef.
	if identityRef := asoManagedClusterIdentityRef(s.InfraCluster); identityRef != nil {
		return azureClusterIdentity(ctx, s.Client, identityRef)
	}

	if s.clusterIdentityRef != nil {
		identity, err := azureClusterIdentity(ctx, s.Client, s.clusterIdentityRef)
		if err == nil {
			return identity, nil
		}
	}

	return s.ManagementClusterIdentity(ctx)
}

func (s *Scope) InfraClusterAnnotations() map[string]string {
	return s.InfraCluster.GetAnnotations()
}

func (s *Scope) ClusterK8sClient(ctx context.Context) (client.Client, error) {
	if s.clusterK8sClient == nil {
		var err error
		s.clusterK8sClient, err = s.getClusterK8sClient(ctx)
		if err != nil {
			return nil, err
		}
	}
	return s.clusterK8sClient, nil
}

// SetClusterK8sClient sets the workload cluster k8s client directly, bypassing
// kubeconfig resolution. Used in tests to inject a fake client.
func (s *Scope) SetClusterK8sClient(c client.Client) {
	s.clusterK8sClient = c
}

func NewScope(ctx context.Context, params ScopeParams) (*Scope, error) {
	var err error

	if isAzureCluster(params.InfraCluster) {
		azureCluster := &infrav1.AzureCluster{}
		err = params.Client.Get(ctx, types.NamespacedName{
			Name:      params.Cluster.Spec.InfrastructureRef.Name,
			Namespace: params.Cluster.Namespace,
		}, azureCluster)

		if err != nil {
			return nil, err
		}

		clusterScope, err := capzscope.NewClusterScope(ctx, capzscope.ClusterScopeParams{
			Client:          params.Client,
			Cluster:         params.Cluster,
			AzureCluster:    azureCluster,
			CredentialCache: azure.NewCredentialCache(),
			Timeouts:        reconciler.Timeouts{},
		})

		if err != nil {
			return nil, err
		}

		publicips, err := publicips.New(clusterScope)
		if err != nil {
			return nil, err
		}

		return &Scope{
			Client:                  params.Client,
			Cluster:                 params.Cluster,
			InfraCluster:            params.InfraCluster,
			Patcher:                 clusterScope,
			cache:                   params.Cache,
			publicIPService:         publicips,
			managementClusterConfig: params.ManagementClusterConfig,
		}, nil
	}

	var managementCluster *infrav1.AzureCluster
	var managementClusterIdentity *infrav1.AzureClusterIdentity

	subscriptionID := params.ClusterZoneAzureConfig.SubscriptionID
	clientID := params.ClusterZoneAzureConfig.ClientID
	tenantID := params.ClusterZoneAzureConfig.TenantID

	// AKS (AzureASOManagedCluster) clusters are self-contained: the subscription
	// the cluster lives in and the AzureClusterIdentity to authenticate with are
	// both provided via annotations (on the Cluster and the infrastructure cluster
	// respectively). These take precedence over any env-provided values, and let
	// us avoid falling back to the management cluster.
	if isASOManagedCluster(params.InfraCluster) {
		if annotationSubscriptionID := params.Cluster.GetAnnotations()[AnnotationAzureSubscriptionID]; annotationSubscriptionID != "" {
			subscriptionID = annotationSubscriptionID
		}
		if identityRef := asoManagedClusterIdentityRef(params.InfraCluster); identityRef != nil {
			identity, err := azureClusterIdentity(ctx, params.Client, identityRef)
			if err != nil {
				return nil, err
			}
			clientID = identity.Spec.ClientID
			tenantID = identity.Spec.TenantID
		}
	}

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
	azureClusterResource := azureClusterFromInfraObject(infraCluster)
	if azureClusterResource == nil {
		return nil
	}
	return &azureClusterResource.Spec
}

func azureClusterStatus(infraCluster *unstructured.Unstructured) *infrav1.AzureClusterStatus {
	azureClusterResource := azureClusterFromInfraObject(infraCluster)
	if azureClusterResource == nil {
		return nil
	}
	return &azureClusterResource.Status
}

func azureClusterFromInfraObject(infraCluster *unstructured.Unstructured) *infrav1.AzureCluster {
	if !isAzureCluster(infraCluster) {
		return nil
	}
	azureClusterResource := infrav1.AzureCluster{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(infraCluster.Object, &azureClusterResource)
	if err != nil {
		return nil
	}
	return &azureClusterResource
}

func isAzureCluster(infraCluster *unstructured.Unstructured) bool {
	return infraCluster.GetKind() == kindAzureCluster
}

func isASOManagedCluster(infraCluster *unstructured.Unstructured) bool {
	return infraCluster.GetKind() == kindAzureASOManagedCluster
}

// asoManagedClusterIdentityRef returns a reference to the AzureClusterIdentity
// an AKS (AzureASOManagedCluster) cluster should authenticate with, read from
// the annotations on the infrastructure cluster. It returns nil for other
// cluster types or when the reference annotation is not set.
func asoManagedClusterIdentityRef(infraCluster *unstructured.Unstructured) *corev1.ObjectReference {
	if !isASOManagedCluster(infraCluster) {
		return nil
	}

	annotations := infraCluster.GetAnnotations()
	name := annotations[AnnotationAzureClusterIdentityName]
	if name == "" {
		return nil
	}

	return &corev1.ObjectReference{
		Name:      name,
		Namespace: annotations[AnnotationAzureClusterIdentityNamespace],
	}
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

func (s *Scope) getClusterK8sClient(ctx context.Context) (client.Client, error) {
	newLogger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	kubeconfig, err := s.getClusterKubeConfig(ctx, newLogger)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	config := k8srestconfig.Config{
		Logger:     newLogger,
		KubeConfig: kubeconfig,
	}

	return getK8sClient(config, newLogger)
}

func (s *Scope) getClusterKubeConfig(ctx context.Context, logger micrologger.Logger) (string, error) {
	config := k8srestconfig.Config{
		Logger:    logger,
		InCluster: true,
	}

	k8sClient, err := getK8sClient(config, logger)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var secret corev1.Secret

	o := client.ObjectKey{
		Name:      fmt.Sprintf("%s%s", s.Cluster.Name, kubeConfigSecretSuffix),
		Namespace: s.Cluster.Namespace,
	}

	if err := k8sClient.Get(ctx, o, &secret); err != nil {
		return "", microerror.Mask(err)
	}

	return string(secret.Data[kubeConfigSecretKey]), nil
}

func getK8sClient(config k8srestconfig.Config, logger micrologger.Logger) (client.Client, error) {
	var restConfig *rest.Config
	var err error
	{
		restConfig, err = k8srestconfig.New(config)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var ctrlClient client.Client
	{
		c := k8sclient.ClientsConfig{
			Logger:     logger,
			RestConfig: restConfig,
		}

		k8sClients, err := k8sclient.NewClients(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}

		ctrlClient = k8sClients.CtrlClient()
	}

	return ctrlClient, nil
}

// SetUnstructuredCondition sets a status condition on an unstructured object.
// The given condition can be one of these types:
//   - [k8s.io/apimachinery/pkg/apis/meta/v1.Condition]
//   - [sigs.k8s.io/cluster-api/api/core/v1beta1.Condition]
//
// The first type is used in Cluster API v1beta2. The latter type is deprecated, but still used
// in CAPZ as of version 1.23.0. To avoid this function breaking as soon as CAPZ updates
// to use Cluster API v1beta2, it dispatches internally to handle the correct type.
func SetUnstructuredCondition(obj *unstructured.Unstructured, condition any) error {
	switch c := condition.(type) {
	case metav1.Condition:
		return setUnstructuredCondition(obj, c)
	case clusterv1beta1.Condition:
		return setUnstructuredCAPICondition(obj, c)
	default:
		return fmt.Errorf("unsupported condition type %T", condition)
	}
}

func setUnstructuredCondition(obj *unstructured.Unstructured, condition metav1.Condition) error {
	conditions := make([]metav1.Condition, 0)

	raw, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return err
	}
	if found {
		for _, item := range raw {
			m, ok := item.(map[string]interface{})
			if !ok {
				return errors.New("condition item is not a map")
			}
			var c metav1.Condition
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, &c); err != nil {
				return err
			}
			conditions = append(conditions, c)
		}
	}

	changed := apimeta.SetStatusCondition(&conditions, condition)
	if !changed {
		return nil
	}

	raw = make([]any, len(conditions))
	for i := range conditions {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&conditions[i])
		if err != nil {
			return err
		}
		raw[i] = m
	}

	return unstructured.SetNestedSlice(obj.Object, raw, "status", "conditions")
}

// setUnstructuredCAPICondition sets a status condition in an unstructured object
// using a Condition from Cluster API's deprecated v1beta1 types.
func setUnstructuredCAPICondition(obj *unstructured.Unstructured, condition clusterv1beta1.Condition) error {
	conditions := make([]clusterv1beta1.Condition, 0)

	raw, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return err
	}
	if found {
		for _, item := range raw {
			m, ok := item.(map[string]interface{})
			if !ok {
				return errors.New("condition item is not a map")
			}
			var c clusterv1beta1.Condition
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, &c); err != nil {
				return err
			}
			conditions = append(conditions, c)
		}
	}

	found = false
	for i := range conditions {
		if conditions[i].Type == condition.Type {
			if condition.LastTransitionTime.IsZero() {
				condition.LastTransitionTime = metav1.Now()
			}
			conditions[i] = condition
			found = true
		}
	}
	if !found {
		condition.LastTransitionTime = metav1.Now()
		conditions = append(conditions, condition)
	}

	raw = make([]any, len(conditions))
	for i := range conditions {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&conditions[i])
		if err != nil {
			return err
		}
		raw[i] = m
	}

	return unstructured.SetNestedSlice(obj.Object, raw, "status", "conditions")
}
