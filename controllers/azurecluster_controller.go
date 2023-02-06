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
	"time"

	"github.com/giantswarm/microerror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"

	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/giantswarm/dns-operator-azure/azure/scope"
	"github.com/giantswarm/dns-operator-azure/azure/services/dns"
)

const (
	AzureClusterControllerFinalizer string = "dns-operator-azure.giantswarm.io/azurecluster"
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

func (r *AzureClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capz.AzureCluster{}).
		Complete(r)
}

func (r *AzureClusterReconciler) reconcileNormal(ctx context.Context, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling AzureCluster DNS zones")

	cluster := clusterScope.Cluster
	azureCluster := clusterScope.AzureCluster

	var err error

	// If the AzureCluster doesn't have our finalizer, add it.
	if !controllerutil.ContainsFinalizer(azureCluster, AzureClusterControllerFinalizer) {
		controllerutil.AddFinalizer(azureCluster, AzureClusterControllerFinalizer)
		// Register the finalizer immediately to avoid orphaning cluster resources on delete
		if err := r.Update(ctx, azureCluster); err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}
	}

	// If a cluster isn't provisioned we don't need to reconcile it
	// as not all information for creating DNS records are available yet.
	if cluster.Status.Phase != string(capi.ClusterPhaseProvisioned) {
		log.Info("Requeuing cluster %s - phase %s, ", cluster.Name, cluster.Status.Phase)
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
	}

	// Reconcile workload cluster DNS records
	publicIPsService := publicips.New(clusterScope)

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

	dnsScope, err := scope.NewDNSScope(ctx, params)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	dnsService, err := dns.New(*dnsScope, publicIPsService)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	err = dnsService.Reconcile(ctx)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	log.Info("Successfully reconciled AzureCluster DNS zones")
	return reconcile.Result{}, nil
}

func (r *AzureClusterReconciler) reconcileDelete(ctx context.Context, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	log := log.FromContext(ctx)

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

	log.Info("Successfully reconciled AzureCluster DNS zones delete")
	return reconcile.Result{}, nil
}
