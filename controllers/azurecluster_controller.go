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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v2/azure/services/dns"
	"github.com/giantswarm/dns-operator-azure/v2/azure/services/privatedns"

	"github.com/giantswarm/dns-operator-azure/v2/pkg/metrics"
)

const (
	AzureClusterControllerFinalizer                 string = "dns-operator-azure.giantswarm.io/azurecluster"
	BastionHostIPAnnotation                         string = "dns-operator-azure.giantswarm.io/bastion-ip"
	privateLinkedAPIServerIP                        string = "dns-operator-azure.giantswarm.io/private-link-apiserver-ip"
	azurePrivateEndpointOperatorApiserverAnnotation string = "azure-private-endpoint-operator.giantswarm.io/private-link-apiserver-ip"
)

// AzureClusterReconciler reconciles a AzureCluster object
type AzureClusterReconciler struct {
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
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azureclusters,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;update;patch

func (r *AzureClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := log.FromContext(ctx)

	// Fetch the AzureCluster instance
	azureCluster := &capz.AzureCluster{}
	err := r.Get(ctx, req.NamespacedName, azureCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.Recorder.Eventf(azureCluster, corev1.EventTypeNormal, "AzureClusterObjectNotFound", err.Error())
			log.Info("object was not found")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, azureCluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}
	if cluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return reconcile.Result{}, microerror.Mask(err)
	}

	log = log.WithValues("cluster", cluster.Name)

	// Return early if the object or Cluster is paused.
	if annotations.IsPaused(cluster, azureCluster) {
		r.Recorder.Eventf(azureCluster, corev1.EventTypeNormal, "ClusterPaused", "AzureCluster or linked Cluster is marked as paused. Won't reconcile")
		log.Info("AzureCluster or linked Cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	// Create the scope.
	clusterScope, err := capzscope.NewClusterScope(ctx, capzscope.ClusterScopeParams{
		Client:       r.Client,
		Cluster:      cluster,
		AzureCluster: azureCluster,
	})
	if err != nil {
		log.Error(err, "failed to create scope")
		r.Recorder.Eventf(azureCluster, corev1.EventTypeWarning, "CreateClusterScopeFailed", "failed to create scope")
		return reconcile.Result{}, microerror.Mask(err)
	}

	defer func() {
		if err := clusterScope.Close(ctx); err != nil && reterr == nil {
			reterr = microerror.Mask(err)
		}
	}()

	// Handle deleted clusters
	if !cluster.DeletionTimestamp.IsZero() || !azureCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, clusterScope)
	}

	// Handle non-deleted clusters
	//
	// only act on Clusters where the LoadBalancersReady condition is true
	clusterConditions := clusterScope.AzureCluster.GetConditions()
	for _, condition := range clusterConditions {
		if condition.Type == capz.LoadBalancersReadyCondition {
			return r.reconcileNormal(ctx, clusterScope)
		}
	}

	return reconcile.Result{}, nil
}

func (r *AzureClusterReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capz.AzureCluster{}).
		WithOptions(options).
		Complete(r)
}

