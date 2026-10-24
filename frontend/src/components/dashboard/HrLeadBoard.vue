<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Bot, Newspaper } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import DashTiles from '@/components/dashboard/DashTiles.vue'
import TodayList from '@/components/dashboard/TodayList.vue'
import { HR_LEAD_TILES, sortLoad } from '@/lib/dashboard'
import type { HrLeadDashboard } from '@/types/api'

// HR lead's first screen (research §2): "Today" (4 row types), 6 tiles, the
// load table sorted by overdue SLA (no ranking, no per-person speed — only the
// team median) and the 3-line "Digest of the week" card.
const props = defineProps<{ data: HrLeadDashboard }>()
const { t } = useI18n()

const load = computed(() => sortLoad(props.data.load))
const median = computed(() => {
  const m = props.data.median_first_response_min
  if (m === null) return null
  return m < 60
    ? t('team.minutes', { n: m })
    : t('team.hours', { h: Math.floor(m / 60), m: m % 60 })
})
</script>

<template>
  <div class="board">
    <TodayList :rows="data.today">
      <template #aside>
        <span class="bot-did txt-sm muted">
          <Bot :size="14" aria-hidden="true" />
          {{
            t('dashboard.botDid', {
              nudges: data.bot_did.nudges,
              summaries: data.bot_did.summaries,
            })
          }}
        </span>
      </template>
    </TodayList>

    <DashTiles :config="HR_LEAD_TILES" :tiles="data.tiles" />

    <div class="board__grid">
      <SectionCard :title="t('dashboard.load.title')" body-padding="none">
        <table class="tbl">
          <thead>
            <tr>
              <th>{{ t('dashboard.load.recruiter') }}</th>
              <th class="td-num">{{ t('dashboard.load.newcomers') }}</th>
              <th class="td-num">{{ t('dashboard.load.open') }}</th>
              <th class="td-num">{{ t('dashboard.load.overdue') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in load" :key="r.user_id">
              <td>
                <RouterLink
                  class="cell-link"
                  :to="{ name: 'newcomers', query: { recruiter: r.user_id } }"
                >
                  {{ r.full_name }}
                </RouterLink>
              </td>
              <td class="td-num tabular">{{ r.newcomers }}</td>
              <td class="td-num tabular">{{ r.tickets_open }}</td>
              <td :class="['td-num tabular', { 'cell-warn': r.tickets_sla_overdue > 0 }]">
                {{ r.tickets_sla_overdue }}
              </td>
            </tr>
          </tbody>
        </table>
        <p class="load-foot muted txt-sm">
          <template v-if="median">{{ t('dashboard.load.median', { value: median }) }}</template>
          <template v-else>{{ t('dashboard.load.medianNone') }}</template>
        </p>
      </SectionCard>

      <SectionCard :title="t('dashboard.digest.title')">
        <template v-if="data.digest">
          <p class="muted txt-sm digest__week">{{ data.digest.week }}</p>
          <ul class="digest">
            <li v-for="(l, i) in data.digest.lines.slice(0, 3)" :key="i">{{ l }}</li>
          </ul>
        </template>
        <p v-else class="muted txt-sm">{{ t('dashboard.digest.none') }}</p>
        <RouterLink class="btn btn-outline btn-sm" :to="{ name: 'analytics' }">
          <Newspaper :size="14" aria-hidden="true" />{{ t('dashboard.digest.open') }}
        </RouterLink>
      </SectionCard>
    </div>
  </div>
</template>

<style scoped>
.board {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.board__grid {
  display: grid;
  grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
  gap: var(--space-4);
  align-items: start;
}
@media (max-width: 1100px) {
  .board__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
.bot-did {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.cell-link {
  color: var(--fg);
  font-weight: 500;
}
.cell-warn {
  color: var(--danger);
  font-weight: 600;
}
.load-foot {
  margin: 0;
  padding: 8px var(--space-4);
  border-top: 1px solid var(--border-subtle);
}
.digest__week {
  margin: 0 0 4px;
}
.digest {
  margin: 0 0 var(--space-3);
  padding-left: 18px;
}
.digest li + li {
  margin-top: 4px;
}
</style>
