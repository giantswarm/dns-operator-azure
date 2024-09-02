/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/giantswarm/microerror"

	azurescope "github.com/giantswarm/dns-operator-azure/v3/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v3/azure/services/dns"
	"github.com/giantswarm/dns-operator-azure/v3/azure/services/privatedns"
	"github.com/giantswarm/dns-operator-azure/v3/pkg/infracluster"
	"github.com/giantswarm/dns-operator-azure/v3/pkg/metrics"
)

const (
	AzureClusterControllerFinalizer                 string = "dns-operator-azure.giantswarm.io/azurecluster"
	azurePrivateEndpointOperatorApiServerAnnotation string = "azure-private-endpoint-operator.giantswarm.io/private-link-apiserver-ip"
	azurePrivateEndpointOperatorMcIngressAnnotation string = "azure-private-endpoint-operator.giantswarm.io/private-link-mc-ingress-ip"
)

// ClusterReconcilerx reconciles a Cluster object
type ClusterReconciler struct {
	client.Client

	BaseDomain              string
	BaseDomainResourceGroup string
	BaseZoneClientID        string
	BaseZoneClientSecret    string
	BaseZoneSubscriptionID  string
	BaseZoneTenantID        string
	Recorder                record.EventRecorder

	ManagementClusterConfig     infracluster.ManagementClusterConfig
	InfraClusterZoneAzureConfig infracluster.ClusterZoneAzureConfig

	ClusterAzureIdentityRef *corev1.ObjectReference
}

// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;update;patch

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)
	logger.WithValues("cluster", req.NamespacedName)

	cluster, err := util.GetClusterByName(ctx, r.Client, req.Namespace, req.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		} else {
			return reconcile.Result{}, err
		}
	}

	// init the unstructured client
	infraCluster := &unstructured.Unstructured{}

	// get the InfrastructureRef (v1.ObjectReference) from the CAPI cluster
	infraRef := cluster.Spec.InfrastructureRef

	if infraRef == nil {
		logger.Info("infrastructure cluster ref for core cluster is not ready", "Cluster", cluster.Name)
		return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// set the GVK to the unstructured infraCluster
	infraCluster.SetGroupVersionKind(infraRef.GroupVersionKind())

	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: infraRef.Namespace, Name: infraRef.Name}, infraCluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("infrastructure cluster for core cluster is not ready", "Cluster", cluster.Name)
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
	}

	logger.WithValues("infrastructure cluster", infraCluster.GetName())
	logger.WithValues("infrastructure group", infraCluster.GroupVersionKind().Group, "infrastructure kind", infraCluster.GroupVersionKind().Kind, "infrastructure version", infraCluster.GroupVersionKind().Version)

	// Return early if the core or infrastructure cluster is paused.
	if annotations.IsPaused(cluster, infraCluster) {
		logger.Info("infrastructure or core cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	// Create the cluster scope.
	clusterScope, err := infracluster.NewScope(ctx, infracluster.ScopeParams{
		Client:                  r.Client,
		Cluster:                 cluster,
		InfraCluster:            infraCluster,
		ClusterZoneAzureConfig:  r.InfraClusterZoneAzureConfig,
		ClusterIdentityRef:      r.ClusterAzureIdentityRef,
		ManagementClusterConfig: r.ManagementClusterConfig,
	})
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	defer func() {
		if err = clusterScope.Patcher.Close(ctx); err != nil && reterr == nil {
			reterr = microerror.Mask(err)
		}
	}()

	// Handle deleted clusters
	if !cluster.GetDeletionTimestamp().IsZero() || !infraCluster.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, clusterScope)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, clusterScope)
}

