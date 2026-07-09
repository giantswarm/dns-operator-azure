package infracluster

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/scheme"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api/api/core/v1beta1"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	capi "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	fakeClientID = "fake-client-id"
	fakeTenantID = "fake-tenant-id"
)

func Test_CreateScope(t *testing.T) {

	testSubscriptionId := uuid.New().String()

	testCases := []struct {
		name                       string
		cluster                    *capi.Cluster
		infraCluster               runtime.Object
		managementCluster          runtime.Object
		identity                   *infrav1.AzureClusterIdentity
		identitySecret             *corev1.Secret
		expectAzureCluster         bool
		expectedAzureClusterSpec   *infrav1.AzureClusterSpec
		expectedAzureClusterStatus *infrav1.AzureClusterStatus
	}{
		{
			name: "case0: Create AzureCluster scope",
			cluster: &capi.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-azure",
					Namespace: "default",
				},
				Spec: capi.ClusterSpec{
					InfrastructureRef: capi.ContractVersionedObjectReference{
						Name: "test-infra-cluster-azure",
					},
				},
			},
			infraCluster: &infrav1.AzureCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AzureCluster",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-infra-cluster-azure",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterSpec{
					ResourceGroup: "flkjd",
					AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
						IdentityRef: &corev1.ObjectReference{
							Kind: infrav1.AzureClusterIdentityKind,
							Name: "fake-identity",
						},
						SubscriptionID: testSubscriptionId,
					},
				},
				Status: infrav1.AzureClusterStatus{
					Ready: true,
				},
			},
			identity: &infrav1.AzureClusterIdentity{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fake-identity",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterIdentitySpec{
					Type:     infrav1.ServicePrincipal,
					ClientID: fakeClientID,
					TenantID: fakeTenantID,
				},
			},
			identitySecret:     &corev1.Secret{Data: map[string][]byte{"clientSecret": []byte("fooSecret")}},
			expectAzureCluster: true,
			expectedAzureClusterSpec: &infrav1.AzureClusterSpec{
				ResourceGroup: "flkjd",
				AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
					IdentityRef: &corev1.ObjectReference{
						Kind: infrav1.AzureClusterIdentityKind,
						Name: "fake-identity",
					},
					SubscriptionID: testSubscriptionId,
				},
			},
			expectedAzureClusterStatus: &infrav1.AzureClusterStatus{
				Ready: true,
			},
		},
		{
			name: "case1: Create non-Azure cluster scope",
			cluster: &capi.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-non-azure",
					Namespace: "default",
				},
				Spec: capi.ClusterSpec{
					InfrastructureRef: capi.ContractVersionedObjectReference{
						Name: "test-infra-cluster-non-azure",
					},
				},
			},
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-infra-cluster-non-azure",
					Namespace: "default",
				},
			},
			identity: &infrav1.AzureClusterIdentity{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fake-identity",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterIdentitySpec{
					Type:     infrav1.ServicePrincipal,
					ClientID: fakeClientID,
					TenantID: fakeTenantID,
				},
			},
			identitySecret: &corev1.Secret{Data: map[string][]byte{"clientSecret": []byte("fooSecret")}},
			managementCluster: &infrav1.AzureCluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AzureCluster",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-management-cluster",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterSpec{
					AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
						IdentityRef: &corev1.ObjectReference{
							Name:      "test-management-cluster-identity",
							Namespace: "default",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			ctx := context.TODO()

			schemeBuilder := runtime.SchemeBuilder{
				capi.AddToScheme,
				infrav1.AddToScheme,
			}

			err := schemeBuilder.AddToScheme(scheme.Scheme)
			if err != nil {
				t.Fatal(err)
			}

			kubeClientBuilder := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithRuntimeObjects(tc.cluster, tc.infraCluster, tc.identity, tc.identitySecret)

			if tc.managementCluster != nil {
				kubeClientBuilder.WithRuntimeObjects(tc.managementCluster)
			}

			kubeClient := kubeClientBuilder.Build()

			infraClusterObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tc.infraCluster)
			if err != nil {
				t.Fatal(err)
			}

			infraCluster := &unstructured.Unstructured{Object: infraClusterObj}
			infraCluster.SetGroupVersionKind(tc.infraCluster.GetObjectKind().GroupVersionKind())

			scope, err := NewScope(ctx, ScopeParams{
				Client:       kubeClient,
				Cluster:      tc.cluster,
				InfraCluster: infraCluster,
				ManagementClusterConfig: ManagementClusterConfig{
					Namespace: "default",
					Name:      "test-management-cluster",
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			if scope.IsAzureCluster() != tc.expectAzureCluster {
				if tc.expectAzureCluster {
					t.Fatalf("Failed to create scope for infra cluster, expected Azure, got non-Azure")
				} else {
					t.Fatalf("Failed to create scope for infra cluster, expected non-Azure, got Azure")
				}
			}

			actualAzureClusterSpec := scope.AzureClusterSpec()
			if tc.expectedAzureClusterSpec == nil && actualAzureClusterSpec != nil {
				t.Fatalf("Unexpected Azure cluster spec: %v\n", actualAzureClusterSpec)
			} else if tc.expectedAzureClusterSpec != nil && (actualAzureClusterSpec == nil || !reflect.DeepEqual(*tc.expectedAzureClusterSpec, *actualAzureClusterSpec)) {
				t.Fatalf("Unexpected Azure cluster spec\nexpected\n%v,\nactual\n%v\n", tc.expectedAzureClusterSpec, actualAzureClusterSpec)
			}

			actualAzureClusterStatus := scope.AzureClusterStatus()
			if tc.expectedAzureClusterStatus == nil && actualAzureClusterStatus != nil {
				t.Fatalf("Unexpected Azure cluster status: %v\n", actualAzureClusterStatus)
			} else if tc.expectedAzureClusterStatus != nil && (actualAzureClusterStatus == nil || !reflect.DeepEqual(*tc.expectedAzureClusterStatus, *actualAzureClusterStatus)) {
				t.Fatalf("Unexpected Azure cluster status\nexpected\n%v,\nactual\n%v\n", tc.expectedAzureClusterStatus, actualAzureClusterStatus)
			}
		})
	}
}

