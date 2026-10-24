<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { Clock } from 'lucide-vue-next'
import { apiPatchNewcomer } from '@/api/newcomers'
import { useAuthStore } from '@/stores/auth'
import { useUsersStore } from '@/stores/users'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import { TZ_OPTIONS, clockIn, dateIn, effectiveTz, nextStep, tzKey, tzOffsetLabel } from '@/lib/tz'
import type { Newcomer, NewcomerPatch, TimelineEvent } from '@/types/api'

// Recruiter and time zone of the newcomer (api_contract_auth.md): hr_lead
// picks the recruiter, everyone else sees the name; the zone is picked here
// and the nearest step is shown in the newcomer's time and in the viewer's.
const props = defineProps<{ newcomer: Newcomer; timeline: TimelineEvent[] | null }>()
const emit = defineEmits<{ updated: [newcomer: Newcomer, replanned: boolean] }>()
const { t, d } = useI18n()
const toast = useToast()
const auth = useAuthStore()
const users = useUsersStore()
const { recruiters } = storeToRefs(users)

const canPickRecruiter = computed(() => auth.viewer === 'hr_lead')
onMounted(() => {
  if (canPickRecruiter.value) users.loadRecruiters().catch(() => undefined)
})

const busy = ref(false)
async function patch(body: NewcomerPatch, replans: boolean): Promise<void> {
  busy.value = true
  try {
    const n = await apiPatchNewcomer(props.newcomer.id, body)
    toast.success(t('common.toast.saved'))
    emit('updated', n, replans)
  } catch (e) {
    toast.error(errorMessage(e, 'common.toast.updateFailed'))
  } finally {
    busy.value = false
  }
}

// ── Recruiter ───────────────────────────────────────────────────────────────
const recruiterId = ref(props.newcomer.recruiter_id ?? '')
watch(
  () => props.newcomer.recruiter_id,
  (v) => (recruiterId.value = v ?? ''),
)
function onRecruiter(): void {
  void patch({ recruiter_id: recruiterId.value || null }, false)
}

// ── Time zone ───────────────────────────────────────────────────────────────
const tz = ref(props.newcomer.tz ?? '')
watch(
  () => props.newcomer.tz,
  (v) => (tz.value = v ?? ''),
)
function onTz(): void {
  void patch({ tz: tz.value || null }, true)
}

function zoneLabel(id: string): string {
  const key = tzKey(id)
  return t('tz.option', {
    city: key ? t(`tz.city.${key}`) : id,
    offset: tzOffsetLabel(id),
  })
}
const siteOption = computed(() => {
  const siteTz = props.newcomer.site_tz
  return siteTz ? t('tz.bySiteWith', { zone: zoneLabel(siteTz) }) : t('tz.bySite')
})

// ── Nearest step ────────────────────────────────────────────────────────────
const VIEWER_TZ = Intl.DateTimeFormat().resolvedOptions().timeZone
const viewerTz = computed(() => auth.user?.tz || VIEWER_TZ)
const next = computed(() => (props.timeline ? nextStep(props.timeline) : null))
const nextLine = computed(() => {
  const e = next.value
  if (!e) return null
  const zone = effectiveTz(props.newcomer)
  if (!zone) {
    return {
      title: e.title,
      local: t('tz.nextPlain', {
        date: d(new Date(e.fire_at), 'short'),
        time: clockIn(viewerTz.value, e.fire_at),
      }),
      mine: null,
    }
  }
  const at = new Date(e.fire_at)
  const local = t('tz.nextLocal', {
    date: d(new Date(`${dateIn(zone, e.fire_at)}T00:00:00`), 'short'),
    time: clockIn(zone, e.fire_at),
    site: t(`common.site.${props.newcomer.site}`),
    offset: tzOffsetLabel(zone, at),
  })
  const differs = tzOffsetLabel(zone, at) !== tzOffsetLabel(viewerTz.value, at)
  const mine = differs
    ? t('tz.nextMine', {
        time: clockIn(viewerTz.value, e.fire_at),
        offset: tzOffsetLabel(viewerTz.value, at),
      })
    : null
  return { title: e.title, local, mine }
})
</script>

<template>
  <div class="assign">
    <label class="assign__field">
      <span class="field-label">{{ t('newcomers.card.recruiter') }}</span>
      <select
        v-if="canPickRecruiter"
        v-model="recruiterId"
        class="input"
        :disabled="busy"
        @change="onRecruiter"
      >
        <option value="">{{ t('team.noRecruiter') }}</option>
        <option v-for="u in recruiters" :key="u.id" :value="u.id">{{ u.full_name }}</option>
        <!-- A deactivated recruiter is not in the list but may still be assigned. -->
        <option
          v-if="newcomer.recruiter_id && !recruiters.some((u) => u.id === newcomer.recruiter_id)"
          :value="newcomer.recruiter_id"
        >
          {{ newcomer.recruiter_name }}
        </option>
      </select>
      <span v-else class="assign__value">{{
        newcomer.recruiter_name || t('team.noRecruiter')
      }}</span>
    </label>

    <label class="assign__field">
      <span class="field-label">{{ t('tz.label') }}</span>
      <select v-model="tz" class="input" :disabled="busy" @change="onTz">
        <option value="">{{ siteOption }}</option>
        <option v-for="o in TZ_OPTIONS" :key="o.id" :value="o.id">{{ zoneLabel(o.id) }}</option>
      </select>
    </label>

    <div v-if="nextLine" class="assign__next">
      <Clock :size="14" aria-hidden="true" class="assign__icon" />
      <div>
        <div class="txt-sm muted">{{ t('tz.nextTitle', { title: nextLine.title }) }}</div>
        <div class="fw-medium">
          {{ nextLine.local }}
          <span v-if="nextLine.mine" class="muted txt-sm"> · {{ nextLine.mine }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.assign {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px 16px;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border-subtle);
}
.assign__field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 300px;
}
.assign__value {
  display: flex;
  align-items: center;
  min-height: var(--h-input);
  font-weight: 500;
}
.assign__next {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  flex: 1 1 280px;
}
.assign__icon {
  margin-top: 3px;
  color: var(--fg-subtle);
}
</style>