func (r *ClusterReconciler) reconcileNormal(ctx context.Context, clusterScope *infracluster.Scope) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling AzureCluster DNS zones")

	var err error
	cluster := clusterScope.Cluster
	infraCluster := clusterScope.InfraCluster

	// If the AzureCluster doesn't have our finalizer, add it.
	if !controllerutil.ContainsFinalizer(infraCluster, AzureClusterControllerFinalizer) {
		controllerutil.AddFinalizer(infraCluster, AzureClusterControllerFinalizer)
		// Register the finalizer immediately to avoid orphaning Azure resources on delete
		if err = clusterScope.Patcher.PatchObject(ctx); err != nil {
			return reconcile.Result{}, err
		}
	}

	result, err, isReady := isClusterReadyForDnsManagements(cluster, logger, clusterScope)
	if !isReady {
		return result, err
	}

	clusterAnnotations := infraCluster.GetAnnotations()
	azureClusterSpec := clusterScope.AzureClusterSpec()

	// Private DNS for MC-to-WC api
	if azureClusterSpec != nil && clusterAnnotations[azurePrivateEndpointOperatorApiServerAnnotation] != "" {

		logger.V(1).Info(fmt.Sprintf("annotation %s found", azurePrivateEndpointOperatorApiServerAnnotation))

		privateDnsService, result, err := r.getPrivateDnsServiceForMcToWcApi(ctx, logger, clusterScope)
		if err != nil {
			return result, err
		}

		err = privateDnsService.Reconcile(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}
	}

	// Private DNS for WC-to-MC ingress
	if azureClusterSpec != nil && clusterAnnotations[azurePrivateEndpointOperatorMcIngressAnnotation] != "" {

		logger.V(1).Info(fmt.Sprintf("annotation %s found", azurePrivateEndpointOperatorMcIngressAnnotation))

		privateDnsService, result, err := r.getPrivateDnsServiceForWcToMcIngress(ctx, logger, clusterScope)
		if err != nil {
			return result, microerror.Mask(err)
		}

		err = privateDnsService.Reconcile(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}
	}

	// Public DNS
	dnsService, result, err := r.getDnsServiceForPublicRecords(ctx, logger, clusterScope)
	if err != nil {
		return result, microerror.Mask(err)
	}

	// generate base domain info metric
	// dns_operator_base_domain_info{controller="dns-operator-azure",resource_group="root_dns_zone_rg",subscription_id="1be3b2e6-xxxx-xxxx-xxxx-eb35cae23c6a",tenant_id="31f75bf9-xxxx-xxxx-xxxx-eb35cae23c6a",zone="azuretest.gigantic.io"}
	metrics.ZoneInfo.WithLabelValues(
		r.BaseDomain,              // label: zone
		metrics.ZoneTypePublic,    // label: type
		r.BaseDomainResourceGroup, // label: resource_group
		r.BaseZoneTenantID,        // label: tenant_id
		r.BaseZoneSubscriptionID,  // label: subscription_id
	).Set(1)

	err = dnsService.Reconcile(ctx)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	logger.Info("Successfully reconciled InfraCluster DNS zones")
	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ClusterReconciler) reconcileDelete(ctx context.Context, clusterScope *infracluster.Scope) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	deletedMetrics := 0

	logger.Info("Reconciling AzureCluster DNS zones delete")

	infraCluster := clusterScope.InfraCluster
	clusterAnnotations := infraCluster.GetAnnotations()
	azureClusterSpec := clusterScope.AzureClusterSpec()

	// Private DNS for MC-to-WC api
	if azureClusterSpec != nil && clusterAnnotations[azurePrivateEndpointOperatorApiServerAnnotation] != "" {

		logger.V(1).Info(fmt.Sprintf("annotation %s found", azurePrivateEndpointOperatorApiServerAnnotation))

		privateDnsService, result, err := r.getPrivateDnsServiceForMcToWcApi(ctx, logger, clusterScope)
		if err != nil {
			return result, err
		}

		err = privateDnsService.ReconcileDelete(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		deletedMetrics += deleteClusterMetrics(
			fmt.Sprintf("%s.%s", clusterScope.Patcher.ClusterName(), r.BaseDomain),
			metrics.ZoneTypePrivate,
		)
	}

	// Private DNS for WC-to-MC ingress
	if azureClusterSpec != nil && clusterAnnotations[azurePrivateEndpointOperatorMcIngressAnnotation] != "" {

		logger.V(1).Info(fmt.Sprintf("annotation %s found", azurePrivateEndpointOperatorMcIngressAnnotation))

		privateDnsService, result, err := r.getPrivateDnsServiceForWcToMcIngress(ctx, logger, clusterScope)
		if err != nil {
			return result, err
		}

		err = privateDnsService.ReconcileDelete(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		deletedMetrics += deleteClusterMetrics(
			fmt.Sprintf("%s.%s", clusterScope.Patcher.ClusterName(), r.BaseDomain),
			metrics.ZoneTypePrivate,
		)
	}

	// Public DNS
	dnsService, result, err := r.getDnsServiceForPublicRecords(ctx, logger, clusterScope)
	if err != nil {
		return result, microerror.Mask(err)
	}

	err = dnsService.ReconcileDelete(ctx)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	// remove finalizer
	if controllerutil.ContainsFinalizer(clusterScope.InfraCluster, AzureClusterControllerFinalizer) {
		controllerutil.RemoveFinalizer(clusterScope.InfraCluster, AzureClusterControllerFinalizer)
	}

	deletedMetrics += deleteClusterMetrics(
		fmt.Sprintf("%s.%s", clusterScope.Patcher.ClusterName(), r.BaseDomain),
		metrics.ZoneTypePublic,
	)
	logger.V(1).Info(fmt.Sprintf("%d metrics for cluster %s got deleted", deletedMetrics, clusterScope.Patcher.ClusterName()))

	logger.Info("Successfully reconciled AzureCluster DNS zones delete")
	return reconcile.Result{}, nil
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capi.Cluster{}).
		WithOptions(options).
		Complete(r)
}

