package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricControllerLabel = "controller"
	metricControllerValue = "dns-operator-azure"

	metricNamespace = "dns_operator_azure"

	MetricZone      = "zone"
	metricRecordSet = "record_set"
	metricAzure     = "api_request"

	ZoneType        = "type"
	ZoneTypePrivate = "private"
	ZoneTypePublic  = "public"
)

var (
	ZoneInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: MetricZone,
			Name:      "info",
			Help:      "Info about cluster DNS zone",
			ConstLabels: prometheus.Labels{
				metricControllerLabel: metricControllerValue,
			},
		}, []string{
			MetricZone,
			ZoneType,
			"resource_group",
			"tenant_id",
			"subscription_id",
		})

	ClusterZoneRecords = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: MetricZone,
			Name:      "records_sum",
			Help:      "Info about cluster",
			ConstLabels: prometheus.Labels{
				metricControllerLabel: metricControllerValue,
			},
		}, []string{
			MetricZone,
			ZoneType,
		})

	RecordInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: metricRecordSet,
			Name:      "info",
			Help:      "Info about existing record set",
			ConstLabels: prometheus.Labels{
				metricControllerLabel: metricControllerValue,
			},
		},
		[]string{
			MetricZone,
			ZoneType,
			"fqdn",
			"ip",
			"ttl",
		})

	AzureRequestError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricNamespace,
			Subsystem: metricAzure,
			Name:      "errors_total",
			Help:      "Total number of errors for an Azure API call",
		}, []string{"method"})
	AzureRequest = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricNamespace,
			Subsystem: metricAzure,
			Name:      "total",
			Help:      "Total number of Azure API calls",
			ConstLabels: prometheus.Labels{
				metricControllerLabel: metricControllerValue,
			},
		}, []string{"method"})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(ZoneInfo)
	metrics.Registry.MustRegister(ClusterZoneRecords)
	metrics.Registry.MustRegister(RecordInfo)

	metrics.Registry.MustRegister(AzureRequestError)
	metrics.Registry.MustRegister(AzureRequest)
}