func Test_CreateScope_SubscriptionID(t *testing.T) {
	const (
		envSubscriptionID        = "env-subscription-id"
		annotationSubscriptionID = "annotation-subscription-id"
	)

	testCases := []struct {
		name          string
		infraKind     string
		annotations   map[string]string
		expectedSubID string
	}{
		{
			name:          "AKS cluster: subscription comes from the Cluster annotation",
			infraKind:     infrav1.AzureASOManagedClusterKind,
			annotations:   map[string]string{AnnotationAzureSubscriptionID: annotationSubscriptionID},
			expectedSubID: annotationSubscriptionID,
		},
		{
			name:          "AKS cluster without annotation: falls back to the configured subscription",
			infraKind:     infrav1.AzureASOManagedClusterKind,
			annotations:   nil,
			expectedSubID: envSubscriptionID,
		},
		{
			name:          "non-AKS cluster: annotation is ignored",
			infraKind:     "VSphereCluster",
			annotations:   map[string]string{AnnotationAzureSubscriptionID: annotationSubscriptionID},
			expectedSubID: envSubscriptionID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.TODO()

			schemeBuilder := runtime.SchemeBuilder{
				capi.AddToScheme,
				infrav1.AddToScheme,
			}
			if err := schemeBuilder.AddToScheme(scheme.Scheme); err != nil {
				t.Fatal(err)
			}

			cluster := &capi.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-cluster",
					Namespace:   "default",
					Annotations: tc.annotations,
				},
				Spec: capi.ClusterSpec{
					InfrastructureRef: capi.ContractVersionedObjectReference{
						Name: "test-infra-cluster",
					},
				},
			}

			infraCluster := &unstructured.Unstructured{}
			infraCluster.SetGroupVersionKind(infrav1.GroupVersion.WithKind(tc.infraKind))
			infraCluster.SetName("test-infra-cluster")
			infraCluster.SetNamespace("default")

			kubeClient := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithRuntimeObjects(cluster).
				Build()

			// Provide a complete ClusterZoneAzureConfig so the scope does not
			// look up the management cluster for credentials.
			scope, err := NewScope(ctx, ScopeParams{
				Client:       kubeClient,
				Cluster:      cluster,
				InfraCluster: infraCluster,
				ClusterZoneAzureConfig: ClusterZoneAzureConfig{
					SubscriptionID: envSubscriptionID,
					ClientID:       fakeClientID,
					TenantID:       fakeTenantID,
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			if got := scope.Patcher.SubscriptionID(); got != tc.expectedSubID {
				t.Errorf("SubscriptionID() = %q, want %q", got, tc.expectedSubID)
			}
		})
	}
}

