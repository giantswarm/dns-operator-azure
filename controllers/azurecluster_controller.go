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
)

const (
	AzureClusterControllerFinalizer string = "dns-operator-azure.giantswarm.io/azurecluster"
	BastionHostIPAnnotation         string = "dns-operator-azure.giantswarm.io/bastion-ip"
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
