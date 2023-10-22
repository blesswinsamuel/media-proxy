package mediaprocessor

import (
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/prometheus/client_golang/prometheus"
)

func NewVipsPrometheusCollector() prometheus.Collector {
	return &VipsPrometheusCollector{}
}

type VipsPrometheusCollector struct {
}

func (c *VipsPrometheusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc("vips_stats", "Vips stats", []string{"type", "name"}, nil)
}

func (c *VipsPrometheusCollector) Collect(ch chan<- prometheus.Metric) {
	runtimeStats := vips.RuntimeStats{}
	vips.ReadRuntimeStats(&runtimeStats)
	for operationName, count := range runtimeStats.OperationCounts {
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc("media_proxy_vips_operation_count", "Vips operation count", []string{"operation"}, nil),
			prometheus.CounterValue,
			float64(count),
			operationName,
		)
	}
	memoryStats := vips.MemoryStats{}
	vips.ReadVipsMemStats(&memoryStats)
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("media_proxy_vips_memory_bytes", "", nil, nil),
		prometheus.GaugeValue,
		float64(memoryStats.Mem),
	)
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("media_proxy_vips_memory_highwater_bytes", "", nil, nil),
		prometheus.GaugeValue,
		float64(memoryStats.MemHigh),
	)
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("media_proxy_vips_memory_allocs", "", nil, nil),
		prometheus.GaugeValue,
		float64(memoryStats.MemHigh),
	)
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("media_proxy_vips_memory_files", "", nil, nil),
		prometheus.GaugeValue,
		float64(memoryStats.Files),
	)
}
