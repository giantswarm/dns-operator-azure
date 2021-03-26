package scope

import (
	"fmt"
	"os"

	"github.com/giantswarm/microerror"
	"sigs.k8s.io/cluster-api-provider-azure/cloud/scope"

	"github.com/giantswarm/dns-operator-azure/azure"
	"github.com/giantswarm/dns-operator-azure/azure/services/dns"
	"github.com/giantswarm/dns-operator-azure/pkg/errors"
)

type clusterScopeWrapper struct {
	scope.ClusterScope
	managementClusterName string
}

func (csw *clusterScopeWrapper) DNSSpec() azure.DNSSpec {
	zoneName := fmt.Sprintf("%s.k8s.%s.%s.azure.gigantic.io",
		csw.ClusterScope.ClusterName(),
		csw.managementClusterName,
		csw.ClusterScope.Location())

	dnsSpec := azure.DNSSpec{
		ZoneName: zoneName,
		ARecordSets: []azure.ARecordSetSpec{
			{
				Hostname:     "api",
				PublicIPName: csw.ClusterScope.APIServerPublicIP().Name,
				TTL:          3600,
			},
		},
		CNameRecordSets: []azure.CNameRecordSetSpec{
			{
				Alias: "*",
				CName: fmt.Sprintf("ingress.%s", zoneName),
				TTL:   3600,
			},
		},
	}

	return dnsSpec
}

func NewClusterScopeWrapper(clusterScope scope.ClusterScope) (dns.Scope, error) {
	managementClusterName := os.Getenv(ManagementClusterName)
	if managementClusterName == "" {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%s environment variable is not set", ManagementClusterName)
	}

	return &clusterScopeWrapper{
		ClusterScope:          clusterScope,
		managementClusterName: managementClusterName,
	}, nil
}