func (r *AzureClusterReconciler) reconcileNormal(ctx context.Context, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling AzureCluster DNS zones")

	cluster := clusterScope.Cluster
	azureCluster := clusterScope.AzureCluster

	var err error

	// If the AzureCluster doesn't has our finalizer, add it.
	if !controllerutil.ContainsFinalizer(azureCluster, AzureClusterControllerFinalizer) {
		controllerutil.AddFinalizer(azureCluster, AzureClusterControllerFinalizer)
		// Register the finalizer immediately to avoid orphaning Azure resources on delete
		if err := clusterScope.PatchObject(ctx); err != nil {
			return reconcile.Result{}, err
		}
	}

	// If a cluster isn't provisioned we don't need to reconcile it
	// as not all information for creating DNS records are available yet.
	if cluster.Status.Phase != string(capi.ClusterPhaseProvisioned) {
		log.Info(fmt.Sprintf("Requeuing cluster %s - phase %s", cluster.Name, cluster.Status.Phase))
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
	}

	// Reconcile workload cluster DNS records
	publicIPsService := publicips.New(clusterScope)

	azureClusterIdentity := &capz.AzureClusterIdentity{}
	log.V(1).Info(fmt.Sprintf("try to get the clusterClusterIdentity - %s", clusterScope.AzureCluster.Spec.IdentityRef.Name))

	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      clusterScope.AzureCluster.Spec.IdentityRef.Name,
		Namespace: clusterScope.AzureCluster.Spec.IdentityRef.Namespace,
	}, azureClusterIdentity)
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
		"spec.clientID", azureClusterIdentity.Spec.TenantID,
	)

	staticServicePrincipalSecret := &corev1.Secret{}
	if azureClusterIdentity.Spec.Type == capz.ManualServicePrincipal {
		log.V(1).Info(fmt.Sprintf("try to get the referenced secret - %s/%s", azureClusterIdentity.Spec.ClientSecret.Namespace, azureClusterIdentity.Spec.ClientSecret.Name))

		err = r.Client.Get(ctx, types.NamespacedName{
			Name:      azureClusterIdentity.Spec.ClientSecret.Name,
			Namespace: azureClusterIdentity.Spec.ClientSecret.Namespace,
		}, staticServicePrincipalSecret)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.V(1).Info("static service principal secret object was not found", "error", err)
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, microerror.Mask(err)
		}
	}

	params := scope.DNSScopeParams{
		ClusterScope:                       *clusterScope,
		AzureClusterIdentity:               *azureClusterIdentity,
		AzureClusterServicePrincipalSecret: *staticServicePrincipalSecret,
		BaseDomain:                         r.BaseDomain,
		BaseDomainResourceGroup:            r.BaseDomainResourceGroup,
		BaseZoneCredentials: scope.BaseZoneCredentials{
			ClientID:       r.BaseZoneClientID,
			ClientSecret:   r.BaseZoneClientSecret,
			SubscriptionID: r.BaseZoneSubscriptionID,
			TenantID:       r.BaseZoneTenantID,
		},
	}

	// add the bastionIP from the annotations
	clusterAnnotations := azureCluster.GetAnnotations()
	if clusterAnnotations[BastionHostIPAnnotation] != "" {
		log.V(1).Info("bastion host annotation is not empty")
		params.BastionIP = clusterAnnotations[BastionHostIPAnnotation]
	}

	// `azureCluster.spec.networkSpec.apiServerLB.privateLinks` is the current identifier
	// for private DNS zone creation in the management cluster
	if len(azureCluster.Spec.NetworkSpec.APIServerLB.PrivateLinks) > 0 {

		managementCluster, err := r.getManagementAzureClusterCR(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterAzureIdentity, err := r.getManagementClusterIdentity(ctx, managementCluster)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterStaticServicePrincipalSecret := &corev1.Secret{}
		if managementClusterAzureIdentity.Spec.Type == capz.ManualServicePrincipal {
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

		privateParams := scope.PrivateDNSScopeParams{
			BaseDomain:                              r.BaseDomain,
			ClusterName:                             azureCluster.GetName(),
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

		privateDnsScope, err := scope.NewPrivateDNSScope(ctx, privateParams)
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

	dnsScope, err := scope.NewDNSScope(ctx, params)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	dnsService, err := dns.New(*dnsScope, publicIPsService)
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

	log.Info("Successfully reconciled AzureCluster DNS zones")
	return reconcile.Result{}, nil
}

func (r *AzureClusterReconciler) reconcileDelete(ctx context.Context, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	deletedMetrics := 0

	log.Info("Reconciling AzureCluster DNS zones delete")

	params := scope.DNSScopeParams{
		ClusterScope:            *clusterScope,
		BaseDomain:              r.BaseDomain,
		BaseDomainResourceGroup: r.BaseDomainResourceGroup,
		BaseZoneCredentials: scope.BaseZoneCredentials{
			ClientID:       r.BaseZoneClientID,
			ClientSecret:   r.BaseZoneClientSecret,
			SubscriptionID: r.BaseZoneSubscriptionID,
			TenantID:       r.BaseZoneTenantID,
		},
	}

	azureCluster := clusterScope.AzureCluster
	// `azureCluster.spec.networkSpec.apiServerLB.privateLinks` is the current identifier
	// for private DNS zone creation in the management cluster
	if len(azureCluster.Spec.NetworkSpec.APIServerLB.PrivateLinks) > 0 {

		managementCluster, err := r.getManagementAzureClusterCR(ctx)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterAzureIdentity, err := r.getManagementClusterIdentity(ctx, managementCluster)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		managementClusterStaticServicePrincipalSecret := &corev1.Secret{}
		if managementClusterAzureIdentity.Spec.Type == capz.ManualServicePrincipal {
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

		privateParams := scope.PrivateDNSScopeParams{
			BaseDomain:                              r.BaseDomain,
			ClusterName:                             clusterScope.Cluster.Name,
			ManagementClusterSpec:                   managementCluster.Spec,
			ManagementClusterAzureIdentity:          *managementClusterAzureIdentity,
			ManagementClusterServicePrincipalSecret: *managementClusterStaticServicePrincipalSecret,
		}

		privateDnsScope, err := scope.NewPrivateDNSScope(ctx, privateParams)
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
			fmt.Sprintf("%s.%s", clusterScope.ClusterName(), r.BaseDomain),
			metrics.ZoneTypePrivate,
		)
	}

	dnsScope, err := scope.NewDNSScope(ctx, params)
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
	if controllerutil.ContainsFinalizer(clusterScope.AzureCluster, AzureClusterControllerFinalizer) {
		controllerutil.RemoveFinalizer(clusterScope.AzureCluster, AzureClusterControllerFinalizer)
	}

	deletedMetrics += deleteClusterMetrics(
		fmt.Sprintf("%s.%s", clusterScope.ClusterName(), r.BaseDomain),
		metrics.ZoneTypePublic,
	)
	log.V(1).Info(fmt.Sprintf("%d metrics for cluster %s got deleted", deletedMetrics, clusterScope.ClusterName()))

	log.Info("Successfully reconciled AzureCluster DNS zones delete")
	return reconcile.Result{}, nil
}

func (r *AzureClusterReconciler) getManagementAzureClusterCR(ctx context.Context) (*capz.AzureCluster, error) {

	log := log.FromContext(ctx)
	log.V(1).Info(fmt.Sprintf("try to get the azureCluster - %s/%s", r.ManagementClusterNamespace, r.ManagementClusterName))

	managementCluster := &capz.AzureCluster{}

	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      r.ManagementClusterName,
		Namespace: r.ManagementClusterNamespace,
	}, managementCluster)

	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("azureCluster was not found", "error", err)
			return managementCluster, nil
		}
		return managementCluster, microerror.Mask(err)
	}

	log.V(1).Info("azureManagementCluster information",
		"spec.Type", managementCluster.Spec.IdentityRef.Name,
		"spec.subscriptionID", managementCluster.Spec.SubscriptionID,
	)

	return managementCluster, nil

}

func (r *AzureClusterReconciler) getManagementClusterIdentity(ctx context.Context, managementCluster *capz.AzureCluster) (*capz.AzureClusterIdentity, error) {

	log := log.FromContext(ctx)
	log.V(1).Info(fmt.Sprintf("try to get the azureClusterIdentity - %s/%s", managementCluster.Spec.IdentityRef.Namespace, managementCluster.Spec.IdentityRef.Name))

	managementClusterAzureIdentity := &capz.AzureClusterIdentity{}

	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      managementCluster.Spec.IdentityRef.Name,
		Namespace: managementCluster.Spec.IdentityRef.Namespace,
	}, managementClusterAzureIdentity)

	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("azureClusterIdentity was not found", "error", err)
			return managementClusterAzureIdentity, nil
		}
		return managementClusterAzureIdentity, microerror.Mask(err)
	}

	log.V(1).Info("azureClusterIdentity information",
		"spec.Type", managementClusterAzureIdentity.Spec.Type,
		"spec.tenantID", managementClusterAzureIdentity.Spec.TenantID,
		"spec.clientID", managementClusterAzureIdentity.Spec.TenantID,
	)

	return managementClusterAzureIdentity, nil

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
