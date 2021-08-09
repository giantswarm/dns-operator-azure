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
	"context"
	"flag"
	"fmt"

	aadpodv1 "github.com/Azure/aad-pod-identity/pkg/apis/aadpodidentity/v1"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	capz "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-azure/util/reconciler"
	capi "sigs.k8s.io/cluster-api/api/v1alpha3"
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
	aadPodIdentityGroupVersion := schema.GroupVersion{Group: aadpodv1.CRDGroup, Version: aadpodv1.CRDVersion}
	scheme.AddKnownTypes(aadPodIdentityGroupVersion,
		&aadpodv1.AzureIdentity{},
		&aadpodv1.AzureIdentityList{},
		&aadpodv1.AzureIdentityBinding{},
		&aadpodv1.AzureIdentityBindingList{},
		&aadpodv1.AzurePodIdentityException{},
		&aadpodv1.AzurePodIdentityExceptionList{},
	)
	metav1.AddToGroupVersion(scheme, aadPodIdentityGroupVersion)
}

func main() {
	err := mainError()
	if err != nil {
		panic(fmt.Sprintf("%#v\n", err))
	}
}

func mainError() error {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctx := context.Background()
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		return microerror.Mask(err)
	}

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
		logger.Errorf(ctx, errors.FatalError, "unable to start manager")
		return microerror.Mask(err)
	}

	// Initialize event recorder.
	record.InitFromRecorder(mgr.GetEventRecorderFor("dns-operator-azure"))

	// if err = (&controllers.AzureClusterReconciler{
	// 	Client:      mgr.GetClient(),
	// 	Micrologger: logger.With("controllers", "AzureCluster"),
	// 	Scheme:      mgr.GetScheme(),
	// }).SetupWithManager(mgr); err != nil {
	// 	logger.Errorf(ctx, errors.FatalError, "unable to create controller AzureCluster")
	// 	return microerror.Mask(err)
	// }
	azureClusterReconciler := controllers.NewAzureClusterReconciler(
		mgr.GetClient(),
		logger,
		mgr.GetEventRecorderFor("azurecluster-reconciler"),
		reconciler.DefaultLoopTimeout)

	if err = azureClusterReconciler.SetupWithManager(mgr); err != nil {
		logger.Errorf(ctx, errors.FatalError, "unable to start manager")
		setupLog.Error(err, "unable to create controller", "controller", "AzureCluster")
		return microerror.Mask(err)
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Errorf(ctx, errors.FatalError, "problem running manager")
		return microerror.Mask(err)
	}

	return nil
}
