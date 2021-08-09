package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/azure"
)

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
			"DNSZone", dnsSpec.ZoneName,
			"alias", cnameRecord.Alias,
			"cname", cnameRecord.CName)
	}

	return nil
}

