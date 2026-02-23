package dns

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	kubeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/giantswarm/microerror"
)

const (
	gatewayNamespace              = "envoy-gateway-system"
	externalDNSManagedAnnotation  = "giantswarm.io/external-dns"
	externalDNSManagedValue       = "managed"
	externalDNSHostnameAnnotation = "external-dns.alpha.kubernetes.io/hostname"

	gatewayRecordTTL = 300
)

type gatewayService struct {
	hostname string
	ip       string
}

func (s *Service) updateGatewayARecords(ctx context.Context, currentRecordSets []*armdns.RecordSet) error {
	logger := log.FromContext(ctx).WithName("gatewayrecords")

	gateways, err := s.getGatewayServices(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(gateways) == 0 {
		logger.Info("No gateway services found", "DNSZone", s.scope.ClusterDomain())
		return nil
	}

	recordsToCreate := s.calculateMissingGatewayARecords(logger, currentRecordSets, gateways)

	if len(recordsToCreate) == 0 {
		logger.Info(
			"All DNS gateway A records have already been created",
			"DNSZone", s.scope.ClusterDomain())
		return nil
	}

	for _, aRecord := range recordsToCreate {
		logger.Info(
			fmt.Sprintf("DNS gateway A record %s is missing, it will be created", *aRecord.Name),
			"DNSZone", s.scope.ClusterDomain(),
			"FQDN", fmt.Sprintf("%s.%s", *aRecord.Name, s.scope.ClusterDomain()))

		logger.Info(
			"Creating DNS gateway A record",
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
			"Successfully created DNS gateway A record",
			"DNSZone", s.scope.ClusterDomain(),
			"hostname", aRecord.Name,
			"id", createdRecordSet.ID)
	}

	return nil
}

func (s *Service) calculateMissingGatewayARecords(logger logr.Logger, currentRecordSets []*armdns.RecordSet, gateways []gatewayService) []*armdns.RecordSet {
	clusterZoneName := s.scope.ClusterDomain()

	var desiredRecords []*armdns.RecordSet
	for _, gw := range gateways {
		recordName := strings.TrimSuffix(gw.hostname, "."+clusterZoneName)
		desiredRecords = append(desiredRecords, &armdns.RecordSet{
			Name: pointer.String(recordName),
			Type: pointer.String(string(armdns.RecordTypeA)),
			Properties: &armdns.RecordSetProperties{
				TTL: pointer.Int64(gatewayRecordTTL),
				ARecords: []*armdns.ARecord{
					{IPv4Address: pointer.String(gw.ip)},
				},
			},
		})
	}

	var recordsToCreate []*armdns.RecordSet
	for _, desiredRecord := range desiredRecords {
		logger.V(1).Info(fmt.Sprintf("compare gateway entries individually - %s", *desiredRecord.Name))

		currentIdx := slices.IndexFunc(currentRecordSets, func(rs *armdns.RecordSet) bool {
			return *rs.Name == *desiredRecord.Name
		})
		if currentIdx < 0 {
			recordsToCreate = append(recordsToCreate, desiredRecord)
		} else {
			current := currentRecordSets[currentIdx]
			current.Properties.ProvisioningState = nil
			switch {
			case !reflect.DeepEqual(desiredRecord.Properties.ARecords, current.Properties.ARecords):
				logger.V(1).Info(fmt.Sprintf("A records for gateway %s are not equal - force update", *desiredRecord.Name))
				recordsToCreate = append(recordsToCreate, desiredRecord)
			case !reflect.DeepEqual(desiredRecord.Properties.TTL, current.Properties.TTL):
				logger.V(1).Info(fmt.Sprintf("TTL for gateway %s is not equal - force update", *desiredRecord.Name))
				recordsToCreate = append(recordsToCreate, desiredRecord)
			}
		}
	}

	return recordsToCreate
}

func (s *Service) getGatewayServices(ctx context.Context) ([]gatewayService, error) {
	k8sClient, err := s.scope.ClusterK8sClient(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var services corev1.ServiceList
	if err = k8sClient.List(ctx, &services, kubeclient.InNamespace(gatewayNamespace)); err != nil {
		return nil, microerror.Mask(err)
	}

	var result []gatewayService
	for _, svc := range services.Items {
		if svc.Annotations[externalDNSManagedAnnotation] != externalDNSManagedValue {
			continue
		}
		hostname, ok := svc.Annotations[externalDNSHostnameAnnotation]
		if !ok || hostname == "" {
			continue
		}
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		if len(svc.Status.LoadBalancer.Ingress) < 1 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
			continue
		}
		result = append(result, gatewayService{
			hostname: hostname,
			ip:       svc.Status.LoadBalancer.Ingress[0].IP,
		})
	}

	return result, nil
}
