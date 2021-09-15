package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/azure"
)

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
		var nsRecords []dns.NsRecord
		for i, nsdn := range nsRecord.NSDomainNames {
			if i > 0 {
				allNSDomainNames += ", " + nsdn.NSDomainName
			} else {
				allNSDomainNames += nsdn.NSDomainName
			}

			nsDomainName := nsdn.NSDomainName
			nsRecord := dns.NsRecord{
				Nsdname: &nsDomainName,
			}
			nsRecords = append(nsRecords, nsRecord)
		}

		s.Scope.V(2).Info(
			"Creating DNS NS record",
			"DNSZone", dnsSpec.ZoneName,
			"name", nsRecord.Name,
			"NSDomainNames", allNSDomainNames)

		recordSet := dns.RecordSet{
			Type: to.StringPtr(string(dns.NS)),
			RecordSetProperties: &dns.RecordSetProperties{
				NsRecords: &nsRecords,
				TTL:       to.Int64Ptr(nsRecord.TTL),
			},
		}
		_, err = s.client.CreateOrUpdateRecordSet(
			ctx,
			s.Scope.ResourceGroup(),
			dnsSpec.ZoneName,
			dns.NS,
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