func (r *ClusterReconciler) getDnsServiceForPublicRecords(ctx context.Context, logger logr.Logger, clusterScope *infracluster.Scope) (*dns.Service, ctrl.Result, error) {
	azureClusterIdentity, err := clusterScope.InfraClusterIdentity(ctx)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	staticServicePrincipalSecret, err := r.getInfraClusterStaticServicePrincipalSecret(ctx, azureClusterIdentity)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	params := azurescope.DNSScopeParams{
		ClusterScope:                       clusterScope,
		AzureClusterIdentity:               *azureClusterIdentity,
		AzureClusterServicePrincipalSecret: *staticServicePrincipalSecret,
		BaseDomain:                         r.BaseDomain,
		BaseDomainResourceGroup:            r.BaseDomainResourceGroup,
		BaseZoneCredentials: azurescope.BaseZoneCredentials{
			ClientID:       r.BaseZoneClientID,
			ClientSecret:   r.BaseZoneClientSecret,
			SubscriptionID: r.BaseZoneSubscriptionID,
			TenantID:       r.BaseZoneTenantID,
		},
		ResourceTags: infracluster.GetResourceTagsFromInfraClusterAnnotations(clusterScope.InfraClusterAnnotations()),
	}

	dnsScope, err := azurescope.NewDNSScope(ctx, params)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	dnsService, err := dns.New(*dnsScope, clusterScope.PublicIPsService())
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	return dnsService, ctrl.Result{}, nil
}

func (r *ClusterReconciler) getPrivateDnsServiceForMcToWcApi(ctx context.Context, logger logr.Logger, clusterScope *infracluster.Scope) (*privatedns.Service, ctrl.Result, error) {
	managementCluster, err := clusterScope.ManagementCluster(ctx)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	managementClusterAzureIdentity, err := clusterScope.ManagementClusterIdentity(ctx)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	managementClusterStaticServicePrincipalSecret := &corev1.Secret{}
	if managementClusterAzureIdentity.Spec.Type == infrav1.ManualServicePrincipal {
		logger.V(1).Info(fmt.Sprintf("try to get the referenced secret - %s/%s", managementClusterAzureIdentity.Spec.ClientSecret.Namespace, managementClusterAzureIdentity.Spec.ClientSecret.Name))

		err = r.Client.Get(ctx, types.NamespacedName{
			Name:      managementClusterAzureIdentity.Spec.ClientSecret.Name,
			Namespace: managementClusterAzureIdentity.Spec.ClientSecret.Namespace,
		}, managementClusterStaticServicePrincipalSecret)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("static service principal secret object was not found", "error", err)
				return nil, reconcile.Result{}, nil
			}
			return nil, reconcile.Result{}, microerror.Mask(err)
		}
	}

	infraCluster := clusterScope.InfraCluster
	clusterAnnotations := infraCluster.GetAnnotations()

	privateParams := azurescope.PrivateDNSScopeParams{
		BaseDomain:                             r.BaseDomain,
		ClusterName:                            infraCluster.GetName(),
		ClusterSpecToAttachPrivateDNS:          managementCluster.Spec,
		ClusterAzureIdentityToAttachPrivateDNS: *managementClusterAzureIdentity,
		ClusterServicePrincipalSecretToAttachPrivateDNS: *managementClusterStaticServicePrincipalSecret,
		VirtualNetworkIDToAttachPrivateDNS:              managementCluster.Spec.NetworkSpec.Vnet.ID,
		APIServerIP:                                     clusterAnnotations[azurePrivateEndpointOperatorApiServerAnnotation],
	}

	privateDnsScope, err := azurescope.NewPrivateDNSScope(ctx, privateParams)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	privateDnsService, err := privatedns.New(*privateDnsScope)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	return privateDnsService, ctrl.Result{}, nil
}

