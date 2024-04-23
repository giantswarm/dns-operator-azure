package privatedns

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/pointer"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api/api/v1beta1"

	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"
)

func Test_CnameRecords(t *testing.T) {
	type args struct {
		ctx               context.Context
		logger            logr.Logger
		currentRecordSets []*armprivatedns.RecordSet
	}
	tests := []struct {
		name            string
		cluster         *v1beta1.Cluster
		azureCluster    *infrav1.AzureCluster
		args            args
		expectedRecords []*armprivatedns.RecordSet
	}{
		{
			name: "create CNAME record in case existing records are empty",
			cluster: &v1beta1.Cluster{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-cluster",
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
			expectedRecords: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						CnameRecord: &armprivatedns.CnameRecord{
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
					Name: "test-cluster",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-cluster",
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
				currentRecordSets: []*armprivatedns.RecordSet{
					{
						Properties: &armprivatedns.RecordSetProperties{
							CnameRecord: &armprivatedns.CnameRecord{
								Cname: pointer.String("endpoint.test-cluster.basedomain.io"),
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("ep.test-cluster.basedomain.io"),
						Type: pointer.String("CNAME"),
					},
				},
			},
			expectedRecords: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						CnameRecord: &armprivatedns.CnameRecord{
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
					Name: "test-cluster",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-cluster",
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
				currentRecordSets: []*armprivatedns.RecordSet{
					{
						Properties: &armprivatedns.RecordSetProperties{
							CnameRecord: &armprivatedns.CnameRecord{
								Cname: pointer.String("ingress.test-cluster.basedomain.io"),
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("*"),
						Type: pointer.String("CNAME"),
					},
				},
			},
			expectedRecords: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						CnameRecord: &armprivatedns.CnameRecord{
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
					Name: "test-cluster",
				},
				Spec: v1beta1.ClusterSpec{
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
				},
			},
			azureCluster: &infrav1.AzureCluster{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-cluster",
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
				currentRecordSets: []*armprivatedns.RecordSet{
					{
						Properties: &armprivatedns.RecordSetProperties{
							CnameRecord: &armprivatedns.CnameRecord{
								Cname: pointer.String("api.test-cluster.basedomain.io"),
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("*"),
						Type: pointer.String("CNAME"),
					},
				},
			},
			expectedRecords: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						CnameRecord: &armprivatedns.CnameRecord{
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

			dnsScopeParams := scope.PrivateDNSScopeParams{
				BaseDomain:  "basedomain.io",
				ClusterName: "test-cluster",
				APIServerIP: "127.0.0.1",
				ClusterSpecToAttachPrivateDNS: infrav1.AzureClusterSpec{
					NetworkSpec: infrav1.NetworkSpec{
						Subnets: infrav1.Subnets{
							{
								ID: "whatever",
								SubnetClassSpec: infrav1.SubnetClassSpec{
									PrivateEndpoints: infrav1.PrivateEndpoints{
										{
											Name: "test-cluster-api-privatelink-privateendpoint",
											PrivateIPAddresses: []string{
												"9.9.9.7",
											},
										},
									},
								},
							},
						},
					},
				},
			}

			dnsScope, err := scope.NewPrivateDNSScope(tt.args.ctx, dnsScopeParams)
			if err != nil {
				t.Fatal(err)
			}

			dnsService, err := New(*dnsScope)
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
