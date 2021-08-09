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

	azuredns "github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/cloud"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/cloud/scope"
	"sigs.k8s.io/cluster-api-provider-azure/cloud/services/publicips"
	"sigs.k8s.io/cluster-api-provider-azure/util/reconciler"
	capi "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/giantswarm/dns-operator-azure/azure"
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
	Micrologger      micrologger.Logger
	Recorder         record.EventRecorder
	ReconcileTimeout time.Duration
	Scheme           *runtime.Scheme
}

// NewAzureClusterReconciler returns a new AzureClusterReconciler instance
func NewAzureClusterReconciler(client client.Client, micrologger micrologger.Logger, recorder record.EventRecorder, reconcileTimeout time.Duration) *AzureClusterReconciler {
	acr := &AzureClusterReconciler{
		Client:           client,
		Micrologger:      micrologger,
		Recorder:         recorder,
		ReconcileTimeout: reconcileTimeout,
	}

	return acr
}

func (r *AzureClusterReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), reconciler.DefaultedLoopTimeout(r.ReconcileTimeout))
	defer cancel()

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
	if !azureCluster.DeletionTimestamp.IsZero() {
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
		WithEventFilter(predicates.ResourceNotPaused(logger)).
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
	azureCluster := clusterScope.AzureCluster
	var err error
	var nsRecordSetSpecs []azure.NSRecordSetSpec

	// If the AzureCluster doesn't have our finalizer, add it.
	controllerutil.AddFinalizer(azureCluster, AzureClusterControllerFinalizer)

	// Reconcile workload cluster DNS records
	nsRecordSetSpecs, err = r.reconcileNormalWorkloadCluster(ctx, clusterScope)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	// Reconcile management cluster DNS records
	err = r.reconcileNormalManagementCluster(ctx, clusterScope, nsRecordSetSpecs)
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

func (r *AzureClusterReconciler) reconcileNormalWorkloadCluster(ctx context.Context, clusterScope *capzscope.ClusterScope) ([]azure.NSRecordSetSpec, error) {
	publicIPsService := publicips.New(clusterScope)
	clusterScopeWrapper, err := scope.NewClusterScopeWrapper(*clusterScope)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	dnsService := dns.New(clusterScopeWrapper, publicIPsService)

	err = dnsService.Reconcile(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return clusterScopeWrapper.DNSSpec().NSRecordSets, nil
}

func (r *AzureClusterReconciler) reconcileNormalManagementCluster(ctx context.Context, clusterScope *capzscope.ClusterScope, nsRecordSetSpecs []azure.NSRecordSetSpec) error {
	var err error
	var managementClusterDNSService *dns.Service
	{
		var managementClusterScope *scope.ManagementClusterScope
		{
			params := scope.ManagementClusterScopeParams{
				Client:                          clusterScope.Client,
				Logger:                          clusterScope.Logger,
				WorkloadClusterName:             clusterScope.ClusterName(),
				WorkloadClusterNSRecordSetSpecs: nsRecordSetSpecs,
			}
			managementClusterScope, err = scope.NewManagementClusterScope(ctx, params)
			if err != nil {
				return microerror.Mask(err)
			}
		}

		managementClusterDNSService = dns.New(managementClusterScope, nil)
	}

	// Reconcile management cluster DNS records
	err = managementClusterDNSService.Reconcile(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *AzureClusterReconciler) reconcileDelete(ctx context.Context, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	clusterScope.Info("Reconciling AzureCluster DNS zones delete")

	err := r.reconcileDeleteWorkloadCluster(ctx, clusterScope)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	err = r.reconcileDeleteManagementCluster(ctx, clusterScope)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}

	controllerutil.RemoveFinalizer(clusterScope.AzureCluster, AzureClusterControllerFinalizer)
	clusterScope.Info("Successfully reconciled AzureCluster DNS zones delete")
	return reconcile.Result{}, nil
}

func (r *AzureClusterReconciler) reconcileDeleteWorkloadCluster(ctx context.Context, clusterScope *capzscope.ClusterScope) error {
	clusterScopeWrapper, err := scope.NewClusterScopeWrapper(*clusterScope)
	if err != nil {
		return microerror.Mask(err)
	}

	dnsZoneName := clusterScopeWrapper.DNSSpec().ZoneName
	clusterScope.Info("Deleting DNS zone", "DNSZone", dnsZoneName)

	dnsService := dns.New(clusterScopeWrapper, nil)
	err = dnsService.DeleteZone(ctx, clusterScope.ResourceGroup(), dnsZoneName)
	if capzazure.ResourceNotFound(err) {
		clusterScope.Info("Azure DNS zone resource has already been deleted")
	} else if err != nil {
		return microerror.Mask(err)
	}

	clusterScope.Info("Successfully deleted DNS zone", "DNSZone", dnsZoneName)
	return nil
}

func (r *AzureClusterReconciler) reconcileDeleteManagementCluster(ctx context.Context, clusterScope *capzscope.ClusterScope) error {
	nsRecordSetName := fmt.Sprintf("%s.k8s", clusterScope.ClusterName())
	clusterScope.Info("Deleting DNS NS record", "NSRecord", nsRecordSetName)

	var err error
	var managementClusterDNSService *dns.Service
	var zoneName string
	{
		var managementClusterScope *scope.ManagementClusterScope
		{
			params := scope.ManagementClusterScopeParams{
				Client:                          clusterScope.Client,
				Logger:                          clusterScope.Logger,
				WorkloadClusterName:             clusterScope.ClusterName(),
				WorkloadClusterNSRecordSetSpecs: []azure.NSRecordSetSpec{},
			}
			managementClusterScope, err = scope.NewManagementClusterScope(ctx, params)
			if err != nil {
				return microerror.Mask(err)
			}
		}

		managementClusterDNSService = dns.New(managementClusterScope, nil)
		zoneName = managementClusterScope.DNSSpec().ZoneName
	}

	// Reconcile management cluster DNS records
	err = managementClusterDNSService.DeleteRecordSet(ctx, clusterScope.ResourceGroup(), zoneName, azuredns.NS, nsRecordSetName)
	if capzazure.ResourceNotFound(err) {
		clusterScope.Info("Azure DNS record set has already been deleted")
	} else if err != nil {
		return microerror.Mask(err)
	}

	clusterScope.Info("Successfully deleted DNS NS record", "NSRecord", nsRecordSetName)
	return nil
}
