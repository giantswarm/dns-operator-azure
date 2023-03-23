package dns

import (
	"context"
	"fmt"
	"net"
	"reflect"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"golang.org/x/exp/slices"

	// Latest capz controller still depends on this library
	// https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/v1.6.0/azure/services/publicips/client.go#L56
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-08-01/network" //nolint
	"github.com/Azure/go-autorest/autorest/to"

	"github.com/giantswarm/microerror"
	"github.com/go-logr/logr"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	bastionRecordName   = "bastion"
	bastion1RecordName  = "bastion1"
	apiRecordName       = "api"
	apiserverRecordName = "apiserver"

	apiRecordTTL     = 300
	bastionRecordTTL = 300
)

func (s *Service) calculateMissingARecords(ctx context.Context, logger logr.Logger, currentRecordSets []*armdns.RecordSet) ([]*armdns.RecordSet, error) {
	desiredRecordSet, err := s.getDesiredARecords(ctx)
	if err != nil {
		return nil, err
	}

	var recordsToCreate []*armdns.RecordSet

	for _, desiredRecordSet := range desiredRecordSet {

		logger.V(1).Info(fmt.Sprintf("compare entries individually - %s", *desiredRecordSet.Name))

		currentRecordSetIndex := slices.IndexFunc(currentRecordSets, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == *desiredRecordSet.Name })
		if currentRecordSetIndex == -1 {
			recordsToCreate = append(recordsToCreate, desiredRecordSet)
		} else {
			// remove ProvisioningState from currentRecordSet to make further comparison easier
			currentRecordSets[currentRecordSetIndex].Properties.ProvisioningState = nil

			// compare ARecords[].IPv4Address
			if !reflect.DeepEqual(desiredRecordSet.Properties.ARecords, currentRecordSets[currentRecordSetIndex].Properties.ARecords) {
				logger.V(1).Info(fmt.Sprintf("A Records for %s are not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)
			}

			// compare TTL
			if !reflect.DeepEqual(desiredRecordSet.Properties.TTL, currentRecordSets[currentRecordSetIndex].Properties.TTL) {
				logger.V(1).Info(fmt.Sprintf("TTL for %s is not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)
			}
		}

	}

	return recordsToCreate, nil
}

func (s *Service) updateARecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	logger := log.FromContext(ctx).WithName("arecords")

	logger.V(1).Info("update A records", "current record sets", currentRecordSets)

	recordsToCreate, err := s.calculateMissingARecords(ctx, logger, currentRecordSets)
	if err != nil {
		return err
	}

	logger.V(1).Info("update A records", "records to create", recordsToCreate)

	if len(recordsToCreate) == 0 {
		logger.Info(
			"All DNS A records have already been created",
			"DNSZone", s.scope.ClusterZoneName())
		return nil
	}

	for _, aRecord := range recordsToCreate {

		logger.Info(
			fmt.Sprintf("DNS A record %s is missing, it will be created", to.String(aRecord.Name)),
			"DNSZone", s.scope.ClusterZoneName(),
			"FQDN", fmt.Sprintf("%s.%s", *aRecord.Name, s.scope.ClusterDomain()))

		logger.Info(
			"Creating DNS A record",
			"DNSZone", s.scope.ClusterZoneName(),
			"hostname", aRecord.Name,
			"ipv4", aRecord.Properties.ARecords)

		createdRecordSet, err := s.azureClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			s.scope.ClusterZoneName(),
			armdns.RecordTypeA,
			*aRecord.Name,
			*aRecord)
		if err != nil {
			return microerror.Mask(err)
		}

		logger.Info(
			"Successfully created DNS A record",
			"DNSZone", s.scope.ClusterZoneName(),
			"hostname", aRecord.Name,
			"id", createdRecordSet.ID)
	}

	return nil
}

// func (s *Service) getDesiredARecords() []azure.ARecordSetSpec {
func (s *Service) getDesiredARecords(ctx context.Context) ([]*armdns.RecordSet, error) {

	var armdnsRecordSet []*armdns.RecordSet

	armdnsRecordSet = append(armdnsRecordSet,
		// api A-Record
		&armdns.RecordSet{
			Name: to.StringPtr(apiRecordName),
			Type: to.StringPtr(string(armdns.RecordTypeA)),
			Properties: &armdns.RecordSetProperties{
				TTL: to.Int64Ptr(apiRecordTTL),
			},
		},
		// apiserver A-Record
		&armdns.RecordSet{
			Name: to.StringPtr(apiserverRecordName),
			Type: to.StringPtr(string(armdns.RecordTypeA)),
			Properties: &armdns.RecordSetProperties{
				TTL: to.Int64Ptr(apiRecordTTL),
			},
		})

	apiIndex := slices.IndexFunc(armdnsRecordSet, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == apiRecordName })
	apiserverIndex := slices.IndexFunc(armdnsRecordSet, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == apiserverRecordName })

	switch {
	case s.scope.IsAPIServerPrivate():

		armdnsRecordSet[apiIndex].Properties.ARecords = append(armdnsRecordSet[apiIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: to.StringPtr(s.scope.APIServerPrivateIP()),
		})

		armdnsRecordSet[apiserverIndex].Properties.ARecords = append(armdnsRecordSet[apiserverIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: to.StringPtr(s.scope.APIServerPrivateIP()),
		})

	case !s.scope.IsAPIServerPrivate():

		publicIP, err := s.getIPAddressForPublicDNS(ctx)
		if err != nil {
			return nil, err
		}

		armdnsRecordSet[apiIndex].Properties.ARecords = append(armdnsRecordSet[apiIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: to.StringPtr(publicIP),
		})

		armdnsRecordSet[apiserverIndex].Properties.ARecords = append(armdnsRecordSet[apiserverIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: to.StringPtr(publicIP),
		})

	}

	switch {
	case s.scope.BastionIPList() != "":

		armdnsRecordSet = append(armdnsRecordSet,
			// bastion A-Record
			&armdns.RecordSet{
				Name: to.StringPtr(bastionRecordName),
				Type: to.StringPtr(string(armdns.RecordTypeA)),
				Properties: &armdns.RecordSetProperties{
					TTL: to.Int64Ptr(bastionRecordTTL),
				},
			},
			// bastion1 A-Record
			&armdns.RecordSet{
				Name: to.StringPtr(bastion1RecordName),
				Type: to.StringPtr(string(armdns.RecordTypeA)),
				Properties: &armdns.RecordSetProperties{
					TTL: to.Int64Ptr(bastionRecordTTL),
				},
			})

		bastionIndex := slices.IndexFunc(armdnsRecordSet, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == bastionRecordName })
		bastion1Index := slices.IndexFunc(armdnsRecordSet, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == bastion1RecordName })

		for _, bastionIP := range s.scope.BastionIP() {

			armdnsRecordSet[bastionIndex].Properties.ARecords = append(armdnsRecordSet[bastionIndex].Properties.ARecords, &armdns.ARecord{
				IPv4Address: to.StringPtr(bastionIP),
			})

			armdnsRecordSet[bastion1Index].Properties.ARecords = append(armdnsRecordSet[bastion1Index].Properties.ARecords, &armdns.ARecord{
				IPv4Address: to.StringPtr(bastionIP),
			})
		}
	}

	return armdnsRecordSet, nil
}

