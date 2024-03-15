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

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	aadpodv1 "github.com/Azure/aad-pod-identity/pkg/apis/aadpodidentity/v1"
	"github.com/giantswarm/microerror"
	"go.uber.org/zap/zapcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/giantswarm/dns-operator-azure/v2/controllers"
	"github.com/giantswarm/dns-operator-azure/v2/pkg/errors"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	SubscriptionId = "AZURE_SUBSCRIPTION_ID"
	TenantId       = "AZURE_TENANT_ID"
	ClientId       = "AZURE_CLIENT_ID"
	ClientSecret   = "AZURE_CLIENT_SECRET" //nolint
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = capi.AddToScheme(scheme)
	_ = infrav1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme

	// Add aadpodidentity v1 to the scheme.
	scheme.AddKnownTypes(schema.GroupVersion{Group: aadpodv1.GroupName, Version: "v1"},
		&aadpodv1.AzureIdentity{},
		&aadpodv1.AzureIdentityList{},
		&aadpodv1.AzureIdentityBinding{},
		&aadpodv1.AzureIdentityBindingList{},
		&aadpodv1.AzurePodIdentityException{},
		&aadpodv1.AzurePodIdentityExceptionList{},
	)
	metav1.AddToGroupVersion(scheme, aadpodv1.SchemeGroupVersion)
}

func main() {
	err := mainError()
	if err != nil {
		panic(fmt.Sprintf("%#v\n", err))
	}
}

func mainError() error {
	var (
		metricsAddr                string
		enableLeaderElection       bool
		baseDomain                 string
		baseDomainResourceGroup    string
		baseZoneClientID           string
		baseZoneClientSecret       string
		baseZoneSubscriptionID     string
		baseZoneTenantID           string
		syncPeriod                 time.Duration
		clusterConcurrency         int
		managementClusterName      string
		managementClusterNamespace string
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")

	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&baseDomain, "base-domain", "",
		"Domain for which to create the DNS entries, e.g. customer.gigantic.io.")
	flag.StringVar(&baseDomainResourceGroup, "base-domain-resource-group", "",
		"Resource Group where the base-domain is placed.")
	flag.DurationVar(&syncPeriod, "sync-period", 5*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)")
	flag.IntVar(&clusterConcurrency, "cluster-concurrency", 5,
		"Number of clusters to process simultaneously")
	flag.StringVar(&managementClusterName, "management-cluster-name", "",
		"The name of the management cluster where this operator is running (also MC AzureCluster CR name)")
	flag.StringVar(&managementClusterNamespace, "management-cluster-namespace", "",
		"The namespace where the management cluster AzureCluster CR is deployed")

	// configure the logger
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	restConfig := ctrl.GetConfigOrDie()
	restConfig.UserAgent = "dns-operator-azure"
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "dns-operator-azure-leader-election",
		SyncPeriod:         &syncPeriod,
	})
	if err != nil {
		setupLog.Error(errors.FatalError, "unable to start manager")
		return microerror.Mask(err)
	}

	// Initialize event recorder.
	record.InitFromRecorder(mgr.GetEventRecorderFor("dns-operator-azure"))

	baseZoneSubscriptionID = os.Getenv(SubscriptionId)
	if baseZoneSubscriptionID == "" {
		return microerror.Mask(fmt.Errorf("environment variable %s not set", SubscriptionId))
	}
	baseZoneClientID = os.Getenv(ClientId)
	if baseZoneClientID == "" {
		return microerror.Mask(fmt.Errorf("environment variable %s not set", ClientId))
	}
	baseZoneClientSecret = os.Getenv(ClientSecret)
	if baseZoneClientSecret == "" {
		return microerror.Mask(fmt.Errorf("environment variable %s not set", ClientSecret))
	}
	baseZoneTenantID = os.Getenv(TenantId)
	if baseZoneTenantID == "" {
		return microerror.Mask(fmt.Errorf("environment variable %s not set", TenantId))
	}

	if err := (&controllers.ClusterReconcilerx{
		Client:                     mgr.GetClient(),
		BaseDomain:                 baseDomain,
		BaseDomainResourceGroup:    baseDomainResourceGroup,
		BaseZoneClientID:           baseZoneClientID,
		BaseZoneClientSecret:       baseZoneClientSecret,
		BaseZoneSubscriptionID:     baseZoneSubscriptionID,
		BaseZoneTenantID:           baseZoneTenantID,
		Recorder:                   mgr.GetEventRecorderFor("azurecluster-reconciler"),
		ManagementClusterName:      managementClusterName,
		ManagementClusterNamespace: managementClusterNamespace,
	}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: clusterConcurrency}); err != nil {
		setupLog.Error(errors.FatalError, "unable to create controller AzureCluster")
		return microerror.Mask(err)
	}

	if err = (&controllers.AzureMachineReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("azuremachine-reconciler"),
	}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: clusterConcurrency}); err != nil {
		setupLog.Error(errors.FatalError, "unable to create controller AzureMachine")
		return microerror.Mask(err)
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(errors.FatalError, "problem running manager")
		return microerror.Mask(err)
	}

	return nil
}