func (r *ClusterReconciler) getPrivateDnsServiceForWcToMcIngress(ctx context.Context, logger logr.Logger, clusterScope *infracluster.Scope) (*privatedns.Service, ctrl.Result, error) {
	managementCluster, err := clusterScope.ManagementCluster(ctx)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	infraClusterAzureIdentity, err := clusterScope.InfraClusterIdentity(ctx)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	infraClusterStaticServicePrincipalSecret := &corev1.Secret{}
	if infraClusterAzureIdentity.Spec.Type == infrav1.ManualServicePrincipal {
		logger.V(1).Info(fmt.Sprintf("try to get the referenced secret - %s/%s", infraClusterAzureIdentity.Spec.ClientSecret.Namespace, infraClusterAzureIdentity.Spec.ClientSecret.Name))

		err = r.Client.Get(ctx, types.NamespacedName{
			Name:      infraClusterAzureIdentity.Spec.ClientSecret.Name,
			Namespace: infraClusterAzureIdentity.Spec.ClientSecret.Namespace,
		}, infraClusterStaticServicePrincipalSecret)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("static service principal secret object was not found", "error", err)
				return nil, reconcile.Result{}, nil
			}
			return nil, reconcile.Result{}, microerror.Mask(err)
		}
	}

	infraCluster := clusterScope.InfraCluster
	clusterAnnotations := infraCluster.GetAnnotations()
	azureClusterSpec := clusterScope.AzureClusterSpec()

	privateParams := azurescope.PrivateDNSScopeParams{
		BaseDomain:                             r.BaseDomain,
		ClusterName:                            managementCluster.GetName(),
		ClusterSpecToAttachPrivateDNS:          *azureClusterSpec,
		ClusterAzureIdentityToAttachPrivateDNS: *infraClusterAzureIdentity,
		ClusterServicePrincipalSecretToAttachPrivateDNS: *infraClusterStaticServicePrincipalSecret,
		VirtualNetworkIDToAttachPrivateDNS:              (*azureClusterSpec).NetworkSpec.Vnet.ID,
		MCIngressIP:                                     clusterAnnotations[azurePrivateEndpointOperatorMcIngressAnnotation],
	}

	privateDnsScope, err := azurescope.NewPrivateDNSScope(ctx, privateParams)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	privateDnsService, err := privatedns.New(*privateDnsScope)
	if err != nil {
		return nil, reconcile.Result{}, microerror.Mask(err)
	}

	return privateDnsService, ctrl.Result{}, nil
}

func (r *ClusterReconciler) getInfraClusterStaticServicePrincipalSecret(ctx context.Context, identity *infrav1.AzureClusterIdentity) (*corev1.Secret, error) {
	staticServicePrincipalSecret := &corev1.Secret{}
	if identity.Spec.Type == infrav1.ManualServicePrincipal {
		err := r.Client.Get(ctx, types.NamespacedName{
			Name:      identity.Spec.ClientSecret.Name,
			Namespace: identity.Spec.ClientSecret.Namespace,
		}, staticServicePrincipalSecret)
		if err != nil {
			return nil, err
		}
	}
	return staticServicePrincipalSecret, nil
}

func isClusterReadyForDnsManagements(cluster *capi.Cluster, logger logr.Logger, clusterScope *infracluster.Scope) (ctrl.Result, error, bool) {
	// If a cluster isn't provisioned we don't need to reconcile it
	// as not all information for creating DNS records are available yet.
	if cluster.Status.Phase != string(capi.ClusterPhaseProvisioned) {
		logger.Info(fmt.Sprintf("Requeuing cluster %s - phase %s", cluster.Name, cluster.Status.Phase))
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil, false
	}

	if clusterScope.IsAzureCluster() {
		// only act on Clusters where the LoadBalancersReady condition is true
		infraClusterStatus := clusterScope.AzureClusterStatus()
		infraClusterIsReady := false
		if infraClusterStatus != nil {
			for _, condition := range infraClusterStatus.Conditions {
				if condition.Type == infrav1.LoadBalancersReadyCondition {
					infraClusterIsReady = true
					break
				}
			}
		}
		if !infraClusterIsReady {
			logger.Info(fmt.Sprintf("Requeuing cluster %s. Load Balancer is not ready.", cluster.Name))
			return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil, false
		}
	}

	return ctrl.Result{}, nil, true
}

// deleteClusterMetrics delete all given metrics where
// labelKey=zone match the given zoneName
func deleteClusterMetrics(zoneName, zoneType string) int {

	deletedMetrics := 0

	deletedMetrics += metrics.ZoneInfo.DeletePartialMatch(prometheus.Labels{
		metrics.MetricZone: zoneName,
		metrics.ZoneType:   zoneType,
	})

	deletedMetrics += metrics.ClusterZoneRecords.DeletePartialMatch(prometheus.Labels{
		metrics.MetricZone: zoneName,
		metrics.ZoneType:   zoneType,
	})

	deletedMetrics += metrics.RecordInfo.DeletePartialMatch(prometheus.Labels{
		metrics.MetricZone: zoneName,
		metrics.ZoneType:   zoneType,
	})

	return deletedMetrics

}
