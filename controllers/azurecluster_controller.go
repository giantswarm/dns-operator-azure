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
	"github.com/giantswarm/micrologger"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha4"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"

	capi "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/giantswarm/dns-operator-azure/azure/scope"
	"github.com/giantswarm/dns-operator-azure/azure/services/dns"
	"github.com/giantswarm/dns-operator-azure/pkg/micrologr"
)

const (
	AzureClusterControllerFinalizer string             = "dns-operator-azure.giantswarm.io/azurecluster"
	DNSZoneReady                    capi.ConditionType = "DNSZoneReady"
)

// AzureClusterReconciler reconciles a AzureCluster object
type AzureClusterReconciler struct {
	client.Client

	BaseDomain             string
	BaseZoneClientID       string
	BaseZoneClientSecret   string
	BaseZoneSubscriptionID string
	BaseZoneTenantID       string
	Micrologger            micrologger.Logger
	Recorder               record.EventRecorder
	WatchFilterValue       string
}

func (r *AzureClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log, err := r.getLogger(ctx, "namespace", req.Namespace, "azureCluster", req.Name)
	if err != nil {
		return ctrl.Result{}, microerror.Mask(err)
	}

	// Fetch the AzureCluster instance
	azureCluster := &capz.AzureCluster{}
	err = r.Get(ctx, req.NamespacedName, azureCluster)
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
		r.Recorder.Eventf(azureCluster, corev1.EventTypeNormal, "OwnerRefNotSet", "Cluster Controller has not yet set OwnerRef")
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
		Logger:       log,
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
	return r.reconcileNormal(ctx, clusterScope)
}

func (r *AzureClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	logger, err := r.getLogger(ctx, "controller", "AzureCluster")
	if err != nil {
		return microerror.Mask(err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capz.AzureCluster{}).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(logger, r.WatchFilterValue)).
		Complete(r)
}

func (r *AzureClusterReconciler) getLogger(ctx context.Context, keysAndValues ...interface{}) (logr.Logger, error) {
	var err error
	var log logr.Logger
	{
		c := micrologr.Config{
			Context:     ctx,
			Micrologger: r.Micrologger,
			Enabled:     true,
		}
		log, err = micrologr.NewLogger(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		log = log.WithValues(keysAndValues...)
	}

	return log, nil
}

func (r *AzureClusterReconciler) reconcileNormal(ctx context.Context, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	clusterScope.Info("Reconciling AzureCluster DNS zones")

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
		clusterScope.Info("Requeuing cluster %s - phase %s, ", cluster.Name, cluster.Status.Phase)
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
	}

	// Reconcile workload cluster DNS records
	publicIPsService := publicips.New(clusterScope)

	var dnsScope *scope.DNSScope
	{
		params := scope.DNSScopeParams{
			ClusterScope: *clusterScope,
			BaseDomain:   r.BaseDomain,
			BaseZoneCredentials: scope.BaseZoneCredentials{
				ClientID:       r.BaseZoneClientID,
				ClientSecret:   r.BaseZoneClientSecret,
				SubscriptionID: r.BaseZoneSubscriptionID,
				TenantID:       r.BaseZoneTenantID,
			},
		}

		dnsScope, err = scope.NewDNSScope(ctx, params)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}
	}

	dnsService, err := dns.New(*dnsScope, publicIPsService)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	err = dnsService.Reconcile(ctx)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	// Update DNSZoneReady condition in AzureCluster
	conditions.MarkTrue(azureCluster, DNSZoneReady)
	err = r.Client.Status().Update(ctx, azureCluster)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	clusterScope.Info("Successfully reconciled AzureCluster DNS zones")
	return reconcile.Result{}, nil
}

