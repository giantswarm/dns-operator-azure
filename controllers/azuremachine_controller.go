package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	AzureMachineControllerFinalizer string = "dns-operator-azure.giantswarm.io/azuremachine"
)

type AzureMachineReconciler struct {
	client.Client

	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azureclusters,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;update;patch

func (r *AzureMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {

	log := log.FromContext(ctx)

	// Fetch the AzureMachine instance
	azureMachine := &infrav1.AzureMachine{}
	err := r.Get(ctx, req.NamespacedName, azureMachine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("object was not found")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, microerror.Mask(err)
	}

	// Fetch the Cluster.
	machine, err := util.GetOwnerMachine(ctx, r.Client, azureMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, microerror.Mask(err)
	}
	if machine == nil {
		log.Info("Machine Controller has not yet set OwnerRef")
		return reconcile.Result{}, microerror.Mask(err)
	}

	// only continue reconcilation if the bastion label is set
	azureMachineLabels := azureMachine.GetLabels()
	// TODO: change the role label as `cluster.x-k8s.io/role` sounds very CAPI common (which is not)
	if azureMachineLabels["cluster.x-k8s.io/role"] == "bastion" {

		cluster, err := util.GetClusterFromMetadata(ctx, r.Client, azureMachine.ObjectMeta)
		if err != nil {
			return reconcile.Result{}, microerror.Mask(err)
		}

		// Return early if the objects or Cluster is paused.
		if annotations.IsPaused(cluster, azureMachine) || annotations.IsPaused(cluster, machine) {
			log.Info("Machine, AzureMachine or linked Cluster is marked as paused. Won't reconcile")
			return ctrl.Result{}, nil
		}

		// Create the scope.
		machineScope, err := capzscope.NewMachineScope(capzscope.MachineScopeParams{
			Client:       r.Client,
			Machine:      machine,
			AzureMachine: azureMachine,
		})
		if err != nil {
			log.Error(err, "failed to create machine scope")
			return reconcile.Result{}, microerror.Mask(err)
		}

		defer func() {
			if err := machineScope.Close(ctx); err != nil && reterr == nil {
				reterr = microerror.Mask(err)
			}
		}()

		azureCluster := &infrav1.AzureCluster{}
		log.V(1).Info(fmt.Sprintf("try to get the cluster - %s", cluster.Spec.InfrastructureRef.Name))

		err = r.Client.Get(ctx, types.NamespacedName{
			Name:      cluster.Spec.InfrastructureRef.Name,
			Namespace: cluster.Spec.InfrastructureRef.Namespace,
		}, azureCluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.V(1).Info("cluster object was not found", "error", err)
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, microerror.Mask(err)
		}

		// Create the cluster scope
		// needed to get cluster credentials to act on clusters domain
		clusterScope, err := capzscope.NewClusterScope(ctx, capzscope.ClusterScopeParams{
			Client:       r.Client,
			Cluster:      cluster,
			AzureCluster: azureCluster,
		})
		if err != nil {
			log.Error(err, "failed to create cluster scope")
			return reconcile.Result{}, microerror.Mask(err)
		}

		defer func() {
			if err := clusterScope.Close(ctx); err != nil && reterr == nil {
				reterr = microerror.Mask(err)
			}
		}()

		// Handle deleted machines
		if !machine.DeletionTimestamp.IsZero() || !azureMachine.DeletionTimestamp.IsZero() {
			return r.reconcileDelete(ctx, machineScope, clusterScope)
		}

		// only act on Machines where the NetworkInterfacesReady condition is true
		azureMachineConditions := machineScope.AzureMachine.GetConditions()
		for _, condition := range azureMachineConditions {
			if condition.Type == infrav1.NetworkInterfaceReadyCondition {
				log.V(1).Info("machine has NetworkInterfacesReady condition", "machine", machineScope.AzureMachine.Name)
				return r.reconcileNormal(ctx, machineScope, clusterScope)
			}
		}
	}

	log.V(1).Info("machine doesn't met required conditions and labels", "machine", azureMachine.Name)

	return reconcile.Result{}, nil
}

func (r *AzureMachineReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureMachine{}).
		WithOptions(options).
		Complete(r)
}

func (r *AzureMachineReconciler) reconcileNormal(ctx context.Context, azureMachineScope *capzscope.MachineScope, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling Bastion IP discovery")

	// If the AzureCluster doesn't has our finalizer, add it.
	if !controllerutil.ContainsFinalizer(azureMachineScope.AzureMachine, AzureMachineControllerFinalizer) {
		controllerutil.AddFinalizer(azureMachineScope.AzureMachine, AzureMachineControllerFinalizer)
		// Register the finalizer immediately to avoid orphaning cluster resources on delete
		if err := azureMachineScope.PatchObject(ctx); err != nil {
			return reconcile.Result{}, err
		}
	}

	bastionIP := getClusterBastionIPsFromAnnotation(ctx, clusterScope)

	for _, addr := range azureMachineScope.AzureMachine.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			bastionIP = append(bastionIP, addr.Address)
		}
	}

	// remove duplicate entries (if there are any)
	bastionIP = slices.Compact(bastionIP)

	clusterScope.SetAnnotation(BastionHostIPAnnotation, strings.Join(bastionIP, ","))

	log.Info("Successfully reconciled Bastion IP discovery")

	return reconcile.Result{}, nil
}

func (r *AzureMachineReconciler) reconcileDelete(ctx context.Context, azureMachineScope *capzscope.MachineScope, clusterScope *capzscope.ClusterScope) (reconcile.Result, error) {

	log := log.FromContext(ctx)
	log.Info("Reconciling Bastion IP deletion")

	bastionIP := getClusterBastionIPsFromAnnotation(ctx, clusterScope)

	for _, addr := range azureMachineScope.AzureMachine.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			bastionIPIndex := slices.Index(bastionIP, addr.Address)
			if bastionIPIndex != -1 {
				bastionIP = slices.Delete(bastionIP, bastionIPIndex, bastionIPIndex+1)
			}
		}
	}

	clusterScope.SetAnnotation(BastionHostIPAnnotation, strings.Join(bastionIP, ","))

	// remove finalizer
	if controllerutil.ContainsFinalizer(azureMachineScope.AzureMachine, AzureMachineControllerFinalizer) {
		controllerutil.RemoveFinalizer(azureMachineScope.AzureMachine, AzureMachineControllerFinalizer)
	}

	log.Info("Successfully reconciled Bastion IP deletion")

	return reconcile.Result{}, nil
}

func getClusterBastionIPsFromAnnotation(ctx context.Context, clusterScope *capzscope.ClusterScope) []string {
	azureClusterAnnotations := clusterScope.AzureCluster.GetAnnotations()
	if azureClusterAnnotations[BastionHostIPAnnotation] != "" {
		return strings.Split(azureClusterAnnotations[BastionHostIPAnnotation], ",")
	}

	return nil
}
