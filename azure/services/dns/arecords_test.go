package dns

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/go-logr/logr"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubectl/pkg/scheme"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/azure/scope"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/cluster-api/api/v1beta1"

	"github.com/giantswarm/dns-operator-azure/v2/azure/scope"

	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"

	"github.com/google/uuid"

	"k8s.io/utils/pointer"
)

func TestService_calculateMissingARecords(t *testing.T) {
	type args struct {
		ctx               context.Context
		logger            logr.Logger
		currentRecordSets []*armdns.RecordSet
	}
	tests := []struct {
		name         string
		cluster      *v1beta1.Cluster
		azureCluster *infrav1.AzureCluster
		args         args
		want         []*armdns.RecordSet
	}{
		{
			name: "private cluster - update A record as current TTL is not equal",
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
						SubscriptionID: "lkjdldj",
					},
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
					NetworkSpec: infrav1.NetworkSpec{
						APIServerLB: infrav1.LoadBalancerSpec{
							LoadBalancerClassSpec: infrav1.LoadBalancerClassSpec{
								SKU:  infrav1.SKUStandard,
								Type: infrav1.Internal,
							},
							FrontendIPs: []infrav1.FrontendIP{
								{
									Name: "test-cluster-api-internal-lb-frontend-ip",
									FrontendIPClass: infrav1.FrontendIPClass{
										PrivateIPAddress: "192.168.2.6",
									},
								},
							},
							PrivateLinks: []infrav1.PrivateLink{},
						},
					},
				},
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armdns.RecordSet{
					{
						Properties: &armdns.RecordSetProperties{
							ARecords: []*armdns.ARecord{
								{
									IPv4Address: pointer.String("8.8.8.8"),
								},
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("not-managed-by-dns-operator"),
					},
					{
						Properties: &armdns.RecordSetProperties{
							ARecords: []*armdns.ARecord{
								{
									IPv4Address: pointer.String("192.168.2.6"),
								},
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("api"),
					},
					{
						Properties: &armdns.RecordSetProperties{
							ARecords: []*armdns.ARecord{
								{
									IPv4Address: pointer.String("192.168.2.6"),
								},
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("apiserver"),
					},
				},
			},
			want: []*armdns.RecordSet{
				{
					Properties: &armdns.RecordSetProperties{
						ARecords: []*armdns.ARecord{
							{
								IPv4Address: pointer.String("192.168.2.6"),
							},
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("api"),
					Type: pointer.String("A"),
				},
				{
					Properties: &armdns.RecordSetProperties{
						ARecords: []*armdns.ARecord{
							{
								IPv4Address: pointer.String("192.168.2.6"),
							},
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("apiserver"),
					Type: pointer.String("A"),
				},
			},
		},
		{
			name: "private cluster - bastion IP has changed",
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
					Annotations: map[string]string{
						"dns-operator-azure.giantswarm.io/bastion-ip": "192.168.2.60",
					},
				},
				Spec: infrav1.AzureClusterSpec{
					ResourceGroup: "flkjd",
					AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
						SubscriptionID: "lkjdldj",
					},
					ControlPlaneEndpoint: v1beta1.APIEndpoint{
						Host: "api-server.mydomain.io",
						Port: 6443,
					},
					NetworkSpec: infrav1.NetworkSpec{
						APIServerLB: infrav1.LoadBalancerSpec{
							LoadBalancerClassSpec: infrav1.LoadBalancerClassSpec{
								SKU:  infrav1.SKUStandard,
								Type: infrav1.Internal,
							},
							FrontendIPs: []infrav1.FrontendIP{
								{
									Name: "test-cluster-api-internal-lb-frontend-ip",
									FrontendIPClass: infrav1.FrontendIPClass{
										PrivateIPAddress: "192.168.2.6",
									},
								},
							},
							PrivateLinks: []infrav1.PrivateLink{},
						},
					},
				},
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armdns.RecordSet{
					{
						Properties: &armdns.RecordSetProperties{
							ARecords: []*armdns.ARecord{
								{
									IPv4Address: pointer.String("192.168.2.50"),
								},
							},
							TTL: pointer.Int64(300),
						},
						Name: pointer.String("bastion1"),
					},
					{
						Properties: &armdns.RecordSetProperties{
							ARecords: []*armdns.ARecord{
								{
									IPv4Address: pointer.String("192.168.2.50"),
								},
							},
							TTL: pointer.Int64(300),
						},
						Name: pointer.String("bastion"),
					},
					{
						Properties: &armdns.RecordSetProperties{
							ARecords: []*armdns.ARecord{
								{
									IPv4Address: pointer.String("192.168.2.6"),
								},
							},
							TTL: pointer.Int64(300),
						},
						Name: pointer.String("api"),
					},
					{
						Properties: &armdns.RecordSetProperties{
							ARecords: []*armdns.ARecord{
								{
									IPv4Address: pointer.String("192.168.2.6"),
								},
							},
							TTL: pointer.Int64(300),
						},
						Name: pointer.String("apiserver"),
					},
				},
			},
			want: []*armdns.RecordSet{
				{
					Properties: &armdns.RecordSetProperties{
						ARecords: []*armdns.ARecord{
							{
								IPv4Address: pointer.String("192.168.2.60"),
							},
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("bastion"),
					Type: pointer.String("A"),
				},
				{
					Properties: &armdns.RecordSetProperties{
						ARecords: []*armdns.ARecord{
							{
								IPv4Address: pointer.String("192.168.2.60"),
							},
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("bastion1"),
					Type: pointer.String("A"),
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

			clusterScope, err := capzscope.NewClusterScope(tt.args.ctx, capzscope.ClusterScopeParams{
				Client: fakeclient.NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithRuntimeObjects(tt.azureCluster, tt.cluster).
					Build(),
				Cluster:      tt.cluster,
				AzureCluster: tt.azureCluster,
			})
			if err != nil {
				t.Fatal(err)
			}

			dnsScopeParams := scope.DNSScopeParams{
				BaseZoneCredentials: scope.BaseZoneCredentials{
					ClientID:       uuid.New().String(),
					ClientSecret:   uuid.New().String(),
					TenantID:       uuid.New().String(),
					SubscriptionID: uuid.New().String(),
				},
				BaseDomain:              "basedomain.io",
				BaseDomainResourceGroup: "basedomain_resource_group",
				ClusterScope:            *clusterScope,
			}

			// add the bastionIP from the annotations
			clusterAnnotations := tt.azureCluster.GetAnnotations()
			if clusterAnnotations["dns-operator-azure.giantswarm.io/bastion-ip"] != "" {
				dnsScopeParams.BastionIP = clusterAnnotations["dns-operator-azure.giantswarm.io/bastion-ip"]
			}

			dnsScope, err := scope.NewDNSScope(tt.args.ctx, dnsScopeParams)
			if err != nil {
				t.Fatal(err)
			}

			publicIPsService := publicips.New(clusterScope)

			dnsService, err := New(*dnsScope, publicIPsService)
			if err != nil {
				t.Fatal(err)
			}

			got, err := dnsService.calculateMissingARecords(tt.args.ctx, tt.args.logger, tt.args.currentRecordSets)
			if err != nil {
				t.Errorf("Service.calculateMissingARecords() error = %v", err)
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				gotJSON, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				wantJSON, err := json.Marshal(tt.want)
				if err != nil {
					t.Fatal(err)
				}
				t.Errorf("Service.calculateMissingARecords() = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}
