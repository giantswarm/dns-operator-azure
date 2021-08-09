package dns

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/dns/mgmt/2018-05-01/dns"
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-02-01/network"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/giantswarm/microerror"
	capzazure "sigs.k8s.io/cluster-api-provider-azure/azure"

	"github.com/giantswarm/dns-operator-azure/azure"
)

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
			"DNSZone", dnsSpec.ZoneName,
			"hostname", aRecord.Hostname,
			"ipv4", aRecord.PublicIPName)
	}

	return nil
}

