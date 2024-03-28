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

	"github.com/giantswarm/microerror"
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

	azurescope "github.com/giantswarm/dns-operator-azure/v2/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v2/azure/services/dns"
	"github.com/giantswarm/dns-operator-azure/v2/azure/services/privatedns"
	"github.com/giantswarm/dns-operator-azure/v2/pkg/infracluster"
	"github.com/giantswarm/dns-operator-azure/v2/pkg/metrics"
)

const (
	AzureClusterControllerFinalizer                 string = "dns-operator-azure.giantswarm.io/azurecluster"
	BastionHostIPAnnotation                         string = "dns-operator-azure.giantswarm.io/bastion-ip"
	privateLinkedAPIServerIP                        string = "dns-operator-azure.giantswarm.io/private-link-apiserver-ip"
	azurePrivateEndpointOperatorApiserverAnnotation string = "azure-private-endpoint-operator.giantswarm.io/private-link-apiserver-ip"
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

	ManagementClusterName      string
	ManagementClusterNamespace string

	InfraClusterZoneAzureConfig      infracluster.ClusterZoneAzureConfig
	ClusterAzureIdentityRefName      string
	ClusterAzureIdentityRefNamespace string
}

// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;update;patch

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := log.FromContext(ctx)
	log.WithValues("cluster", req.NamespacedName)

	cluster, err := util.GetClusterByName(ctx, r.Client, req.Namespace, req.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
	}

	// init the unstructured client
	infraCluster := &unstructured.Unstructured{}

	// get the InfrastructureRef (v1.ObjectReference) from the CAPI cluster
	infraRef := cluster.Spec.InfrastructureRef

	if infraRef == nil {
		log.Info("infrastructure cluster ref for core cluster is not ready", "Cluster", cluster.Name)
		return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// set the GVK to the unstructured infraCluster
	infraCluster.SetGroupVersionKind(infraRef.GroupVersionKind())

	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: infraRef.Namespace, Name: infraRef.Name}, infraCluster); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("infrastructure cluster for core cluster is not ready", "Cluster", cluster.Name)
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
	}

	log.WithValues("infrastructure cluster", infraCluster.GetName())
	log.WithValues("infrastructure group", infraCluster.GroupVersionKind().Group, "infrastructure kind", infraCluster.GroupVersionKind().Kind, "infrastructure version", infraCluster.GroupVersionKind().Version)

	// Return early if the core or infrastructure cluster is paused.
	if annotations.IsPaused(cluster, infraCluster) {
		log.Info("infrastructure or core cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	var clusterIdentityRef *corev1.ObjectReference
	if r.ClusterAzureIdentityRefName != "" && r.ClusterAzureIdentityRefNamespace != "" {
		clusterIdentityRef = &corev1.ObjectReference{
			Name:      r.ClusterAzureIdentityRefName,
			Namespace: r.ClusterAzureIdentityRefNamespace,
		}
	}

	// Create the cluster scope.
	clusterScope, err := infracluster.NewScope(ctx, infracluster.ScopeParams{
		Client:                 r.Client,
		Cluster:                cluster,
		InfraCluster:           infraCluster,
		ClusterZoneAzureConfig: r.InfraClusterZoneAzureConfig,
		ClusterIdentityRef:     clusterIdentityRef,
		ManagementClusterConfig: infracluster.ManagementClusterConfig{
			Name:      r.ManagementClusterName,
			Namespace: r.ManagementClusterNamespace,
		},
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
	if !cluster.DeletionTimestamp.IsZero() || !infraCluster.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, clusterScope)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, clusterScope)
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capi.Cluster{}).
		WithOptions(options).
		Complete(r)
}

