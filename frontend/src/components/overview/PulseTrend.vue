<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import type { EChartsCoreOption } from 'echarts/core'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Overview } from '@/types/api'
import { SERIES_COLORS, trendLines } from '@/lib/overview'
import { CHART_INK } from './echarts'

// Mood by week (ux-spec 3.1, 3.7): the mean pulse on a 10 scale per site and
// for all sites. A week with fewer than min_group respondents comes back null
// from the server and is a gap in the line, never an invented value.
const props = defineProps<{ trend: Overview['trend'] }>()
const { t, d } = useI18n()

const lines = computed(() => trendLines(props.trend))
const anyValue = computed(() => lines.value.some((l) => l.values.some((v) => v !== null)))
const anyHidden = computed(() => lines.value.some((l) => l.hidden > 0))

function seriesName(site: string): string {
  return site === 'all' ? t('overview.trend.all') : t(`common.site.${site}`)
}

function weekLabel(iso: string): string {
  return d(new Date(`${iso}T00:00:00`), 'dayMonth')
}

interface AxisParam {
  dataIndex: number
  seriesIndex: number
}

const option = computed<EChartsCoreOption>(() => {
  const weeks = props.trend.weeks
  const min = props.trend.min_group
  return {
    animation: false,
    grid: { left: 8, right: 16, top: 36, bottom: 8, containLabel: true },
    legend: {
      top: 0,
      left: 0,
      icon: 'roundRect',
      itemWidth: 12,
      itemHeight: 3,
      textStyle: { color: CHART_INK.text, fontSize: 12 },
    },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'line', lineStyle: { color: CHART_INK.axis } },
      formatter: (params: AxisParam[]) => {
        if (params.length === 0) return ''
        const i = params[0].dataIndex
        const head = t('overview.trend.weekOf', { date: weekLabel(weeks[i]) })
        const rows = props.trend.series.map((s) => {
          const p = s.points[i]
          const site = seriesName(s.site)
          if (!p) return t('overview.trend.pointEmpty', { site })
          if (p.hidden) return t('overview.trend.pointHidden', { site, min })
          if (p.avg === null) return t('overview.trend.pointEmpty', { site })
          return t('overview.trend.point', { site, avg: p.avg.toFixed(1) })
        })
        return [head, ...rows].join('<br/>')
      },
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: weeks.map(weekLabel),
      axisLine: { lineStyle: { color: CHART_INK.axis } },
      axisTick: { show: false },
      axisLabel: { color: CHART_INK.muted, fontSize: 11 },
    },
    yAxis: {
      type: 'value',
      min: 1,
      max: 10,
      interval: 3,
      axisLabel: { color: CHART_INK.muted, fontSize: 11 },
      splitLine: { lineStyle: { color: CHART_INK.grid } },
    },
    series: lines.value.map((l) => ({
      type: 'line',
      name: seriesName(l.site),
      data: l.values,
      connectNulls: false,
      symbol: 'circle',
      symbolSize: 8,
      lineStyle: { width: l.site === 'all' ? 3 : 2 },
      itemStyle: { color: SERIES_COLORS[l.site], borderColor: CHART_INK.surface, borderWidth: 2 },
      z: l.site === 'all' ? 3 : 2,
      emphasis: { focus: 'series' },
    })),
  }
})
</script>

<template>
  <SectionCard :title="t('overview.trend.title')">
    <template #desc>{{ t('overview.trend.desc', { min: trend.min_group }) }}</template>
    <p v-if="!anyValue" class="muted">{{ t('overview.trend.empty') }}</p>
    <template v-else>
      <VChart
        class="chart"
        :option="option"
        autoresize
        role="img"
        :aria-label="t('overview.trend.title')"
      />
      <p v-if="anyHidden" class="muted txt-xs note">{{ t('overview.trend.gaps') }}</p>
    </template>
  </SectionCard>
</template>

<style scoped>
.chart {
  width: 100%;
  height: 240px;
}
.note {
  margin: var(--space-1) 0 0;
}
</style>