func (r *AzureClusterReconciler) reconcileDelete(ctx context.Context, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	clusterScope.Info("Reconciling AzureCluster DNS zones delete")

	var err error
	var dnsScope *scope.DNSScope
	{
		params := scope.DNSScopeParams{
			ClusterScope: *clusterScope,
			BaseDomain:   r.BaseDomain,
			BaseZoneCredentials: scope.BaseZoneCredentials{
				ClientID:       r.BaseZoneClientID,
				ClientSecret:   r.BaseZoneClientSecret,
				SubscriptionID: r.BaseZoneSubscriptionID,
				TenantID:       r.BaseZoneTenantID,
			},
		}

		dnsScope, err = scope.NewDNSScope(ctx, params)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}
	}

	dnsService, err := dns.New(*dnsScope, nil)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	err = dnsService.ReconcileDelete(ctx)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	if controllerutil.ContainsFinalizer(clusterScope.AzureCluster, AzureClusterControllerFinalizer) {
		controllerutil.RemoveFinalizer(clusterScope.AzureCluster, AzureClusterControllerFinalizer)
	}

	clusterScope.Info("Successfully reconciled AzureCluster DNS zones delete")
	return reconcile.Result{}, nil
}

// func (r *AzureClusterReconciler) reconcileDeleteWorkloadClusterRecords(ctx context.Context, clusterScope *capzscope.ClusterScope) error {
// 	clusterScopeWrapper, err := scope.NewClusterScopeWrapper(*clusterScope)
// 	if err != nil {
// 		return microerror.Mask(err)
// 	}

// 	zoneName := clusterScopeWrapper.ClusterName()
// 	clusterScope.Info("Deleting DNS zone in workload cluster", "DNSZone", zoneName)

// 	dnsService := dns.New(clusterScopeWrapper, nil)
// 	err = dnsService.DeleteZone(ctx, clusterScope.ResourceGroup(), zoneName)
// 	if azure.IsParentResourceNotFound(err) {
// 		clusterScope.Info("Cannot delete DNS zone in workload cluster, resource group not found", "resourceGroup", clusterScope.ResourceGroup(), "DNSZone", zoneName, "error", err.Error())
// 	} else if capzazure.ResourceNotFound(err) {
// 		clusterScope.Info("Azure DNS zone resource has already been deleted")
// 	} else if err != nil {
// 		return microerror.Mask(err)
// 	}

// 	clusterScope.Info("Successfully deleted DNS zone in workload cluster", "DNSZone", zoneName)
// 	return nil
// }

// func (r *AzureClusterReconciler) reconcileDeleteManagementClusterRecords(ctx context.Context, clusterScope *capzscope.ClusterScope) error {
// 	nsRecordSetName := fmt.Sprintf("%s.k8s", clusterScope.ClusterName())

// 	var err error
// 	var managementClusterScope *scope.ManagementClusterScope
// 	var managementClusterDNSService *dns.Service
// 	var zoneName string
// 	{
// 		{
// 			params := scope.ManagementClusterScopeParams{
// 				Client:                          clusterScope.Client,
// 				Logger:                          clusterScope.Logger,
// 				WorkloadClusterName:             clusterScope.ClusterName(),
// 				WorkloadClusterNSRecordSetSpecs: []azure.NSRecordSetSpec{},
// 			}
// 			managementClusterScope, err = scope.NewManagementClusterScope(ctx, params)
// 			if err != nil {
// 				return microerror.Mask(err)
// 			}
// 		}

// 		managementClusterDNSService = dns.New(managementClusterScope, nil)
// 		zoneName = managementClusterScope.DNSSpec().ZoneName
// 	}

// 	clusterScope.Info("Deleting DNS NS record in management cluster", "DNSZone", zoneName, "NSRecord", nsRecordSetName)

// 	// Reconcile management cluster DNS records
// 	err = managementClusterDNSService.DeleteRecordSet(ctx, managementClusterScope.ResourceGroup(), zoneName, azuredns.NS, nsRecordSetName)
// 	if azure.IsParentResourceNotFound(err) {
// 		clusterScope.Info("DNS zone not found", "DNSZone", zoneName, "error", err.Error())
// 	} else if capzazure.ResourceNotFound(err) {
// 		clusterScope.Info("Azure NS record not found", "DNSZone", zoneName, "NSRecord", nsRecordSetName, "error", err.Error())
// 	} else if err != nil {
// 		return microerror.Mask(err)
// 	}

// 	clusterScope.Info("Successfully deleted DNS NS record in management cluster", "DNSZone", zoneName, "NSRecord", nsRecordSetName)
// 	return nil
// }
