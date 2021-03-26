package dns

import (
	"context"
	"fmt"

	azuredns "github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-06-01/network"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/cloud"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/cloud/services/publicips"

	"github.com/giantswarm/dns-operator-azure/azure"
)

const (
	RecordSetTypeA     = "Microsoft.Network/dnszones/" + string(azuredns.A)
	RecordSetTypeCNAME = "Microsoft.Network/dnszones/" + string(azuredns.CNAME)
	RecordSetTypeNS    = "Microsoft.Network/dnszones/" + string(azuredns.NS)
)

type Scope interface {
	logr.Logger
	capzazure.ClusterDescriber
	DNSSpec() azure.DNSSpec
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
func (s *Service) Reconcile(ctx context.Context) ([]azuredns.RecordSet, error) {
	dnsSpec := s.Scope.DNSSpec()
	var aRecordsToCreate []azure.ARecordSetSpec
	var cnameRecordsToCreate []azure.CNameRecordSetSpec
	var nsRecordsToCreate []azure.NSRecordSetSpec

	currentRecordSets, err := s.client.ListRecordSets(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		s.Scope.V(2).Info("DNS zone not found", "DNSZone", dnsSpec.ZoneName)

		_, rErr := s.reconcileWorkloadClusterDNSZone(ctx)
		if rErr != nil {
			return nil, microerror.Mask(err)
		}

		aRecordsToCreate = dnsSpec.ARecordSets
		cnameRecordsToCreate = dnsSpec.CNameRecordSets
		nsRecordsToCreate = dnsSpec.NSRecordSets
	} else if err != nil {
		return nil, microerror.Mask(err)
	} else {
		// We've got some records sets, let's fund if we have to create some
		// more. There shouldn't be many records, so let's do some brute force
		// search here :)
		s.Scope.V(2).Info("DNS zone found", "DNSZone", dnsSpec.ZoneName)

		// Finding missing A records
		for _, a := range dnsSpec.ARecordSets {
			foundRecord := false
			for _, recordSet := range currentRecordSets {
				if recordSet.Type != nil && *recordSet.Type == RecordSetTypeA &&
					recordSet.Name != nil && *recordSet.Name == a.Hostname {
					foundRecord = true
					s.Scope.V(2).Info(
						fmt.Sprintf("DNS A record '%s' found", a.Hostname),
						"DNSZone", dnsSpec.ZoneName,
						"hostname", a.Hostname)
				}
			}

			if !foundRecord {
				aRecordsToCreate = append(aRecordsToCreate, a)
				s.Scope.V(2).Info(
					fmt.Sprintf("DNS A record '%s' is missing, it will be created", a.Hostname),
					"DNSZone", dnsSpec.ZoneName,
					"hostname", a.Hostname)
			}
		}

		// Finding missing CNAME records
		for _, cname := range dnsSpec.CNameRecordSets {
			foundRecord := false
			for _, recordSet := range currentRecordSets {
				if recordSet.Type != nil && *recordSet.Type == RecordSetTypeCNAME &&
					recordSet.Name != nil && *recordSet.Name == cname.Alias {
					foundRecord = true
					s.Scope.V(2).Info(
						fmt.Sprintf("DNS CNAME record '%s' found", cname.Alias),
						"DNSZone", dnsSpec.ZoneName,
						"alias", cname.Alias,
						"cname", cname.CName)
				}
			}

			if !foundRecord {
				cnameRecordsToCreate = append(cnameRecordsToCreate, cname)
				s.Scope.V(2).Info(
					fmt.Sprintf("DNS CNAME record '%s' is missing, it will be created", cname.Alias),
					"DNSZone", dnsSpec.ZoneName,
					"alias", cname.Alias,
					"cname", cname.CName)
			}
		}

		// Finding missing NS records
		for _, nsRecordSet := range dnsSpec.NSRecordSets {
			foundRecord := false
			for _, recordSet := range currentRecordSets {
				if recordSet.Type != nil && *recordSet.Type == RecordSetTypeNS &&
					recordSet.Name != nil && *recordSet.Name == nsRecordSet.Name {
					foundRecord = true
					s.Scope.V(2).Info(
						fmt.Sprintf("DNS NS record '%s' found", nsRecordSet.Name),
						"DNSZone", dnsSpec.ZoneName,
						"name", nsRecordSet.Name)
				}
			}

			if !foundRecord {
				nsRecordsToCreate = append(nsRecordsToCreate, nsRecordSet)
				s.Scope.V(2).Info(
					fmt.Sprintf("DNS NS record '%s' is missing, it will be created", nsRecordSet.Name),
					"DNSZone", dnsSpec.ZoneName,
					"name", nsRecordSet.Name)
			}
		}
	}

	err = s.createARecords(ctx, aRecordsToCreate)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	err = s.createCNameRecords(ctx, cnameRecordsToCreate)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	err = s.createNSRecords(ctx, nsRecordsToCreate)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	if len(currentRecordSets) == 0 {
		// If this was the first reconciliation loop for the workload cluster,
		// and the DNS zone just got created, here we fetch the records we need
		currentRecordSets, err = s.client.ListRecordSets(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	return currentRecordSets, nil
}

func (s *Service) reconcileWorkloadClusterDNSZone(ctx context.Context) (*azuredns.Zone, error) {
	var dnsZone *azuredns.Zone
	var err error
	dnsSpec := s.Scope.DNSSpec()
	s.Scope.V(2).Info("Creating DNS zone", "DNSZone", dnsSpec.ZoneName)

	// DNS zone not found, let's create it.
	dnsZoneParams := azuredns.Zone{
		Name:     &dnsSpec.ZoneName,
		Type:     to.StringPtr(string(azuredns.Public)),
		Location: to.StringPtr(capzazure.Global),
	}
	dnsZone, err = s.client.CreateOrUpdateZone(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName, dnsZoneParams)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	s.Scope.V(2).Info("Successfully created DNS zone", "DNSZone", dnsSpec.ZoneName)

	return dnsZone, nil
}

func (s *Service) createARecords(ctx context.Context, aRecords []azure.ARecordSetSpec) error {
	var err error
	dnsSpec := s.Scope.DNSSpec()
	if len(aRecords) == 0 {
		s.Scope.V(2).Info(
			"All DNS A records have already been created",
			"DNSZone", dnsSpec.ZoneName)
		return nil
	}

	for _, aRecord := range aRecords {
		var ipAddressObject network.PublicIPAddress
		ipAddressObject, err = s.publicIPsService.Get(ctx, s.Scope.ResourceGroup(), aRecord.PublicIPName)
		if capzazure.ResourceNotFound(err) {
			s.Scope.V(2).Info(
				"Cannot create DNS A record, public IP still not deployed",
				"DNSZone", dnsSpec.ZoneName,
				"hostname", aRecord.Hostname,
				"IP resource name", aRecord.PublicIPName)
			continue
		} else if err != nil {
			return microerror.Mask(err)
		}

		if ipAddressObject.IPAddress == nil {
			s.Scope.V(2).Info(
				"Cannot create DNS A record, public Azure IP object does not have IP address set",
				"DNSZone", dnsSpec.ZoneName,
				"hostname", aRecord.Hostname,
				"IP resource name", aRecord.PublicIPName)
			continue
		}

		s.Scope.V(2).Info(
			"Creating DNS A record",
			"DNSZone", dnsSpec.ZoneName,
			"hostname", aRecord.Hostname,
			"ipv4", *ipAddressObject.IPAddress)

		recordSet := azuredns.RecordSet{
			Type: to.StringPtr(string(azuredns.A)),
			RecordSetProperties: &azuredns.RecordSetProperties{
				ARecords: &[]azuredns.ARecord{
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
			azuredns.A,
			aRecord.Hostname,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.Scope.V(2).Info(
			"Successfully created DNS A record",
			"DNSZone", dnsSpec.ZoneName,
			"hostname", aRecord.Hostname,
			"ipv4", aRecord.PublicIPName)
	}

	return nil
}

func (s *Service) createCNameRecords(ctx context.Context, cnameRecords []azure.CNameRecordSetSpec) error {
	dnsSpec := s.Scope.DNSSpec()
	if len(cnameRecords) == 0 {
		s.Scope.V(2).Info(
			"All DNS CNAME records have already been created",
			"DNSZone", dnsSpec.ZoneName)
		return nil
	}

	for _, cnameRecord := range cnameRecords {
		s.Scope.V(2).Info(
			"Creating DNS CNAME record",
			"DNSZone", dnsSpec.ZoneName,
			"alias", cnameRecord.Alias,
			"cname", cnameRecord.CName)

		recordSet := azuredns.RecordSet{
			Type: to.StringPtr(string(azuredns.CNAME)),
			RecordSetProperties: &azuredns.RecordSetProperties{
				CnameRecord: &azuredns.CnameRecord{
					Cname: to.StringPtr(cnameRecord.CName),
				},
				TTL: to.Int64Ptr(cnameRecord.TTL),
			},
		}
		_, err := s.client.CreateOrUpdateRecordSet(
			ctx,
			s.Scope.ResourceGroup(),
			dnsSpec.ZoneName,
			azuredns.CNAME,
			cnameRecord.Alias,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.Scope.V(2).Info(
			"Successfully created DNS CNAME record",
			"DNSZone", dnsSpec.ZoneName,
			"alias", cnameRecord.Alias,
			"cname", cnameRecord.CName)
	}

	return nil
}

func (s *Service) createNSRecords(ctx context.Context, nsRecords []azure.NSRecordSetSpec) error {
	var err error
	dnsSpec := s.Scope.DNSSpec()
	if len(nsRecords) == 0 {
		s.Scope.V(2).Info(
			"All DNS NS records have already been created",
			"DNSZone", dnsSpec.ZoneName)
		return nil
	}

	for _, nsRecord := range nsRecords {
		var allNSDomainNames string
		var nsRecords []azuredns.NsRecord
		for i, nsdn := range nsRecord.NSDomainNames {
			if i > 0 {
				allNSDomainNames += ", " + nsdn.NSDomainName
			} else {
				allNSDomainNames += nsdn.NSDomainName
			}

			nsDomainName := nsdn.NSDomainName
			nsRecord := azuredns.NsRecord{
				Nsdname: &nsDomainName,
			}
			nsRecords = append(nsRecords, nsRecord)
		}

		s.Scope.V(2).Info(
			"Creating DNS NS record",
			"DNSZone", dnsSpec.ZoneName,
			"name", nsRecord.Name,
			"NSDomainNames", allNSDomainNames)

		recordSet := azuredns.RecordSet{
			Type: to.StringPtr(string(azuredns.NS)),
			RecordSetProperties: &azuredns.RecordSetProperties{
				NsRecords: &nsRecords,
				TTL:       to.Int64Ptr(nsRecord.TTL),
			},
		}
		_, err = s.client.CreateOrUpdateRecordSet(
			ctx,
			s.Scope.ResourceGroup(),
			dnsSpec.ZoneName,
			azuredns.NS,
			nsRecord.Name,
			recordSet)
		if err != nil {
			return microerror.Mask(err)
		}

		s.Scope.V(2).Info(
			"Successfully created DNS NS record",
			"DNSZone", dnsSpec.ZoneName,
			"name", nsRecord.Name,
			"NSDomainNames", allNSDomainNames)
	}

	return nil
}
