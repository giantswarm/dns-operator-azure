package privatedns

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"github.com/go-logr/logr"
	"k8s.io/utils/pointer"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"

	"github.com/giantswarm/dns-operator-azure/v3/azure/scope"
)

func TestService_calculateMissingARecords(t *testing.T) {
	type args struct {
		ctx               context.Context
		logger            logr.Logger
		currentRecordSets []*armprivatedns.RecordSet
	}
	tests := []struct {
		name                  string
		privateDNSScopeParams scope.PrivateDNSScopeParams
		args                  args
		want                  []*armprivatedns.RecordSet
	}{
		{
			name: "update A record as current TTL is not equal",
			privateDNSScopeParams: scope.PrivateDNSScopeParams{
				BaseDomain:  "basedomain.io",
				ClusterName: "test-cluster",
				APIServerIP: "127.0.0.1",
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armprivatedns.RecordSet{
					{
						Properties: &armprivatedns.RecordSetProperties{
							ARecords: []*armprivatedns.ARecord{
								{
									IPv4Address: pointer.String("127.0.0.1"),
								},
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("apiserver"),
					},
				},
			},
			want: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						ARecords: []*armprivatedns.ARecord{
							{
								IPv4Address: pointer.String("127.0.0.1"),
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
			name: "A records already in desired state",
			privateDNSScopeParams: scope.PrivateDNSScopeParams{
				BaseDomain:  "basedomain.io",
				ClusterName: "test-cluster",
				APIServerIP: "127.0.0.1",
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armprivatedns.RecordSet{
					{
						Properties: &armprivatedns.RecordSetProperties{
							ARecords: []*armprivatedns.ARecord{
								{
									IPv4Address: pointer.String("127.0.0.1"),
								},
							},
							TTL: pointer.Int64(300),
						},
						Name: pointer.String("apiserver"),
					},
				},
			},
			want: []*armprivatedns.RecordSet(nil),
		},
		{
			name: "non dns-operator-azure managed record exist and will be ignored",
			privateDNSScopeParams: scope.PrivateDNSScopeParams{
				BaseDomain:  "basedomain.io",
				ClusterName: "test-cluster",
				APIServerIP: "127.0.0.1",
			},
			args: args{
				ctx: context.TODO(),
				currentRecordSets: []*armprivatedns.RecordSet{
					{
						Properties: &armprivatedns.RecordSetProperties{
							ARecords: []*armprivatedns.ARecord{
								{
									IPv4Address: pointer.String("8.8.8.8"),
								},
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("apiserver"),
					},
					{
						Properties: &armprivatedns.RecordSetProperties{
							ARecords: []*armprivatedns.ARecord{
								{
									IPv4Address: pointer.String("1.1.1.1"),
								},
							},
							TTL: pointer.Int64(600),
						},
						Name: pointer.String("not-managed-by-dns-operator"),
					},
				},
			},
			want: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						ARecords: []*armprivatedns.ARecord{
							{
								IPv4Address: pointer.String("127.0.0.1"),
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
			name: "API server IP is set in the privateEndpoint struct on the management Cluster",
			privateDNSScopeParams: scope.PrivateDNSScopeParams{
				BaseDomain:  "basedomain.io",
				ClusterName: "test-cluster",
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
			},
			args: args{
				ctx:               context.TODO(),
				currentRecordSets: []*armprivatedns.RecordSet{},
			},
			want: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						ARecords: []*armprivatedns.ARecord{
							{
								IPv4Address: pointer.String("9.9.9.7"),
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
			name: "two API server IPs are given - IP from annotation should take precedence",
			privateDNSScopeParams: scope.PrivateDNSScopeParams{
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
			},
			args: args{
				ctx:               context.TODO(),
				currentRecordSets: []*armprivatedns.RecordSet{},
			},
			want: []*armprivatedns.RecordSet{
				{
					Properties: &armprivatedns.RecordSetProperties{
						ARecords: []*armprivatedns.ARecord{
							{
								IPv4Address: pointer.String("127.0.0.1"),
							},
						},
						TTL: pointer.Int64(300),
					},
					Name: pointer.String("apiserver"),
					Type: pointer.String("A"),
				},
			},
		},
	}
	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {

			privateDnsScope, err := scope.NewPrivateDNSScope(tt.args.ctx, tt.privateDNSScopeParams)
			if err != nil {
				t.Fatal(err)
			}

			privateDnsService, err := New(*privateDnsScope)
			if err != nil {
				t.Fatal(err)
			}

			got := privateDnsService.calculateMissingARecords(tt.args.ctx, tt.args.logger, tt.args.currentRecordSets)
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