func (s *Service) getIPAddressForPublicDNS(ctx context.Context) (string, error) {
	logger := log.FromContext(ctx).WithName("getIPAddressForPublicDNS")

	logger.V(1).Info(fmt.Sprintf("resolve IP for %s/%s", s.scope.APIServerPublicIP().Name, s.scope.APIServerPublicIP().DNSName))

	if net.ParseIP(s.scope.APIServerPublicIP().Name) == nil {
		publicIPIface, err := s.publicIPsService.Get(ctx, &capzpublicips.PublicIPSpec{
			Name:          s.scope.APIServerPublicIP().Name,
			ResourceGroup: s.scope.ResourceGroup(),
		})
		if err != nil {
			return "", microerror.Mask(err)
		}

		_, ok := publicIPIface.(network.PublicIPAddress)
		if !ok {
			return "", microerror.Mask(fmt.Errorf("%T is not a network.PublicIPAddress", publicIPIface))
		}

		logger.V(1).Info(fmt.Sprintf("got IP %v for %s/%s", *publicIPIface.(network.PublicIPAddress).IPAddress, s.scope.APIServerPublicIP().Name, s.scope.APIServerPublicIP().DNSName))

		return *publicIPIface.(network.PublicIPAddress).IPAddress, nil
	}

	return "", nil
}
