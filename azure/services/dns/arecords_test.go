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

			// inject empty workload cluster client so getGatewayARecords finds no services
			dnsService.scope.SetClusterK8sClient(
				fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build(),
			)

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

// newGatewayTestService builds a DNS Service for gateway tests. The provided
// wcServices are loaded into a fake workload cluster client and injected into
// the service scope, bypassing kubeconfig resolution.
func newGatewayTestService(t *testing.T, ctx context.Context, wcServices []*corev1.Service) *Service {
	t.Helper()

	cluster := &v1beta1.Cluster{
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
	}

	azureCluster := &infrav1.AzureCluster{
		TypeMeta: v1.TypeMeta{
			Kind:       "AzureCluster",
			APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: infrav1.AzureClusterSpec{
			ResourceGroup: "test-rg",
			AzureClusterClassSpec: infrav1.AzureClusterClassSpec{
				SubscriptionID: uuid.New().String(),
			},
			ControlPlaneEndpoint: v1beta1.APIEndpoint{
				Host: "api-server.mydomain.io",
				Port: 6443,
			},
		},
	}

	schemeBuilder := runtime.SchemeBuilder{
		v1beta1.AddToScheme,
		infrav1.AddToScheme,
	}
	if err := schemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		t.Fatal(err)
	}

	mcClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithRuntimeObjects(azureCluster, cluster).
		Build()

	infraClusterObj := &unstructured.Unstructured{}
	infraClusterObj.SetGroupVersionKind(azureCluster.GroupVersionKind())
	if err := mcClient.Get(ctx, k8sclient.ObjectKey{Name: azureCluster.Name, Namespace: azureCluster.Namespace}, infraClusterObj); err != nil {
		t.Fatal(err)
	}

	clusterScope, err := capzscope.NewClusterScope(ctx, capzscope.ClusterScopeParams{
		Client:       mcClient,
		Cluster:      cluster,
		AzureCluster: azureCluster,
	})
	if err != nil {
		t.Fatal(err)
	}

	infraClusterScope, err := infracluster.NewScope(ctx, infracluster.ScopeParams{
		Client:       mcClient,
		Cluster:      cluster,
		InfraCluster: infraClusterObj,
	})
	if err != nil {
		t.Fatal(err)
	}
	infraClusterScope.Patcher = clusterScope

	dnsScope, err := scope.NewDNSScope(ctx, scope.DNSScopeParams{
		BaseZoneCredentials: scope.BaseZoneCredentials{
			ClientID:       uuid.New().String(),
			ClientSecret:   uuid.New().String(),
			TenantID:       uuid.New().String(),
			SubscriptionID: uuid.New().String(),
		},
		BaseDomain:              "basedomain.io",
		BaseDomainResourceGroup: "basedomain_resource_group",
		ClusterScope:            infraClusterScope,
	})
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

	// Use a dedicated scheme for the workload cluster client with only core types.
	wcScheme := runtime.NewScheme()
	if err := corev1.AddToScheme(wcScheme); err != nil {
		t.Fatal(err)
	}
	wcClientBuilder := fakeclient.NewClientBuilder().WithScheme(wcScheme)
	for _, svc := range wcServices {
		wcClientBuilder = wcClientBuilder.WithObjects(svc)
	}
	dnsService.scope.SetClusterK8sClient(wcClientBuilder.Build())

	return dnsService
}

func TestService_getGatewayARecords(t *testing.T) {
	// Cluster domain for the test service: test-cluster.basedomain.io
	ctx := context.TODO()

	tests := []struct {
		name     string
		services []*corev1.Service
		want     []*armdns.RecordSet
	}{
		{
			name:     "returns nil when namespace has no services",
			services: nil,
			want:     nil,
		},
		{
			name: "skips service without annotations",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway",
						Namespace: gatewayNamespace,
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "skips service with wrong managed annotation value",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  "not-managed",
							externalDNSHostnameAnnotation: "gw.test-cluster.basedomain.io",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "skips service without hostname annotation",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation: externalDNSManagedValue,
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "skips non-LoadBalancer service",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "gw.test-cluster.basedomain.io",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
				},
			},
			want: nil,
		},
		{
			name: "skips LoadBalancer service with no IP assigned yet",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "gw.test-cluster.basedomain.io",
						},
					},
					Spec:   corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{},
				},
			},
			want: nil,
		},
		{
			name: "creates A record for valid gateway service",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "gw.test-cluster.basedomain.io",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
						},
					},
				},
			},
			want: []*armdns.RecordSet{
				{
					Name: pointer.String("gw"),
					Type: pointer.String("A"),
					Properties: &armdns.RecordSetProperties{
						TTL:      pointer.Int64(gatewayRecordTTL),
						ARecords: []*armdns.ARecord{{IPv4Address: pointer.String("1.2.3.4")}},
					},
				},
			},
		},
		{
			name: "creates A records for multiple valid gateway services",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway-a",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "app1.test-cluster.basedomain.io",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway-b",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "app2.test-cluster.basedomain.io",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "5.6.7.8"}},
						},
					},
				},
			},
			want: []*armdns.RecordSet{
				{
					Name: pointer.String("app1"),
					Type: pointer.String("A"),
					Properties: &armdns.RecordSetProperties{
						TTL:      pointer.Int64(gatewayRecordTTL),
						ARecords: []*armdns.ARecord{{IPv4Address: pointer.String("1.2.3.4")}},
					},
				},
				{
					Name: pointer.String("app2"),
					Type: pointer.String("A"),
					Properties: &armdns.RecordSetProperties{
						TTL:      pointer.Int64(gatewayRecordTTL),
						ARecords: []*armdns.ARecord{{IPv4Address: pointer.String("5.6.7.8")}},
					},
				},
			},
		},
		{
			name: "ignores services in other namespaces",
			services: []*corev1.Service{
				{
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway",
						Namespace: "kube-system",
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "gw.test-cluster.basedomain.io",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "mixed valid and invalid services only includes valid ones",
			services: []*corev1.Service{
				{
					// valid
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway-valid",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "gw.test-cluster.basedomain.io",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
						},
					},
				},
				{
					// invalid: no IP
					ObjectMeta: v1.ObjectMeta{
						Name:      "envoy-gateway-no-ip",
						Namespace: gatewayNamespace,
						Annotations: map[string]string{
							externalDNSManagedAnnotation:  externalDNSManagedValue,
							externalDNSHostnameAnnotation: "gw2.test-cluster.basedomain.io",
						},
					},
					Spec:   corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
					Status: corev1.ServiceStatus{},
				},
			},
			want: []*armdns.RecordSet{
				{
					Name: pointer.String("gw"),
					Type: pointer.String("A"),
					Properties: &armdns.RecordSetProperties{
						TTL:      pointer.Int64(gatewayRecordTTL),
						ARecords: []*armdns.ARecord{{IPv4Address: pointer.String("1.2.3.4")}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newGatewayTestService(t, ctx, tt.services)

			got, err := svc.getGatewayARecords(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(tt.want)
				t.Errorf("getGatewayARecords() = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}
