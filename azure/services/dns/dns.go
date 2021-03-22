package dns

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-06-01/network"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/cloud"
	capzscope "sigs.k8s.io/cluster-api-provider-azure/cloud/scope"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/cloud/services/publicips"

	"github.com/giantswarm/dns-operator-azure/azure"
)

type Scope interface {
	logr.Logger
	capzazure.ClusterDescriber
	DNSSpec() azure.DNSSpec
}

type clusterScopeWrapper struct {
	capzscope.ClusterScope
}

func (csw *clusterScopeWrapper) DNSSpec() azure.DNSSpec {
	zoneName := fmt.Sprintf("%s.k8s.%s.%s.azure.gigantic.io",
		csw.ClusterScope.ClusterName(),
		"ghost",
		csw.ClusterScope.Location())

	dnsSpec := azure.DNSSpec{
		ZoneName: zoneName,
		ARecords: []azure.ARecord{
			{
				Hostname:     "api",
				PublicIPName: csw.ClusterScope.APIServerPublicIP().Name,
				TTL:          3600,
			},
		},
		CNameRecords: []azure.CNameRecord{
			{
				Alias: "*",
				CName: fmt.Sprintf("ingress.%s", zoneName),
				TTL:   3600,
			},
		},
	}

	return dnsSpec
}

func NewClusterScopeWrapper(clusterScope capzscope.ClusterScope) Scope {
	return &clusterScopeWrapper{
		clusterScope,
	}
}

// Service provides operations on Azure resources.
type Service struct {
	Scope Scope
	client

	publicIPsService *capzpublicips.Service
}

// New creates a new dns service.
func New(scope Scope, publicIPsService *capzpublicips.Service) *Service {
	return &Service{
		Scope:            scope,
		client:           newClient(scope),
		publicIPsService: publicIPsService,
	}
}

