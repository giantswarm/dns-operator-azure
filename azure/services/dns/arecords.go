package dns

import (
	"context"
	"fmt"
	"net"
	"reflect"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	// Latest capz controller still depends on this library
	// https://github.com/kubernetes-sigs/cluster-api-provider-azure/blob/v1.6.0/azure/services/publicips/client.go#L56
	//nolint

	"github.com/go-logr/logr"
	capzpublicips "sigs.k8s.io/cluster-api-provider-azure/azure/services/publicips"
	kubeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/dns-operator-azure/v3/pkg/metrics"
)

const (
	apiRecordName       = "api"
	apiserverRecordName = "apiserver"
	ingressRecordName   = "ingress"

	apiRecordTTL     = 300
	ingressRecordTTL = 300

	ingressServiceSelector = "app.kubernetes.io/name in (ingress-nginx,nginx-ingress-controller)"
	ingressAppNamespace    = "kube-system"
)

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
			"DNSZone", s.scope.ClusterDomain())
		return nil
	}

	for _, aRecord := range recordsToCreate {

		logger.Info(
			fmt.Sprintf("DNS A record %s is missing, it will be created", *aRecord.Name),
			"DNSZone", s.scope.ClusterDomain(),
			"FQDN", fmt.Sprintf("%s.%s", *aRecord.Name, s.scope.ClusterDomain()))

		logger.Info(
			"Creating DNS A record",
			"DNSZone", s.scope.ClusterDomain(),
			"hostname", aRecord.Name,
			"ipv4", aRecord.Properties.ARecords)

		createdRecordSet, err := s.azureClient.CreateOrUpdateRecordSet(
			ctx,
			s.scope.ResourceGroup(),
			s.scope.ClusterDomain(),
			armdns.RecordTypeA,
			*aRecord.Name,
			*aRecord)
		if err != nil {
			return microerror.Mask(err)
		}

		logger.Info(
			"Successfully created DNS A record",
			"DNSZone", s.scope.ClusterDomain(),
			"hostname", aRecord.Name,
			"id", createdRecordSet.ID)
	}

	return nil
}

