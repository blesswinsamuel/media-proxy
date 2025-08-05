import {
  goRuntimeMetricsPanels,
  NewPanelRow,
  NewPrometheusDatasourceVariable,
  NewTimeSeriesPanel,
  PanelRowAndGroups,
  units,
  newDashboard,
  writeDashboardAndPostToGrafana,
  dashboard,
} from 'grafana-dashboard-helpers'

const datasource: dashboard.DataSourceRef = {
  uid: '${DS_PROMETHEUS}',
}

const panels: PanelRowAndGroups = [
  NewPanelRow({ datasource, height: 8 }, [
    NewTimeSeriesPanel(
      { title: 'Cache Files Count', unit: units.Short },
      { expr: 'sum by (cache_path) (media_proxy_cache_fs_files_count)', legendFormat: '{{cache_path}}' }
    ),
    NewTimeSeriesPanel(
      { title: 'Cache Files Bytes', unit: units.BytesSI },
      { expr: 'sum by (cache_path) (media_proxy_cache_fs_size_bytes)', legendFormat: '{{cache_path}}' }
    ),
    NewTimeSeriesPanel(
      { title: 'Loader Downloaded Bytes', unit: units.BytesSI },
      { expr: 'sum(increase(media_proxy_loader_response_size_bytes_sum))', legendFormat: 'value' }
    ),
  ]),
  NewPanelRow({ datasource, height: 8 }, [
    NewTimeSeriesPanel({ title: 'Vips Memory Usage Bytes', unit: units.BytesSI }, { expr: 'sum(media_proxy_vips_memory_bytes)', legendFormat: 'value' }),
    NewTimeSeriesPanel(
      { title: 'Vips Memory HighWater Bytes', unit: units.BytesSI },
      { expr: 'sum(media_proxy_vips_memory_highwater_bytes)', legendFormat: 'value' }
    ),
    NewTimeSeriesPanel({ title: 'Vips Memory Alloc Bytes', unit: units.BytesSI }, { expr: 'sum(media_proxy_vips_memory_allocs)', legendFormat: 'value' }),
  ]),
  NewPanelRow({ datasource, height: 8 }, [
    NewTimeSeriesPanel({ title: 'Vips Memory Files', unit: units.BytesSI }, { expr: 'sum(media_proxy_vips_memory_files)', legendFormat: 'value' }),
    NewTimeSeriesPanel(
      { title: 'Vips Operation Count', unit: units.Short },
      { expr: 'sum by (operation) (media_proxy_vips_operation_count)', legendFormat: '{{ operation }}' }
    ),
  ]),
  goRuntimeMetricsPanels({ datasource, selectors: 'job="media-proxy"' }),
]

const mediaProxyDashboard = newDashboard({
  title: 'Media Proxy',
  description: 'Dashboard for media-proxy',
  tags: ['media-proxy'],
  uid: 'media-proxy',
  time: {
    from: 'now-24h',
    to: 'now',
  },
  panels: panels,
  variables: [NewPrometheusDatasourceVariable({ name: 'DS_PROMETHEUS', label: 'Prometheus' })],
})

writeDashboardAndPostToGrafana({
  dashboard: mediaProxyDashboard.build(),
  filename: 'media-proxy.json',
})