// Reconcile creates or updates the DNS zone, and creates DNS A and CNAME records.
func (s *Service) Reconcile(ctx context.Context) error {
	dnsSpec := s.Scope.DNSSpec()
	var aRecordsToCreate []azure.ARecord
	var cnameRecordsToCreate []azure.CNameRecord

	currentRecordSets, err := s.client.ListRecordSets(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		s.Scope.V(2).Info("DNS zone not found", "dns zone", dnsSpec.ZoneName)

		_, rErr := s.reconcileWorkloadClusterDNSZone(ctx)
		if rErr != nil {
			return microerror.Mask(err)
		}

		aRecordsToCreate = dnsSpec.ARecords
		cnameRecordsToCreate = dnsSpec.CNameRecords
	} else if err != nil {
		return microerror.Mask(err)
	} else {
		// We've got some records sets, let's fund if we have to create some
		// more. There shouldn't be many records, so let's do some brute force
		// search here :)

		// Finding missing A records
		for _, a := range dnsSpec.ARecords {
			foundRecord := false
			for _, recordSet := range currentRecordSets {
				if recordSet.Type != nil && *recordSet.Type == string(dns.A) &&
					recordSet.Name != nil && *recordSet.Name == a.Hostname {
					foundRecord = true
				}
			}

			if !foundRecord {
				aRecordsToCreate = append(aRecordsToCreate, a)
			}
		}

		// Finding missing CNAME records
		for _, cname := range dnsSpec.CNameRecords {
			foundRecord := false
			for _, recordSet := range currentRecordSets {
				if recordSet.Type != nil && *recordSet.Type == string(dns.CNAME) &&
					recordSet.Name != nil && *recordSet.Name == cname.Alias {
					foundRecord = true
				}
			}

			if !foundRecord {
				cnameRecordsToCreate = append(cnameRecordsToCreate, cname)
			}
		}
	}

	err = s.createARecords(ctx, aRecordsToCreate)
	if err != nil {
		return microerror.Mask(err)
	}

	err = s.createCNameRecords(ctx, cnameRecordsToCreate)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (s *Service) reconcileWorkloadClusterDNSZone(ctx context.Context) (*dns.Zone, error) {
	var dnsZone *dns.Zone
	var err error
	dnsSpec := s.Scope.DNSSpec()
	s.Scope.V(2).Info("reconciling DNS zone", "dns zone", dnsSpec.ZoneName)

	// Try to get existing DNS zone from the workload cluster
	dnsZone, err = s.client.GetZone(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName)
	if err != nil && !capzazure.ResourceNotFound(err) {
		return nil, microerror.Mask(err)
	}

	if capzazure.ResourceNotFound(err) {
		s.Scope.V(2).Info("Creating DNS zone", "dns zone", dnsSpec.ZoneName)
		// DNS zone not found, let's create it.
		dnsZoneParams := dns.Zone{
			Name:     &dnsSpec.ZoneName,
			Type:     to.StringPtr(string(dns.Public)),
			Location: to.StringPtr(capzazure.Global),
		}
		dnsZone, err = s.client.CreateOrUpdateZone(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName, dnsZoneParams)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		s.Scope.V(2).Info("Successfully created DNS zone", "dns zone", dnsSpec.ZoneName)
	}

	return dnsZone, nil
}

func (s *Service) createARecords(ctx context.Context, aRecords []azure.ARecord) error {
	var err error
	dnsSpec := s.Scope.DNSSpec()

	for _, aRecord := range aRecords {
		var ipAddressObject network.PublicIPAddress
		ipAddressObject, err = s.publicIPsService.Get(ctx, s.Scope.ResourceGroup(), aRecord.PublicIPName)
		if capzazure.ResourceNotFound(err) {
			s.Scope.V(2).Info(
				"Cannot create DNS A record, public IP still not deployed",
				"dns zone", dnsSpec.ZoneName,
				"hostname", aRecord.Hostname,
				"IP resource name", aRecord.PublicIPName)
			continue
		} else if err != nil {
			return microerror.Mask(err)
		}

		if ipAddressObject.IPAddress == nil {
			s.Scope.V(2).Info(
				"Cannot create DNS A record, public Azure IP object does not have IP address set",
				"dns zone", dnsSpec.ZoneName,
				"hostname", aRecord.Hostname,
				"IP resource name", aRecord.PublicIPName)
			continue
		}

		s.Scope.V(2).Info(
			"Creating DNS A record",
			"dns zone", dnsSpec.ZoneName,
			"hostname", aRecord.Hostname,
			"ipv4", *ipAddressObject.IPAddress)

		recordSet := dns.RecordSet{
			Type: to.StringPtr(string(dns.A)),
			RecordSetProperties: &dns.RecordSetProperties{
				ARecords: &[]dns.ARecord{
					{
						Ipv4Address: ipAddressObject.IPAddress,
					},
				},
				TTL: to.Int64Ptr(aRecord.TTL),
			},
		}
		_, err := s.client.CreateOrUpdateRecordSet(
			ctx,
			s.Scope.ResourceGroup(),
			dnsSpec.ZoneName,
			dns.A,
			aRecord.Hostname,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.Scope.V(2).Info(
			"Successfully created DNS A record",
			"dns zone", dnsSpec.ZoneName,
			"hostname", aRecord.Hostname,
			"ipv4", aRecord.PublicIPName)
	}

	return nil
}

func (s *Service) createCNameRecords(ctx context.Context, cnameRecords []azure.CNameRecord) error {
	dnsSpec := s.Scope.DNSSpec()
	for _, cnameRecord := range cnameRecords {
		s.Scope.V(2).Info(
			"Creating DNS CNAME record",
			"dns zone", dnsSpec.ZoneName,
			"alias", cnameRecord.Alias,
			"cname", cnameRecord.CName)

		recordSet := dns.RecordSet{
			Type: to.StringPtr(string(dns.CNAME)),
			RecordSetProperties: &dns.RecordSetProperties{
				CnameRecord: &dns.CnameRecord{
					Cname: to.StringPtr(cnameRecord.CName),
				},
				TTL: to.Int64Ptr(cnameRecord.TTL),
			},
		}
		_, err := s.client.CreateOrUpdateRecordSet(
			ctx,
			s.Scope.ResourceGroup(),
			dnsSpec.ZoneName,
			dns.CNAME,
			cnameRecord.Alias,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.Scope.V(2).Info(
			"Successfully created DNS CNAME record",
			"dns zone", dnsSpec.ZoneName,
			"alias", cnameRecord.Alias,
			"cname", cnameRecord.CName)
	}

	return nil
}