func (s *Service) calculateMissingARecords(ctx context.Context, logger logr.Logger, currentRecordSets []*armdns.RecordSet) ([]*armdns.RecordSet, error) {
	desiredRecordSets, err := s.getDesiredARecords(ctx)
	if err != nil {
		return nil, err
	}

	var recordsToCreate []*armdns.RecordSet

	for _, desiredRecordSet := range desiredRecordSets {

		logger.V(1).Info(fmt.Sprintf("compare entries individually - %s", *desiredRecordSet.Name))

		currentRecordSetIndex := slices.IndexFunc(currentRecordSets, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == *desiredRecordSet.Name })
		if currentRecordSetIndex == -1 {
			recordsToCreate = append(recordsToCreate, desiredRecordSet)
		} else {
			// remove ProvisioningState from currentRecordSet to make further comparison easier
			currentRecordSets[currentRecordSetIndex].Properties.ProvisioningState = nil

			switch {
			// compare ARecords[].IPv4Address
			case !reflect.DeepEqual(
				desiredRecordSet.Properties.ARecords,
				currentRecordSets[currentRecordSetIndex].Properties.ARecords,
			):
				logger.V(1).Info(fmt.Sprintf("A Records for %s are not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)
			// compare TTL
			case !reflect.DeepEqual(
				desiredRecordSet.Properties.TTL,
				currentRecordSets[currentRecordSetIndex].Properties.TTL,
			):
				logger.V(1).Info(fmt.Sprintf("TTL for %s is not equal - force update", *desiredRecordSet.Name))
				recordsToCreate = append(recordsToCreate, desiredRecordSet)

			}

			for _, ip := range currentRecordSets[currentRecordSetIndex].Properties.ARecords {
				// dns_operator_azure_record_set_info{controller="dns-operator-azure",fqdn="api.glippy.azuretest.gigantic.io",ip="20.4.101.180",ttl="300"} 1
				metrics.RecordInfo.WithLabelValues(
					s.scope.ClusterDomain(), // label: zone
					metrics.ZoneTypePublic,  // label: type
					fmt.Sprintf("%s.%s", *currentRecordSets[currentRecordSetIndex].Name, s.scope.ClusterDomain()), // label: fqdn
					*ip.IPv4Address, // label: ip
					fmt.Sprint(*currentRecordSets[currentRecordSetIndex].Properties.TTL), // label: ttl
				).Set(1)
			}
		}
	}

	return recordsToCreate, nil
}

func (s *Service) getDesiredARecords(ctx context.Context) ([]*armdns.RecordSet, error) {

	var armdnsRecordSet []*armdns.RecordSet

	armdnsRecordSet = append(armdnsRecordSet,
		// api A-Record
		&armdns.RecordSet{
			Name: pointer.String(apiRecordName),
			Type: pointer.String(string(armdns.RecordTypeA)),
			Properties: &armdns.RecordSetProperties{
				TTL: pointer.Int64(apiRecordTTL),
			},
		},
		// apiserver A-Record
		&armdns.RecordSet{
			Name: pointer.String(apiserverRecordName),
			Type: pointer.String(string(armdns.RecordTypeA)),
			Properties: &armdns.RecordSetProperties{
				TTL: pointer.Int64(apiRecordTTL),
			},
		})

	apiIndex := slices.IndexFunc(armdnsRecordSet, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == apiRecordName })
	apiserverIndex := slices.IndexFunc(armdnsRecordSet, func(recordSet *armdns.RecordSet) bool { return *recordSet.Name == apiserverRecordName })

	switch {
	case s.scope.Patcher.IsAPIServerPrivate():

		armdnsRecordSet[apiIndex].Properties.ARecords = append(armdnsRecordSet[apiIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: pointer.String(s.scope.Patcher.APIServerPrivateIP()),
		})

		armdnsRecordSet[apiserverIndex].Properties.ARecords = append(armdnsRecordSet[apiserverIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: pointer.String(s.scope.Patcher.APIServerPrivateIP()),
		})

	case !s.scope.Patcher.IsAPIServerPrivate():

		publicIP, err := s.getIPAddressForPublicDNS(ctx)
		if err != nil {
			return nil, err
		}

		armdnsRecordSet[apiIndex].Properties.ARecords = append(armdnsRecordSet[apiIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: pointer.String(publicIP),
		})

		armdnsRecordSet[apiserverIndex].Properties.ARecords = append(armdnsRecordSet[apiserverIndex].Properties.ARecords, &armdns.ARecord{
			IPv4Address: pointer.String(publicIP),
		})

	}

	if !s.scope.IsAzureCluster() {
		ingressIP, err := s.getIngressIP(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		// TODO: Should the reconciliation fail in case the ingress IP is empty?
		if ingressIP != "" {
			armdnsRecordSet = append(armdnsRecordSet, &armdns.RecordSet{
				Name: pointer.String(ingressRecordName),
				Type: pointer.String(string(armdns.RecordTypeA)),
				Properties: &armdns.RecordSetProperties{
					TTL: pointer.Int64(ingressRecordTTL),
					ARecords: []*armdns.ARecord{
						{IPv4Address: pointer.String(ingressIP)},
					},
				},
			})
		}
	}

	return armdnsRecordSet, nil
}

func (s *Service) getIPAddressForPublicDNS(ctx context.Context) (string, error) {
	logger := log.FromContext(ctx).WithName("getIPAddressForPublicDNS")

	logger.V(1).Info(fmt.Sprintf("resolve IP for %s/%s", s.scope.Patcher.APIServerPublicIP().Name, s.scope.Patcher.APIServerPublicIP().DNSName))

	if net.ParseIP(s.scope.Patcher.APIServerPublicIP().Name) == nil {
		publicIPIface, err := s.publicIPsService.Get(ctx, &capzpublicips.PublicIPSpec{
			Name:          s.scope.Patcher.APIServerPublicIP().Name,
			ResourceGroup: s.scope.ResourceGroup(),
		})
		if err != nil {
			return "", microerror.Mask(err)
		}

		_, ok := publicIPIface.(armnetwork.PublicIPAddress)
		if !ok {
			return "", microerror.Mask(fmt.Errorf("%T is not a armnetwork.PublicIPAddress", publicIPIface))
		}

		logger.V(1).Info(fmt.Sprintf("got IP %v for %s/%s", *publicIPIface.(armnetwork.PublicIPAddress).Properties.IPAddress, s.scope.Patcher.APIServerPublicIP().Name, s.scope.Patcher.APIServerPublicIP().DNSName))

		return *publicIPIface.(armnetwork.PublicIPAddress).Properties.IPAddress, nil
	}

	return s.scope.Patcher.APIServerPublicIP().Name, nil
}

func (s *Service) getIngressIP(ctx context.Context) (string, error) {
	k8sClient, err := s.scope.ClusterK8sClient(ctx)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var icServices corev1.ServiceList

	err = k8sClient.List(ctx, &icServices,
		kubeclient.InNamespace(ingressAppNamespace),
		&kubeclient.ListOptions{Raw: &metav1.ListOptions{LabelSelector: ingressServiceSelector}},
	)

	if err != nil {
		return "", microerror.Mask(err)
	}

	var icServiceIP string

	for _, icService := range icServices.Items {
		if icService.Spec.Type == corev1.ServiceTypeLoadBalancer {
			if icServiceIP != "" {
				return "", microerror.Mask(tooManyICServicesError)
			}

			if len(icService.Status.LoadBalancer.Ingress) < 1 || icService.Status.LoadBalancer.Ingress[0].IP == "" {
				return "", microerror.Mask(ingressNotReadyError)
			}

			icServiceIP = icService.Status.LoadBalancer.Ingress[0].IP
		}
	}

	return icServiceIP, nil
}
