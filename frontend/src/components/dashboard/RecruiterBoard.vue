<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Bot, X } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import RiskMark from '@/components/newcomers/RiskMark.vue'
import DashTiles from '@/components/dashboard/DashTiles.vue'
import TodayList from '@/components/dashboard/TodayList.vue'
import { Users } from 'lucide-vue-next'
import { useRemind } from '@/composables/useRemind'
import { apiAssignTicket } from '@/api/tickets'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import { formatDayOffset, isoToLocalDate } from '@/lib/calendar'
import {
  RECRUITER_TILES,
  chipsFor,
  isQuiet,
  share,
  tileById,
  tileIds,
  toneShown,
} from '@/lib/dashboard'
import type { DashboardItem, Newcomer, RecruiterDashboard, TodayRow } from '@/types/api'

// Recruiter's morning (research §1): "Today" with "the bot did" on the right,
// 6 filter tiles, the compact "mine" list and the week's pulse line. Nothing
// to do → three facts instead of an empty screen.
const props = defineProps<{ data: RecruiterDashboard; mine: Newcomer[] }>()
const emit = defineEmits<{ reload: [] }>()
const { t, d } = useI18n()
const route = useRoute()
const router = useRouter()
const toast = useToast()
const auth = useAuthStore()
const { remind } = useRemind()

const active = computed(() =>
  tileById(RECRUITER_TILES, typeof route.query.tile === 'string' ? route.query.tile : null),
)
function pick(id: string | null): void {
  void router.replace({ query: { ...route.query, tile: id ?? undefined } })
}

const facts = computed(
  () => new Map<string, DashboardItem>(props.data.items.map((i) => [i.newcomer_id, i])),
)
const rows = computed(() => {
  const ids = tileIds(props.data.tiles, active.value)
  return ids ? props.mine.filter((n) => ids.has(n.id)) : props.mine
})
const quiet = computed(() => isQuiet(props.data.today, props.data.tiles))

const busy = ref<string | null>(null)
async function onCall(row: TodayRow, call: 'remind' | 'take'): Promise<void> {
  busy.value = row.id
  try {
    if (call === 'remind' && row.newcomer_id) {
      if (await remind(row.newcomer_id)) emit('reload')
    } else if (call === 'take' && row.ticket_id) {
      await apiAssignTicket(row.ticket_id, {
        assignee_name: auth.user?.full_name ?? '',
        comment: '',
      })
      void router.push({ name: 'ticket', params: { id: row.ticket_id } })
    }
  } catch (e) {
    toast.error(errorMessage(e, 'dashboard.actions.failed'))
  } finally {
    busy.value = null
  }
}

function chipText(key: string, params: Record<string, string | number>): string {
  const p = { ...params }
  if (typeof p.date === 'string') p.date = d(isoToLocalDate(p.date), 'short')
  return t(`dashboard.chips.${key}`, p, typeof p.n === 'number' ? p.n : 1)
}

const pulse = computed(() => {
  const p = props.data.pulse_week
  const answered = share(p.answered, p.sent)
  const answeredText =
    answered.kind === 'few'
      ? t('dashboard.pulse.few')
      : answered.kind === 'count'
        ? t('dashboard.pulse.answeredCount', { part: answered.part, total: answered.total })
        : t('dashboard.pulse.answeredPercent', { value: answered.value })
  const tone =
    toneShown(p) && p.tone
      ? t('dashboard.pulse.tone', {
          pos: p.tone.positive,
          neu: p.tone.neutral,
          neg: p.tone.negative,
          n: p.tone.n,
        })
      : t('dashboard.pulse.toneFew')
  return { answeredText, low: p.low, tone }
})
</script>

