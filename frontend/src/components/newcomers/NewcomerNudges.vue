<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiGetNudges } from '@/api/team'
import SectionCard from '@/components/common/SectionCard.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import { errorMessage, isApiError } from '@/lib/apiError'
import { nudgeHistory, nudgeLine } from '@/lib/nudges'
import type { NudgeList } from '@/types/api'

// Reminder history of the newcomer (GET /api/newcomers/{id}/nudges): auto and
// manual nudges and "contacted" marks, newest first, in the newcomer's zone.
// `version` bumps after "Remind" / "Contacted" so the list reloads.
const props = defineProps<{ newcomerId: string; version: number }>()
const { t } = useI18n()

const data = ref<NudgeList | null>(null)
const error = ref<unknown>(null)
// An older backend without the route: the block is not shown at all.
const missing = ref(false)

async function load(): Promise<void> {
  error.value = null
  try {
    data.value = await apiGetNudges(props.newcomerId)
    missing.value = false
  } catch (e) {
    if (isApiError(e) && (e.status === 404 || e.status === 405)) missing.value = true
    else error.value = e
  }
}
watch(() => [props.newcomerId, props.version], load, { immediate: true })

const lines = computed(() =>
  data.value
    ? nudgeHistory(data.value.items).map((i) => ({ id: i.id, ...nudgeLine(i, data.value!.tz) }))
    : [],
)
</script>

<template>
  <SectionCard v-if="!missing" :title="t('newcomers.nudges.title')">
    <LoadingState v-if="!data && !error" :rows="2" />
    <p v-else-if="error" class="muted txt-sm">
      {{ errorMessage(error, 'newcomers.nudges.loadFailed') }}
    </p>
    <p v-else-if="lines.length === 0" class="muted txt-sm nudges__none">
      {{ t('newcomers.nudges.empty') }}
    </p>
    <ul v-else class="nudges">
      <li v-for="l in lines" :key="l.id" class="nudges__row">
        <span aria-hidden="true">{{ l.kind === 'contacted' ? '📞' : '🔔' }}</span>
        <span>
          <template v-if="l.kind === 'contacted'">{{ t('newcomers.nudges.contacted') }}</template>
          <template v-else>
            {{ t('newcomers.nudges.reminder', { count: l.count }) }}
            {{ t(`newcomers.nudges.kind.${l.kind}`) }}
          </template>
        </span>
        <span class="muted tabular">· {{ l.when }}</span>
        <span v-if="l.title" class="muted">· «{{ l.title }}»</span>
        <span v-if="l.state" :class="['nudges__state', `nudges__state--${l.state}`]">
          {{ t(`newcomers.nudges.state.${l.state}`) }}
        </span>
      </li>
    </ul>
  </SectionCard>
</template>

<style scoped>
.nudges {
  list-style: none;
  margin: 0;
  padding: 0;
}
.nudges__none {
  margin: 0;
}
.nudges__row {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 8px;
  align-items: baseline;
  padding: 6px 0;
  font-size: var(--text-sm);
  border-top: 1px solid var(--border-subtle);
}
.nudges__row:first-child {
  border-top: none;
}
.nudges__state {
  font-size: var(--text-xs);
  font-weight: 500;
  padding: 0 6px;
  border-radius: 999px;
}
.nudges__state--planned {
  background: var(--info-soft);
  color: #0e7490;
}
.nudges__state--failed {
  background: var(--danger-soft);
  color: var(--danger);
}
</style>
