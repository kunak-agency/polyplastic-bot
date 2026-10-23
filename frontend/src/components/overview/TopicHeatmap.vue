<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import type { EChartsCoreOption } from 'echarts/core'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Overview } from '@/types/api'
import { HEAT_RAMP, heatColor, heatRows, hiddenCount } from '@/lib/overview'
import { CHART_INK } from './echarts'

// Topic x site heat map (ux-spec 3.7). A cell is the number of distinct
// newcomers who raised the topic; fewer than min_group people behind a cell
// and it is hidden, by the server (count is null), and drawn as "< 5" here.
const props = defineProps<{ heatmap: Overview['heatmap'] }>()
const { t, n } = useI18n()

const rows = computed(() => heatRows(props.heatmap))
const hidden = computed(() => hiddenCount(props.heatmap))
const anySignal = computed(() => Object.values(props.heatmap.respondents).some((v) => (v ?? 0) > 0))

// The third value is a colour step, not the share: HIDDEN, ZERO, or an index
// into HEAT_RAMP. A heat map series needs a visual map, so the colours live in
// its pieces; heatColor (lib/overview.ts) decides the step.
const HIDDEN = -2
const ZERO = -1

interface CellDatum {
  value: [number, number, number]
  label: { formatter: string; color: string }
  tip: string
}

const option = computed<EChartsCoreOption>(() => {
  const h = props.heatmap
  const min = h.min_group
  // Rows top to bottom in F1..F10 order: the y axis grows upwards, so reverse.
  const ys = rows.value.slice().reverse()
  const data: CellDatum[] = []
  ys.forEach((row, y) => {
    row.cells.forEach((c, x) => {
      const site = t(`common.site.${c.site}`)
      const category = `${row.code} · ${row.name}`
      if (c.hidden || c.count === null) {
        data.push({
          value: [x, y, HIDDEN],
          label: { formatter: t('overview.heatmap.hidden', { min }), color: CHART_INK.muted },
          tip: t('overview.heatmap.tooltipHidden', { category, site, min }),
        })
        return
      }
      const share = c.share ?? 0
      const step = HEAT_RAMP.indexOf(heatColor(share)) // -1: zero, neutral
      const dark = step >= 3
      data.push({
        value: [x, y, step < 0 ? ZERO : step],
        label: { formatter: String(c.count), color: dark ? '#ffffff' : CHART_INK.text },
        tip: t('overview.heatmap.tooltip', {
          category,
          site,
          count: c.count,
          total: h.respondents[c.site] ?? 0,
          share: n(share, 'percent'),
        }),
      })
    })
  })
  return {
    animation: false,
    grid: { left: 8, right: 8, top: 28, bottom: 8, containLabel: true },
    tooltip: {
      trigger: 'item',
      formatter: (p: { data: CellDatum }) => p.data.tip,
    },
    xAxis: {
      type: 'category',
      position: 'top',
      data: h.sites.map((s) => t('common.site.' + s)),
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: CHART_INK.text, fontSize: 12, fontWeight: 500 },
      splitArea: { show: false },
    },
    yAxis: {
      type: 'category',
      data: ys.map((r) => `${r.code} · ${r.name}`),
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: CHART_INK.text,
        fontSize: 12,
        width: 280,
        overflow: 'truncate',
      },
    },
    visualMap: {
      type: 'piecewise',
      show: false,
      dimension: 2,
      pieces: [
        { value: HIDDEN, color: CHART_INK.hidden },
        { value: ZERO, color: CHART_INK.zero },
        ...HEAT_RAMP.map((color, i) => ({ value: i, color })),
      ],
    },
    series: [
      {
        type: 'heatmap',
        data,
        label: { show: true, fontSize: 12 },
        itemStyle: { borderColor: CHART_INK.surface, borderWidth: 2, borderRadius: 4 },
        emphasis: { disabled: true },
      },
    ],
  }
})
</script>

<template>
  <SectionCard :title="t('overview.heatmap.title', { days: heatmap.period_days })">
    <template #desc>{{ t('overview.heatmap.desc', { min: heatmap.min_group }) }}</template>
    <p v-if="!anySignal" class="muted">
      {{ t('overview.heatmap.empty', { days: heatmap.period_days }) }}
    </p>
    <template v-else>
      <VChart
        class="chart"
        :style="{ height: `${rows.length * 30 + 44}px` }"
        :option="option"
        autoresize
        role="img"
        :aria-label="t('overview.a11yTable')"
      />
      <div class="legend txt-xs">
        <span class="muted">{{ t('overview.heatmap.legendLess') }}</span>
        <span class="ramp" aria-hidden="true">
          <span v-for="c in HEAT_RAMP" :key="c" :style="{ background: c }" />
        </span>
        <span class="muted">{{ t('overview.heatmap.legendMore') }}</span>
        <span class="swatch" aria-hidden="true" />
        <span class="muted">{{
          t('overview.heatmap.hiddenLabel', { min: heatmap.min_group })
        }}</span>
        <span class="grow" />
        <span v-for="s in heatmap.sites" :key="s" class="muted">
          {{ s }}: {{ t('overview.heatmap.respondents', { n: heatmap.respondents[s] ?? 0 }) }}
        </span>
        <span v-if="hidden > 0" class="muted"
          >· {{ t('overview.heatmap.hiddenCount', { n: hidden }) }}</span
        >
      </div>
      <!-- The same matrix as text, for screen readers and copy-paste. -->
      <table class="sr-only">
        <caption>
          {{
            t('overview.a11yTable')
          }}
        </caption>
        <thead>
          <tr>
            <th />
            <th v-for="s in heatmap.sites" :key="s">{{ s }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.code">
            <th>{{ r.code }} · {{ r.name }}</th>
            <td v-for="c in r.cells" :key="c.site">
              {{ c.hidden ? t('overview.heatmap.hidden', { min: heatmap.min_group }) : c.count }}
            </td>
          </tr>
        </tbody>
      </table>
    </template>
  </SectionCard>
</template>

<style scoped>
.chart {
  width: 100%;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 8px;
  margin-top: var(--space-2);
}
.ramp {
  display: inline-flex;
  border-radius: 3px;
  overflow: hidden;
}
.ramp span {
  width: 14px;
  height: 10px;
}
.swatch {
  width: 14px;
  height: 10px;
  border-radius: 3px;
  background: #e4e4e7;
  margin-left: 8px;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
</style>
