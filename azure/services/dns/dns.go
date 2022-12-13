package dns

import (
	"context"

	azuredns "github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"

	"github.com/giantswarm/dns-operator-azure/azure"
	"github.com/giantswarm/dns-operator-azure/azure/scope"
)

const (
	RecordSetTypePrefix = "Microsoft.Network/dnszones/"
	RecordSetTypeA      = RecordSetTypePrefix + string(azuredns.A)
	RecordSetTypeCNAME  = RecordSetTypePrefix + string(azuredns.CNAME)
	RecordSetTypeNS     = RecordSetTypePrefix + string(azuredns.NS)
)

// Service provides operations on Azure resources.
type Service struct {
	scope scope.DNSScope
	client

	publicIPsService *capzpublicips.Service
}

// New creates a new dns service.
func New(scope scope.DNSScope, publicIPsService *capzpublicips.Service) *Service {
	return &Service{
		Scope:            scope,
		client:           newClient(scope),
		publicIPsService: publicIPsService,
	}
}

// Reconcile creates or updates the DNS zone, and creates DNS A and CNAME records.
func (s *Service) Reconcile(ctx context.Context) error {
	clusterZoneName := s.scope.ClusterDomain()
	s.scope.Info("Reconcile DNS", "DNSZone", clusterZoneName)

	currentRecordSets, err := s.client.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		s.scope.V(2).Info("DNS zone not found", "DNSZone", clusterZoneName)

		_, rErr := s.createClusterDNSZone(ctx)
		if rErr != nil {
			return microerror.Mask(err)
		}

		currentRecordSets, err = s.client.ListRecordSets(ctx, s.scope.ResourceGroup(), clusterZoneName)
		if err != nil {
			return microerror.Mask(err)
		}

	} else if err != nil {
		return microerror.Mask(err)
	}

	// Create required A records.
	err = s.updateARecords(ctx, currentRecordSets)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create required CName records.
	err = s.updateCNameRecords(ctx, currentRecordSets)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create required NS records.
	err = s.updateNSRecords(ctx, currentRecordSets)
	if err != nil {
		return microerror.Mask(err)
	}

	s.scope.Info("Successfully reconciled DNS", "DNSZone", clusterZoneName)
	return nil
}

func (s *Service) ReconcileDelete(ctx context.Context) error {
	return nil
}

func (s *Service) createClusterDNSZone(ctx context.Context) (*azuredns.Zone, error) {
	var dnsZone *azuredns.Zone
	var err error
	zoneName := s.scope.ClusterDomain()
	s.scope.V(2).Info("Creating DNS zone", "DNSZone", zoneName)

	// DNS zone not found, let's create it.
	dnsZoneParams := azuredns.Zone{
		Name:     &zoneName,
		Type:     to.StringPtr(string(azuredns.Public)),
		Location: to.StringPtr(capzazure.Global),
	}
	dnsZone, err = s.client.CreateOrUpdateZone(ctx, s.scope.ResourceGroup(), zoneName, dnsZoneParams)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	s.scope.V(2).Info("Successfully created DNS zone", "DNSZone", zoneName)

	return dnsZone, nil
}

// func (s *Service) deleteClusterRecords(ctx context.Context, hostedZoneID string) error

// func (s *Service) describeHostedZone(ctx context.Context) (string, error) {
// 	// input := &route53.ListHostedZonesByNameInput{
// 	// 	DNSName: aws.String(s.scope.BaseDomain()),
// 	// }
// 	// out, err := s.Route53Client.ListHostedZonesByNameWithContext(ctx, input)
// 	// if err != nil {
// 	// 	return "", wrapRoute53Error(err)
// 	// }
// 	// if len(out.HostedZones) == 0 {
// 	// 	return "", microerror.Mask(hostedZoneNotFoundError)
// 	// }

// 	// if *out.HostedZones[0].Name != fmt.Sprintf("%s.", s.scope.BaseDomain()) {
// 	// 	return "", microerror.Mask(hostedZoneNotFoundError)
// 	// }

// 	// return *out.HostedZones[0].Id, nil
// 	return "", nil
// }
