package infracluster

import (
	"context"
	"github.com/google/uuid"
	"testing"
	
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/scheme"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_CreateScope(t *testing.T) {

	testCases := []struct {
		name                    string
		cluster                 *capi.Cluster
		infraCluster            runtime.Object
		managementCluster       runtime.Object
		expectAzureClusterScope bool
	}{
		{
			name: "case0: Create AzureCluster scope",
			cluster: &capi.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-azure",
					Namespace: "default",
				},
				Spec: capi.ClusterSpec{
					InfrastructureRef: &corev1.ObjectReference{
						Name:      "test-infra-cluster-azure",
						Namespace: "default",
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
						SubscriptionID: uuid.New().String(),
					},
				},
			},
			expectAzureClusterScope: true,
		},
		{
			name: "case1: Create non-Azure cluster scope",
			cluster: &capi.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-non-azure",
					Namespace: "default",
				},
				Spec: capi.ClusterSpec{
					InfrastructureRef: &corev1.ObjectReference{
						Name:      "test-infra-cluster-non-azure",
						Namespace: "default",
					},
				},
			},
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-infra-cluster-non-azure",
					Namespace: "default",
				},
			},
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
				WithRuntimeObjects(tc.cluster, tc.infraCluster)

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

			if scope.IsAzureCluster() != tc.expectAzureClusterScope {
				if tc.expectAzureClusterScope {
					t.Fatalf("Failed to create scope for infra cluster, expected Azure, got non-Azure")
				} else {
					t.Fatalf("Failed to create scope for infra cluster, expected non-Azure, got Azure")
				}
			}
		})
	}
}