<template>
  <div class="board">
    <TodayList :rows="data.today" :busy="busy" @call="onCall">
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
      <template #empty>
        <div v-if="quiet" class="quiet">
          <p class="quiet__title fw-medium">{{ t('dashboard.quiet.title') }}</p>
          <ul class="quiet__facts">
            <li>
              {{
                t('dashboard.quiet.botDid', {
                  nudges: data.bot_did.nudges,
                  summaries: data.bot_did.summaries,
                })
              }}
            </li>
            <li v-if="data.next_event">
              {{
                t('dashboard.quiet.next', {
                  title: data.next_event.title,
                  name: data.next_event.newcomer_name,
                  at: d(new Date(data.next_event.at), 'short'),
                })
              }}
            </li>
            <li v-else>{{ t('dashboard.quiet.noNext') }}</li>
            <li>{{ t('dashboard.quiet.led', { n: data.newcomers_led }, data.newcomers_led) }}</li>
          </ul>
        </div>
        <p v-else class="muted txt-sm">{{ t('dashboard.today.empty') }}</p>
      </template>
    </TodayList>

    <DashTiles
      :config="RECRUITER_TILES"
      :tiles="data.tiles"
      :active="active?.id ?? null"
      @pick="pick"
    />

    <SectionCard :title="t('dashboard.mine.title')" body-padding="none">
      <div v-if="active" class="filter-bar">
        <span class="txt-sm">{{ t('dashboard.mine.filtered', { tile: t(active.labelKey) }) }}</span>
        <button type="button" class="btn btn-ghost btn-sm" @click="pick(null)">
          <X :size="14" aria-hidden="true" />{{ t('dashboard.mine.reset') }}
        </button>
      </div>
      <EmptyState
        v-if="rows.length === 0"
        :icon="Users"
        :title="active ? t('dashboard.mine.emptyFiltered') : t('dashboard.mine.empty')"
      />
      <ul v-else class="mine">
        <li v-for="n in rows" :key="n.id" class="mine__row">
          <RouterLink class="cell-link" :to="{ name: 'newcomer', params: { id: n.id } }">
            {{ n.full_name }}
          </RouterLink>
          <span class="muted txt-sm">
            {{ t(`common.site.${n.site}`) }} · {{ formatDayOffset(n.day) }}
          </span>
          <span class="mine__chips">
            <span
              v-for="c in chipsFor(facts.get(n.id))"
              :key="c.key"
              :class="['dchip', `dchip--${c.tone}`]"
            >
              {{ chipText(c.key, c.params) }}
            </span>
          </span>
          <RiskMark :risk="n.risk" :reason="n.risk_reason" />
        </li>
      </ul>
      <footer class="pulse txt-sm">
        <span class="fw-medium">{{ t('dashboard.pulse.title') }}</span>
        <span>{{ pulse.answeredText }}</span>
        <span>· {{ t('dashboard.pulse.low', { n: pulse.low }) }}</span>
        <span class="muted">· {{ pulse.tone }}</span>
      </footer>
    </SectionCard>
  </div>
</template>

<style scoped>
.board {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.bot-did {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.quiet__title {
  margin: 0 0 4px;
}
.quiet__facts {
  margin: 0;
  padding-left: 18px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.filter-bar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 8px var(--space-4);
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-subtle);
}
.mine {
  list-style: none;
  margin: 0;
  padding: 0;
}
.mine__row {
  display: grid;
  grid-template-columns: minmax(180px, 1.2fr) minmax(120px, 0.8fr) minmax(0, 2fr) auto;
  gap: var(--space-3);
  align-items: center;
  padding: 8px var(--space-4);
  border-top: 1px solid var(--border-subtle);
}
.mine__row:first-child {
  border-top: none;
}
.mine__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.cell-link {
  color: var(--fg);
  font-weight: 500;
}
.cell-link:hover {
  text-decoration: underline;
}
.dchip {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  font-size: var(--text-xs);
  font-weight: 500;
  white-space: nowrap;
  background: var(--bg-muted);
  color: var(--fg-muted);
}
.dchip--danger {
  background: var(--danger-soft);
  color: var(--danger);
}
.dchip--warning {
  background: var(--warning-soft);
  color: #b45309;
}
.dchip--info {
  background: var(--info-soft);
  color: #0e7490;
}
.pulse {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 8px;
  padding: 10px var(--space-4);
  border-top: 1px solid var(--border-subtle);
}
</style>
