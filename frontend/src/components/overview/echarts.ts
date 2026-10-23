// The ECharts pieces the overview draws with, registered once. Modular imports
// keep the bundle to a heat map and a line; everything is bundled locally (no
// CDN, CSP connect-src stays the API origin only).
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { HeatmapChart, LineChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapPiecewiseComponent,
} from 'echarts/components'

use([
  CanvasRenderer,
  HeatmapChart,
  LineChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapPiecewiseComponent,
])

// Recessive chrome: ink for text, faint grid (styles/tokens.css, light theme).
export const CHART_INK = {
  text: '#09090b',
  muted: '#71717a',
  grid: '#ededef',
  axis: '#d4d4d8',
  surface: '#ffffff',
  zero: '#f4f4f5',
  hidden: '#e4e4e7',
}
