import { Dashboard, DashboardCursorSync, DataSourceRef, defaultDashboard } from '@grafana/schema'
import {
  NewGoRuntimeMetrics,
  NewPanelRow,
  NewPrometheusDatasource as NewPrometheusDatasourceVariable,
  NewTimeSeriesPanel,
  PanelRowAndGroups,
  Unit,
  autoLayout,
  writeDashboardAndPostToGrafana,
} from 'grafana-dashboard-helpers'

const datasource: DataSourceRef = {
  uid: '${DS_PROMETHEUS}',
}

const panels: PanelRowAndGroups = [
  NewPanelRow({ datasource, height: 8 }, [
    NewTimeSeriesPanel({
      title: 'Cache Files Count',
      targets: [{ expr: 'sum by (cache_path) (media_proxy_cache_fs_files_count)', legendFormat: '{{cache_path}}' }],
      defaultUnit: Unit.SHORT,
    }),
    NewTimeSeriesPanel({
      title: 'Cache Files Bytes',
      targets: [{ expr: 'sum by (cache_path) (media_proxy_cache_fs_size_bytes)', legendFormat: '{{cache_path}}' }],
      defaultUnit: Unit.BYTES_SI,
    }),
    NewTimeSeriesPanel({
      title: 'Loader Downloaded Bytes',
      targets: [{ expr: 'sum(increase(media_proxy_loader_response_size_bytes_sum))', legendFormat: 'value' }],
      defaultUnit: Unit.BYTES_SI,
    }),
  ]),
  NewPanelRow({ datasource, height: 8 }, [
    NewTimeSeriesPanel({
      title: 'Vips Memory Usage Bytes',
      targets: [{ expr: 'sum(media_proxy_vips_memory_bytes)', legendFormat: 'value' }],
      defaultUnit: Unit.BYTES_SI,
    }),
    NewTimeSeriesPanel({
      title: 'Vips Memory HighWater Bytes',
      targets: [{ expr: 'sum(media_proxy_vips_memory_highwater_bytes)', legendFormat: 'value' }],
      defaultUnit: Unit.BYTES_SI,
    }),
    NewTimeSeriesPanel({
      title: 'Vips Memory Alloc Bytes',
      targets: [{ expr: 'sum(media_proxy_vips_memory_allocs)', legendFormat: 'value' }],
      defaultUnit: Unit.BYTES_SI,
    }),
  ]),
  NewPanelRow({ datasource, height: 8 }, [
    NewTimeSeriesPanel({
      title: 'Vips Memory Files',
      targets: [{ expr: 'sum(media_proxy_vips_memory_files)', legendFormat: 'value' }],
      defaultUnit: Unit.BYTES_SI,
    }),
    NewTimeSeriesPanel({
      title: 'Vips Operation Count',
      targets: [{ expr: 'sum by (operation) (media_proxy_vips_operation_count)', legendFormat: '{{ operation }}' }],
      defaultUnit: Unit.SHORT,
    }),
  ]),
  NewGoRuntimeMetrics({ datasource, selector: '{job="media-proxy"}' }),
]

const dashboard: Dashboard = {
  ...defaultDashboard,
  description: 'Dashboard for media-proxy',
  graphTooltip: DashboardCursorSync.Crosshair,
  style: 'dark',
  tags: ['media-proxy'],
  time: {
    from: 'now-6h',
    to: 'now',
  },
  title: 'Media Proxy',
  uid: 'media-proxy',
  version: 1,
  panels: autoLayout(panels),
  templating: {
    list: [NewPrometheusDatasourceVariable({ name: 'DS_PROMETHEUS', label: 'Prometheus' })],
  },
}

writeDashboardAndPostToGrafana({
  grafanaURL: process.env.GRAFANA_URL,
  dashboard,
  filename: 'media-proxy.json',
})