func (r *ClusterReconciler) reconcileNormal(ctx context.Context, clusterScope *infracluster.Scope) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling AzureCluster DNS zones")

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

	// If a cluster isn't provisioned we don't need to reconcile it
	// as not all information for creating DNS records are available yet.
	if cluster.Status.Phase != string(capi.ClusterPhaseProvisioned) {
		log.Info(fmt.Sprintf("Requeuing cluster %s - phase %s", cluster.Name, cluster.Status.Phase))
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
	}

	azureClusterIdentity, err := clusterScope.InfraClusterIdentity(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("cluster object was not found", "error", err)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
	}

	log.V(1).Info("azureClusterIdentity information",
		"spec.Type", azureClusterIdentity.Spec.Type,
		"spec.tenantID", azureClusterIdentity.Spec.TenantID,
		"spec.clientID", azureClusterIdentity.Spec.ClientID,
	)

	// Reconcile workload cluster DNS records

	staticServicePrincipalSecret, err := r.infraClusterStaticServicePrincipalSecret(ctx, azureClusterIdentity)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("static service principal secret object was not found", "error", err)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
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
	}

	// add the bastionIP from the annotations
	clusterAnnotations := infraCluster.GetAnnotations()
	if clusterAnnotations[BastionHostIPAnnotation] != "" {
		log.V(1).Info("bastion host annotation is not empty")
		params.BastionIP = clusterAnnotations[BastionHostIPAnnotation]
	}

	// `azureCluster.spec.networkSpec.apiServerLB.privateLinks` is the current identifier
	// for private DNS zone creation in the management cluster
	azureClusterSpec := clusterScope.AzureClusterSpec()
	if azureClusterSpec != nil && len(azureClusterSpec.NetworkSpec.APIServerLB.PrivateLinks) > 0 {

		managementCluster, err := clusterScope.ManagementCluster(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterAzureIdentity, err := clusterScope.ManagementClusterIdentity(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterStaticServicePrincipalSecret := &corev1.Secret{}
		if managementClusterAzureIdentity.Spec.Type == infrav1.ManualServicePrincipal {
			log.V(1).Info(fmt.Sprintf("try to get the referenced secret - %s/%s", managementClusterAzureIdentity.Spec.ClientSecret.Namespace, managementClusterAzureIdentity.Spec.ClientSecret.Name))

			err = r.Client.Get(ctx, types.NamespacedName{
				Name:      managementClusterAzureIdentity.Spec.ClientSecret.Name,
				Namespace: managementClusterAzureIdentity.Spec.ClientSecret.Namespace,
			}, managementClusterStaticServicePrincipalSecret)
			if err != nil {
				if apierrors.IsNotFound(err) {
					log.V(1).Info("static service principal secret object was not found", "error", err)
					return reconcile.Result{}, nil
				}
				return reconcile.Result{}, microerror.Mask(err)
			}
		}

		privateParams := azurescope.PrivateDNSScopeParams{
			BaseDomain:                              r.BaseDomain,
			ClusterName:                             infraCluster.GetName(),
			ManagementClusterSpec:                   managementCluster.Spec,
			ManagementClusterAzureIdentity:          *managementClusterAzureIdentity,
			ManagementClusterServicePrincipalSecret: *managementClusterStaticServicePrincipalSecret,
			VirtualNetworkID:                        managementCluster.Spec.NetworkSpec.Vnet.ID,
		}

		// TODO: delete once azure-private-endpoint-operator uses the desired annotation
		if clusterAnnotations[privateLinkedAPIServerIP] != "" {
			log.V(1).Info("private link api server IP annotation found")
			privateParams.APIServerIP = clusterAnnotations[privateLinkedAPIServerIP]
		}
		// TODO end

		if clusterAnnotations[azurePrivateEndpointOperatorApiserverAnnotation] != "" {
			log.V(1).Info(fmt.Sprintf("annotation %s found", azurePrivateEndpointOperatorApiserverAnnotation))
			privateParams.APIServerIP = clusterAnnotations[azurePrivateEndpointOperatorApiserverAnnotation]
		}

		privateDnsScope, err := azurescope.NewPrivateDNSScope(ctx, privateParams)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		privateDnsService, err := privatedns.New(*privateDnsScope)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		err = privateDnsService.Reconcile(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}
	}

	dnsScope, err := azurescope.NewDNSScope(ctx, params)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	dnsService, err := dns.New(*dnsScope, clusterScope.PublicIPsService())
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
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

	log.Info("Successfully reconciled InfraCluster DNS zones")
	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ClusterReconciler) reconcileDelete(ctx context.Context, clusterScope *infracluster.Scope) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	deletedMetrics := 0

	log.Info("Reconciling AzureCluster DNS zones delete")

	azureClusterIdentity, err := clusterScope.InfraClusterIdentity(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("cluster object was not found", "error", err)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
	}

	staticServicePrincipalSecret, err := r.infraClusterStaticServicePrincipalSecret(ctx, azureClusterIdentity)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("static service principal secret object was not found", "error", err)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
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
	}

	// `azureCluster.spec.networkSpec.apiServerLB.privateLinks` is the current identifier
	// for private DNS zone creation in the management cluster

	if clusterScope.IsAzureCluster() && len(clusterScope.AzureClusterSpec().NetworkSpec.APIServerLB.PrivateLinks) > 0 {

		managementCluster, err := clusterScope.ManagementCluster(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterAzureIdentity, err := clusterScope.ManagementClusterIdentity(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterStaticServicePrincipalSecret := &corev1.Secret{}
		if managementClusterAzureIdentity.Spec.Type == infrav1.ManualServicePrincipal {
			log.V(1).Info(fmt.Sprintf("try to get the referenced secret - %s/%s", managementClusterAzureIdentity.Spec.ClientSecret.Namespace, managementClusterAzureIdentity.Spec.ClientSecret.Name))

			err = r.Client.Get(ctx, types.NamespacedName{
				Name:      managementClusterAzureIdentity.Spec.ClientSecret.Name,
				Namespace: managementClusterAzureIdentity.Spec.ClientSecret.Namespace,
			}, managementClusterStaticServicePrincipalSecret)
			if err != nil {
				if apierrors.IsNotFound(err) {
					log.V(1).Info("static service principal secret object was not found", "error", err)
					return reconcile.Result{}, nil
				}
				return reconcile.Result{}, microerror.Mask(err)
			}
		}

		privateParams := azurescope.PrivateDNSScopeParams{
			BaseDomain:                              r.BaseDomain,
			ClusterName:                             clusterScope.Cluster.Name,
			ManagementClusterSpec:                   managementCluster.Spec,
			ManagementClusterAzureIdentity:          *managementClusterAzureIdentity,
			ManagementClusterServicePrincipalSecret: *managementClusterStaticServicePrincipalSecret,
		}

		privateDnsScope, err := azurescope.NewPrivateDNSScope(ctx, privateParams)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		privateDnsService, err := privatedns.New(*privateDnsScope)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
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

	dnsScope, err := azurescope.NewDNSScope(ctx, params)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	dnsService, err := dns.New(*dnsScope, nil)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
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
	log.V(1).Info(fmt.Sprintf("%d metrics for cluster %s got deleted", deletedMetrics, clusterScope.Patcher.ClusterName()))

	log.Info("Successfully reconciled AzureCluster DNS zones delete")
	return reconcile.Result{}, nil
}

func (r *ClusterReconciler) infraClusterStaticServicePrincipalSecret(ctx context.Context, identity *infrav1.AzureClusterIdentity) (*corev1.Secret, error) {
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
