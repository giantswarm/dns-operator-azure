package infracluster

import (
	"context"
	"fmt"
	"net"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-08-01/network"
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
		return network.PublicIPAddress{}, fmt.Errorf("cluster %s does not have valid control plane IP address", s.cluster.Name)
	}
	return network.PublicIPAddress{
		PublicIPAddressPropertiesFormat: &network.PublicIPAddressPropertiesFormat{
			IPAddress: pointer.String(ip.String()),
		},
	}, nil
}
