package dns

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/pointer"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/cluster-api/api/v1beta1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/giantswarm/dns-operator-azure/v3/azure/scope"
	"github.com/giantswarm/dns-operator-azure/v3/pkg/infracluster"
)

func Test_CnameRecords(t *testing.T) {
	type args struct {
		ctx               context.Context
		logger            logr.Logger
		currentRecordSets []*armdns.RecordSet
	}
	tests := []struct {
		name            string
		cluster         *v1beta1.Cluster
		azureCluster    *infrav1.AzureCluster
		args            args
		expectedRecords []*armdns.RecordSet
	}{
		{
			name: "create CNAME record in case existing records are empty",
			cluster: &v1beta1.Cluster{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
					InfrastructureRef: &corev1.ObjectReference{
						Name:      "test-cluster",
						Namespace: "default",
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				TypeMeta: v1.TypeMeta{
					Kind:       "AzureCluster",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterSpec{
					ResourceGroup: "flkjd",
					AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
						SubscriptionID: uuid.New().String(),
					},
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			args: args{
				ctx: context.TODO(),
			},
			expectedRecords: []*armdns.RecordSet{
				{
					Properties: &armdns.RecordSetProperties{
						CnameRecord: &armdns.CnameRecord{
							Cname: pointer.String("ingress.test-cluster.basedomain.io"),
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("*"),
					Type: pointer.String("CNAME"),
				},
			},
		},
		{
			name: "create CNAME record in case it does not exist",
			cluster: &v1beta1.Cluster{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
					InfrastructureRef: &corev1.ObjectReference{
						Name:      "test-cluster",
						Namespace: "default",
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				TypeMeta: v1.TypeMeta{
					Kind:       "AzureCluster",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterSpec{
					ResourceGroup: "flkjd",
					AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
						SubscriptionID: uuid.New().String(),
					},
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armdns.RecordSet{
					{
						Properties: &armdns.RecordSetProperties{
							CnameRecord: &armdns.CnameRecord{
								Cname: pointer.String("endpoint.test-cluster.basedomain.io"),
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("ep.test-cluster.basedomain.io"),
						Type: pointer.String("CNAME"),
					},
				},
			},
			expectedRecords: []*armdns.RecordSet{
				{
					Properties: &armdns.RecordSetProperties{
						CnameRecord: &armdns.CnameRecord{
							Cname: pointer.String("ingress.test-cluster.basedomain.io"),
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("*"),
					Type: pointer.String("CNAME"),
				},
			},
		},
		{
			name: "update CNAME record as current TTL is not equal",
			cluster: &v1beta1.Cluster{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
					InfrastructureRef: &corev1.ObjectReference{
						Name:      "test-cluster",
						Namespace: "default",
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				TypeMeta: v1.TypeMeta{
					Kind:       "AzureCluster",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterSpec{
					ResourceGroup: "flkjd",
					AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
						SubscriptionID: uuid.New().String(),
					},
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armdns.RecordSet{
					{
						Properties: &armdns.RecordSetProperties{
							CnameRecord: &armdns.CnameRecord{
								Cname: pointer.String("ingress.test-cluster.basedomain.io"),
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("*"),
						Type: pointer.String("CNAME"),
					},
				},
			},
			expectedRecords: []*armdns.RecordSet{
				{
					Properties: &armdns.RecordSetProperties{
						CnameRecord: &armdns.CnameRecord{
							Cname: pointer.String("ingress.test-cluster.basedomain.io"),
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("*"),
					Type: pointer.String("CNAME"),
				},
			},
		},
		{
			name: "update CNAME record as current value is not equal",
			cluster: &v1beta1.Cluster{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
					InfrastructureRef: &corev1.ObjectReference{
						Name:      "test-cluster",
						Namespace: "default",
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				TypeMeta: v1.TypeMeta{
					Kind:       "AzureCluster",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: infrav1.AzureClusterSpec{
					ResourceGroup: "flkjd",
					AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
						SubscriptionID: uuid.New().String(),
					},
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armdns.RecordSet{
					{
						Properties: &armdns.RecordSetProperties{
							CnameRecord: &armdns.CnameRecord{
								Cname: pointer.String("api.test-cluster.basedomain.io"),
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("*"),
						Type: pointer.String("CNAME"),
					},
				},
			},
			expectedRecords: []*armdns.RecordSet{
				{
					Properties: &armdns.RecordSetProperties{
						CnameRecord: &armdns.CnameRecord{
							Cname: pointer.String("ingress.test-cluster.basedomain.io"),
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("*"),
					Type: pointer.String("CNAME"),
				},
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

			kubeClient := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithRuntimeObjects(tt.azureCluster, tt.cluster).
				Build()

			infraCluster := &unstructured.Unstructured{}
			infraCluster.SetGroupVersionKind(tt.azureCluster.GroupVersionKind())
			err = kubeClient.Get(tt.args.ctx, k8sclient.ObjectKey{Name: tt.azureCluster.Name, Namespace: tt.azureCluster.Namespace}, infraCluster)
			if err != nil {
				t.Fatal(err)
			}

			clusterScope, err := capzscope.NewClusterScope(tt.args.ctx, capzscope.ClusterScopeParams{
				Client:       kubeClient,
				Cluster:      tt.cluster,
				AzureCluster: tt.azureCluster,
			})
			if err != nil {
				t.Fatal(err)
			}

			infraClusterScope, err := infracluster.NewScope(tt.args.ctx, infracluster.ScopeParams{
				Client:       kubeClient,
				Cluster:      tt.cluster,
				InfraCluster: infraCluster,
			})
			if err != nil {
				t.Fatal(err)
			}

			infraClusterScope.Patcher = clusterScope

			dnsScopeParams := scope.DNSScopeParams{
				BaseZoneCredentials: scope.BaseZoneCredentials{
					ClientID:       uuid.New().String(),
					ClientSecret:   uuid.New().String(),
					TenantID:       uuid.New().String(),
					SubscriptionID: uuid.New().String(),
				},
				BaseDomain:              "basedomain.io",
				BaseDomainResourceGroup: "basedomain_resource_group",
				ClusterScope:            infraClusterScope,
			}

			dnsScope, err := scope.NewDNSScope(tt.args.ctx, dnsScopeParams)
			if err != nil {
				t.Fatal(err)
			}

			publicIPsService, err := publicips.New(clusterScope)
			if err != nil {
				t.Fatal(err)
			}

			dnsService, err := New(*dnsScope, publicIPsService)
			if err != nil {
				t.Fatal(err)
			}

			got := dnsService.calculateMissingCnameRecords(tt.args.logger, tt.args.currentRecordSets)
			if err != nil {
				t.Errorf("Service.calculateMissingARecords() error = %v", err)
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.expectedRecords) {
				gotJSON, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				wantJSON, err := json.Marshal(tt.expectedRecords)
				if err != nil {
					t.Fatal(err)
				}
				t.Errorf("Service.calculateMissingCnameRecords() = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}
