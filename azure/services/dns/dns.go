package dns

import (
	"context"
	"fmt"

	azuredns "github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"

	"github.com/giantswarm/dns-operator-azure/azure"
)

const (
	RecordSetTypePrefix = "Microsoft.Network/dnszones/"
	RecordSetTypeA      = RecordSetTypePrefix + string(azuredns.A)
	RecordSetTypeCNAME  = RecordSetTypePrefix + string(azuredns.CNAME)
	RecordSetTypeNS     = RecordSetTypePrefix + string(azuredns.NS)
)

type Scope interface {
	logr.Logger
	azure.ResourceGroupDescriber
	DNSSpec() azure.DNSSpec
	SetNSRecordSetSpecs(nsRecordSetSpecs []azure.NSRecordSetSpec)
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
	s.Scope.Info("Reconcile DNS", "DNSZone", dnsSpec.ZoneName)

	var aRecordsToCreate []azure.ARecordSetSpec
	var cnameRecordsToCreate []azure.CNameRecordSetSpec
	var nsRecordsToCreate []azure.NSRecordSetSpec

	currentRecordSets, err := s.client.ListRecordSets(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName)
	if err != nil && azure.IsParentResourceNotFound(err) {
		s.Scope.V(2).Info("DNS zone not found", "DNSZone", dnsSpec.ZoneName)

		_, rErr := s.createDNSZone(ctx)
		if rErr != nil {
			return microerror.Mask(err)
		}

		aRecordsToCreate = dnsSpec.ARecordSets
		cnameRecordsToCreate = dnsSpec.CNameRecordSets
		nsRecordsToCreate = dnsSpec.NSRecordSets
	} else if err != nil {
		return microerror.Mask(err)
	} else {
		// We've got some records sets, let's fund if we have to create some
		// more. There shouldn't be many records, so let's do some brute force
		// search here :)
		s.Scope.V(2).Info("DNS zone found", "DNSZone", dnsSpec.ZoneName)

		// Finding missing A records and add them to the list of records that will be created.
		for _, desiredARecordSet := range dnsSpec.ARecordSets {
			foundRecord := false
			for _, recordSet := range currentRecordSets {
				if recordSet.Type != nil && *recordSet.Type == RecordSetTypeA &&
					recordSet.Name != nil && *recordSet.Name == desiredARecordSet.Hostname {
					foundRecord = true
					s.Scope.V(2).Info(
						fmt.Sprintf("DNS A record '%s' found", desiredARecordSet.Hostname),
						"DNSZone", dnsSpec.ZoneName,
						"hostname", desiredARecordSet.Hostname)
				}
			}

			if !foundRecord {
				aRecordsToCreate = append(aRecordsToCreate, desiredARecordSet)
				s.Scope.V(2).Info(
					fmt.Sprintf("DNS A record '%s' is missing, it will be created", desiredARecordSet.Hostname),
					"DNSZone", dnsSpec.ZoneName,
					"hostname", desiredARecordSet.Hostname)
			}
		}

		// Finding missing CNAME records and add them to the list of records that will be created.
		for _, desiredCName := range dnsSpec.CNameRecordSets {
			foundRecord := false
			for _, recordSet := range currentRecordSets {
				if recordSet.Type != nil && *recordSet.Type == RecordSetTypeCNAME &&
					recordSet.Name != nil && *recordSet.Name == desiredCName.Alias {
					foundRecord = true
					s.Scope.V(2).Info(
						fmt.Sprintf("DNS CNAME record '%s' found", desiredCName.Alias),
						"DNSZone", dnsSpec.ZoneName,
						"alias", desiredCName.Alias,
						"cname", desiredCName.CName)
				}
			}

			if !foundRecord {
				cnameRecordsToCreate = append(cnameRecordsToCreate, desiredCName)
				s.Scope.V(2).Info(
					fmt.Sprintf("DNS CNAME record '%s' is missing, it will be created", desiredCName.Alias),
					"DNSZone", dnsSpec.ZoneName,
					"alias", desiredCName.Alias,
					"cname", desiredCName.CName)
			}
		}

		// Finding missing NS records and add them to the list of records that will be created.
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

	// Create required A records.
	err = s.createARecords(ctx, aRecordsToCreate)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create required CName records.
	err = s.createCNameRecords(ctx, cnameRecordsToCreate)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create required NS records.
	err = s.createNSRecords(ctx, nsRecordsToCreate)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(currentRecordSets) == 0 {
		// If this was the first reconciliation loop for the workload cluster,
		// and the DNS zone just got created, here we fetch the records we need
		currentRecordSets, err = s.client.ListRecordSets(ctx, s.Scope.ResourceGroup(), dnsSpec.ZoneName)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// We update only NS records, since NS records from workload cluster are required to create
	nsRecords := filterAndGetNSRecordSetSpecs(currentRecordSets)
	if len(dnsSpec.NSRecordSets) == 0 {
		s.Scope.SetNSRecordSetSpecs(nsRecords)
	}

	s.Scope.Info("Successfully reconciled DNS", "DNSZone", dnsSpec.ZoneName)
	return nil
}

func (s *Service) createDNSZone(ctx context.Context) (*azuredns.Zone, error) {
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