func Test_InfraClusterIdentity_ASOManagedCluster(t *testing.T) {
	ctx := context.TODO()

	const (
		identityName      = "aks-identity"
		identityNamespace = "org-test"
		identityClientID  = "aks-identity-client-id"
		identityTenantID  = "aks-identity-tenant-id"
	)

	schemeBuilder := runtime.SchemeBuilder{
		capi.AddToScheme,
		infrav1.AddToScheme,
	}
	if err := schemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		t.Fatal(err)
	}

	cluster := &capi.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-aks",
			Namespace: "default",
		},
		Spec: capi.ClusterSpec{
			InfrastructureRef: capi.ContractVersionedObjectReference{
				Name: "test-aks",
			},
		},
	}

	identity := &infrav1.AzureClusterIdentity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identityName,
			Namespace: identityNamespace,
		},
		Spec: infrav1.AzureClusterIdentitySpec{
			Type:     infrav1.WorkloadIdentity,
			ClientID: identityClientID,
			TenantID: identityTenantID,
		},
	}

	infraCluster := &unstructured.Unstructured{}
	infraCluster.SetGroupVersionKind(infrav1.GroupVersion.WithKind(infrav1.AzureASOManagedClusterKind))
	infraCluster.SetName("test-aks")
	infraCluster.SetNamespace("default")
	infraCluster.SetAnnotations(map[string]string{
		AnnotationAzureClusterIdentityName:      identityName,
		AnnotationAzureClusterIdentityNamespace: identityNamespace,
	})

	kubeClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithRuntimeObjects(cluster, identity).
		Build()

	scope, err := NewScope(ctx, ScopeParams{
		Client:       kubeClient,
		Cluster:      cluster,
		InfraCluster: infraCluster,
		ClusterZoneAzureConfig: ClusterZoneAzureConfig{
			SubscriptionID: "sub",
			ClientID:       fakeClientID,
			TenantID:       fakeTenantID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := scope.InfraClusterIdentity(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != identityName || got.Namespace != identityNamespace {
		t.Errorf("InfraClusterIdentity() = %s/%s, want %s/%s", got.Namespace, got.Name, identityNamespace, identityName)
	}
	if got.Spec.ClientID != identityClientID || got.Spec.TenantID != identityTenantID {
		t.Errorf("InfraClusterIdentity() client/tenant = %s/%s, want %s/%s", got.Spec.ClientID, got.Spec.TenantID, identityClientID, identityTenantID)
	}
}

func Test_CommonPatcher_PatchObject_ASOManagedClusterPersistsFinalizer(t *testing.T) {
	ctx := context.TODO()

	const finalizer = "dns-operator-azure.giantswarm.io/azurecluster"

	schemeBuilder := runtime.SchemeBuilder{
		capi.AddToScheme,
		infrav1.AddToScheme,
	}
	if err := schemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		t.Fatal(err)
	}

	infraCluster := &unstructured.Unstructured{}
	infraCluster.SetGroupVersionKind(infrav1.GroupVersion.WithKind(infrav1.AzureASOManagedClusterKind))
	infraCluster.SetName("test-aks")
	infraCluster.SetNamespace("default")

	kubeClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infraCluster).
		Build()

	// Fetch a fresh copy, as the controller does before mutating.
	fetched := &unstructured.Unstructured{}
	fetched.SetGroupVersionKind(infrav1.GroupVersion.WithKind(infrav1.AzureASOManagedClusterKind))
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: "test-aks", Namespace: "default"}, fetched); err != nil {
		t.Fatal(err)
	}

	patcher, err := NewCommonPatcher(ctx, CommonPatcherParams{
		Cluster: &capi.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-aks", Namespace: "default"},
			Spec: capi.ClusterSpec{
				ControlPlaneEndpoint: capi.APIEndpoint{
					Host: "test-aks-abcd1234.hcp.westeurope.azmk8s.io",
					Port: 443,
				},
			},
		},
		InfraCluster: fetched,
		K8sClient:    kubeClient,
	})
	if err != nil {
		t.Fatal(err)
	}

	controllerutil.AddFinalizer(fetched, finalizer)
	if err := patcher.PatchObject(ctx); err != nil {
		t.Fatalf("PatchObject() error = %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(infrav1.GroupVersion.WithKind(infrav1.AzureASOManagedClusterKind))
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: "test-aks", Namespace: "default"}, got); err != nil {
		t.Fatal(err)
	}
	if !controllerutil.ContainsFinalizer(got, finalizer) {
		t.Errorf("finalizer %q was not persisted on the AzureASOManagedCluster", finalizer)
	}
}

func TestSetUnstructuredCondition(t *testing.T) {
	t.Run("adds condition to the unstructured object", func(t *testing.T) {
		g := NewWithT(t)
		azureCluster := infrav1.AzureCluster{
			Status: infrav1.AzureClusterStatus{
				Conditions: v1beta1.Conditions{
					v1beta1.Condition{
						Type:   "Foo",
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&azureCluster)
		g.Expect(err).Should(BeNil())

		obj := unstructured.Unstructured{Object: unstructuredMap}
		err = SetUnstructuredCondition(&obj, metav1.Condition{
			Type:   "Bar",
			Status: metav1.ConditionTrue,
		})
		g.Expect(err).Should(BeNil())
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &azureCluster)
		g.Expect(err).Should(BeNil())

		found := slices.ContainsFunc(azureCluster.Status.Conditions, func(c clusterv1beta1.Condition) bool {
			return c.Type == "Bar"
		})
		g.Expect(found).To(BeTrue())
	})

	t.Run("adds condition when object has no status.conditions field", func(t *testing.T) {
		g := NewWithT(t)
		azureCluster := infrav1.AzureCluster{}
		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&azureCluster)
		g.Expect(err).Should(BeNil())
		obj := unstructured.Unstructured{Object: unstructuredMap}

		err = SetUnstructuredCondition(&obj, metav1.Condition{
			Type:   "Foo",
			Status: metav1.ConditionTrue,
		})
		g.Expect(err).Should(BeNil())
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &azureCluster)
		g.Expect(err).Should(BeNil())

		found := slices.ContainsFunc(azureCluster.Status.Conditions, func(c clusterv1beta1.Condition) bool {
			return c.Type == "Foo"
		})
		g.Expect(found).To(BeTrue())
	})
}
