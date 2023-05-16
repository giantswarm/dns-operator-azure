package controllers

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	v1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func TestAzureMachineReconciler_Reconcile(t *testing.T) {

	clusterName := "test-cluster"
	clusterNamespace := "test-org"

	tests := []struct {
		name    string
		objects []runtime.Object
		want    map[string]string
	}{
		{
			name: "bastion host exist",
			objects: []runtime.Object{
				&infrav1.AzureCluster{
					ObjectMeta: v1.ObjectMeta{
						Name:      clusterName,
						Namespace: clusterNamespace,
						Annotations: map[string]string{
							"another-operator.giantswarm.io/whatever-annotation": "guess-who",
						},
						Labels: map[string]string{
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
					Spec: infrav1.AzureClusterSpec{
						AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
							SubscriptionID: uuid.New().String(),
						},
					},
				},
				&v1beta1.Cluster{
					ObjectMeta: v1.ObjectMeta{
						Name:      clusterName,
						Namespace: clusterNamespace,
						Labels: map[string]string{
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
					Spec: v1beta1.ClusterSpec{
						InfrastructureRef: &corev1.ObjectReference{
							Name:       clusterName,
							Namespace:  clusterNamespace,
							Kind:       "AzureCluster",
							APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
						},
					},
				},
				&infrav1.AzureMachine{
					ObjectMeta: v1.ObjectMeta{
						Name:      "test-cluster-bastion-ldkjv",
						Namespace: clusterNamespace,
						OwnerReferences: []v1.OwnerReference{
							{
								APIVersion: "cluster.x-k8s.io/v1beta1",
								Kind:       "Machine",
								Name:       "test-cluster-bastion-ldkjf",
							},
						},
						Labels: map[string]string{
							"cluster.x-k8s.io/role":         "bastion",
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
					Spec: infrav1.AzureMachineSpec{},
					Status: infrav1.AzureMachineStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.10.5",
							},
							{
								Type:    corev1.NodeExternalIP,
								Address: "10.24.16.10",
							},
						},
						Conditions: v1beta1.Conditions{
							{
								Type:   infrav1.NetworkInterfaceReadyCondition,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
				&v1beta1.Machine{
					ObjectMeta: v1.ObjectMeta{
						Name:      "test-cluster-bastion-ldkjf",
						Namespace: clusterNamespace,
						Labels: map[string]string{
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
				},
			},
			want: map[string]string{
				"another-operator.giantswarm.io/whatever-annotation": "guess-who",
				"dns-operator-azure.giantswarm.io/bastion-ip":        "192.168.10.5",
			},
		},
		{
			name: "bastion host exist but no ip assigned yet",
			objects: []runtime.Object{
				&infrav1.AzureCluster{
					ObjectMeta: v1.ObjectMeta{
						Name:      clusterName,
						Namespace: clusterNamespace,
						Annotations: map[string]string{
							"another-operator.giantswarm.io/whatever-annotation": "guess-who",
						},
						Labels: map[string]string{
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
					Spec: infrav1.AzureClusterSpec{
						AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
							SubscriptionID: uuid.New().String(),
						},
					},
				},
				&v1beta1.Cluster{
					ObjectMeta: v1.ObjectMeta{
						Name:      clusterName,
						Namespace: clusterNamespace,
						Labels: map[string]string{
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
					Spec: v1beta1.ClusterSpec{
						InfrastructureRef: &corev1.ObjectReference{
							Name:       clusterName,
							Namespace:  clusterNamespace,
							Kind:       "AzureCluster",
							APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
						},
					},
				},
				&infrav1.AzureMachine{
					ObjectMeta: v1.ObjectMeta{
						Name:      "test-cluster-bastion-ldkjv",
						Namespace: clusterNamespace,
						OwnerReferences: []v1.OwnerReference{
							{
								APIVersion: "cluster.x-k8s.io/v1beta1",
								Kind:       "Machine",
								Name:       "test-cluster-bastion-ldkjf",
							},
						},
						Labels: map[string]string{
							"cluster.x-k8s.io/role":         "bastion",
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
					Spec:   infrav1.AzureMachineSpec{},
					Status: infrav1.AzureMachineStatus{},
				},
				&v1beta1.Machine{
					ObjectMeta: v1.ObjectMeta{
						Name:      "test-cluster-bastion-ldkjf",
						Namespace: clusterNamespace,
						Labels: map[string]string{
							"cluster.x-k8s.io/cluster-name": clusterName,
						},
					},
				},
			},
			want: map[string]string{
				"another-operator.giantswarm.io/whatever-annotation": "guess-who",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			schemeBuilder := runtime.SchemeBuilder{
				v1beta1.AddToScheme,
				infrav1.AddToScheme,
			}

			err := schemeBuilder.AddToScheme(scheme.Scheme)
			if err != nil {
				t.Fatal(err)
			}
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(tt.objects...).Build()

			reconciler := &AzureMachineReconciler{
				Client:   client,
				Recorder: record.NewFakeRecorder(128),
			}

			azureMachine := &infrav1.AzureMachine{}
			for _, object := range tt.objects {
				var ok bool
				azureMachine, ok = object.DeepCopyObject().(*infrav1.AzureMachine)
				if ok {
					break
				}
			}

			_, err = reconciler.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: azureMachine.Namespace,
					Name:      azureMachine.Name,
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			azureCluster := &infrav1.AzureCluster{}

			err = client.Get(context.TODO(), types.NamespacedName{
				Name:      clusterName,
				Namespace: clusterNamespace,
			}, azureCluster)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(azureCluster.Annotations, tt.want) {
				t.Errorf("AzureMachineReconciler.Reconcile() = %v, want %v", azureCluster.Annotations, tt.want)
			}
		})
	}
}
