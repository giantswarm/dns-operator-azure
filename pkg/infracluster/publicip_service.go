package infracluster

import (
	"context"
	"fmt"
	"net"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v7"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

type PublicIPService struct {
	cluster *capi.Cluster
}

func NewPublicIPService(cluster *capi.Cluster) *PublicIPService {
	return &PublicIPService{
		cluster: cluster,
	}
}

func (s *PublicIPService) Get(ctx context.Context, spec azure.ResourceSpecGetter) (result interface{}, err error) {
	ip := net.ParseIP(s.cluster.Spec.ControlPlaneEndpoint.Host)
	if ip == nil {
		return armnetwork.PublicIPAddress{}, fmt.Errorf("cluster %s does not have valid control plane IP address", s.cluster.Name)
	}
	return armnetwork.PublicIPAddress{
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			IPAddress: pointer.String(ip.String()),
		},
	}, nil
}
