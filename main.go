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

	aadpodv1 "github.com/Azure/aad-pod-identity/pkg/apis/aadpodidentity/v1"
	"github.com/giantswarm/microerror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/giantswarm/dns-operator-azure/controllers"
	"github.com/giantswarm/dns-operator-azure/pkg/errors"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = capi.AddToScheme(scheme)
	_ = capz.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme

	// Add aadpodidentity v1 to the scheme.
	scheme.AddKnownTypes(aadpodv1.SchemeGroupVersion,
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
		metricsAddr            string
		enableLeaderElection   bool
		watchFilterValue       string
		baseDomain             string
		baseZoneClientID       string
		baseZoneClientSecret   string
		baseZoneSubscriptionID string
		baseZoneTenantID       string
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")

	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&baseDomain, "base-domain", "", "Domain for which to create the DNS entries, e.g. customer.gigantic.io.")
	flag.StringVar(&baseZoneClientID, "base-zone-client-id", "", "Client ID to access the base DNS domain.")
	flag.StringVar(&baseZoneClientSecret, "base-zone-client-secret", "", "Client secret to access the base DNS domain.")
	flag.StringVar(&baseZoneSubscriptionID, "base-zone-subscription-id", "", "Subscription ID of the base DNS domain.")
	flag.StringVar(&baseZoneTenantID, "base-zone-tenant-id", "", "Tenant ID of the base DNS domain.")

	flag.StringVar(
		&watchFilterValue,
		"watch-filter",
		"",
		fmt.Sprintf("Label value that the controller watches to reconcile cluster-api objects. Label key is always %s. If unspecified, the controller watches for all cluster-api objects.", capi.WatchLabel),
	)

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	restConfig := ctrl.GetConfigOrDie()
	restConfig.UserAgent = "dns-operator-azure"
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "2af49e02.giantswarm.io",
	})
	if err != nil {
		setupLog.Error(errors.FatalError, "unable to start manager")
		return microerror.Mask(err)
	}

	// Initialize event recorder.
	record.InitFromRecorder(mgr.GetEventRecorderFor("dns-operator-azure"))

	if err = (&controllers.AzureClusterReconciler{
		Client:                 mgr.GetClient(),
		BaseDomain:             baseDomain,
		BaseZoneClientID:       baseZoneClientID,
		BaseZoneClientSecret:   baseZoneClientSecret,
		BaseZoneSubscriptionID: baseZoneSubscriptionID,
		BaseZoneTenantID:       baseZoneTenantID,
		Recorder:               mgr.GetEventRecorderFor("azurecluster-reconciler"),
		WatchFilterValue:       watchFilterValue,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(errors.FatalError, "unable to create controller AzureCluster")
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
