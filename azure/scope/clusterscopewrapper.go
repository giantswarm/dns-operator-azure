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
	nsRecordSetSpecs []azure.NSRecordSetSpec

	managementClusterName string
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

func (s *clusterScopeWrapper) SetNSRecordSetSpecs(nsRecordSetSpecs []azure.NSRecordSetSpec) {
	s.nsRecordSetSpecs = nsRecordSetSpecs
}

func (s *clusterScopeWrapper) DNSSpec() azure.DNSSpec {
	zoneName := fmt.Sprintf("%s.k8s.%s.%s.azure.gigantic.io",
		s.ClusterName(),
		s.managementClusterName,
		s.Location())

	dnsSpec := azure.DNSSpec{
		ZoneName: zoneName,
		ARecordSets: []azure.ARecordSetSpec{
			{
				Hostname:     "api",
				PublicIPName: s.APIServerPublicIP().Name,
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
		NSRecordSets: s.nsRecordSetSpecs,
	}

	return dnsSpec
}
