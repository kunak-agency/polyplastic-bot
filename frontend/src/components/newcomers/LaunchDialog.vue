<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import PlanPreview from '@/components/newcomers/PlanPreview.vue'
import { Button } from '@/components/ui/button'
import { apiCreateNewcomer } from '@/api/newcomers'
import { apiGetScenario } from '@/api/scenarios'
import { useScenariosStore } from '@/stores/scenarios'
import { SITES, useSiteStore } from '@/stores/site'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { useUsersStore } from '@/stores/users'
import { TZ_OPTIONS, tzKey, tzOffsetLabel } from '@/lib/tz'
import { errorMessage } from '@/lib/apiError'
import { addDays, buildPlanPreview, localIsoDate, weekStart } from '@/lib/calendar'
import type { Newcomer, NewcomerCreate, ScenarioStep, Shift, SiteCode } from '@/types/api'

const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ launched: [newcomer: Newcomer] }>()

const { t } = useI18n()
const toast = useToast()
const scenariosStore = useScenariosStore()
const { items: scenarios } = storeToRefs(scenariosStore)
const siteStore = useSiteStore()
// api_contract_auth.md: hr_lead picks the recruiter from the users, a
// recruiter launches for himself; with auth off the name is typed as before.
const auth = useAuthStore()
const users = useUsersStore()
const { recruiters } = storeToRefs(users)
const recruiterMode = computed<'pick' | 'self' | 'text'>(() =>
  auth.viewer === 'hr_lead' ? 'pick' : auth.viewer === 'recruiter' ? 'self' : 'text',
)

function zoneLabel(id: string): string {
  const key = tzKey(id)
  return t('tz.option', { city: key ? t(`tz.city.${key}`) : id, offset: tzOffsetLabel(id) })
}

type Form = Omit<NewcomerCreate, 'shift'> & { shift: Shift | '' }

function blank(): Form {
  return {
    full_name: '',
    phone: '',
    site: siteStore.apiSite() ?? 'GTP',
    department: '',
    position: '',
    manager_name: '',
    mentor_name: '',
    recruiter_name: recruiterMode.value === 'self' ? (auth.user?.full_name ?? '') : '',
    recruiter_id: recruiterMode.value === 'self' ? (auth.user?.id ?? '') : '',
    tz: '',
    // Monday two weeks ahead: the usual distance between offer and start.
    start_date: addDays(weekStart(localIsoDate()), 14),
    shift: '',
    scenario_id: '',
  }
}

const form = reactive<Form>(blank())
const touched = ref(false)
const submitting = ref(false)

// Mentor is optional: it is often assigned on the first day.
const REQUIRED = [
  'full_name',
  'start_date',
  'site',
  'department',
  'position',
  'manager_name',
  'recruiter_name',
  'scenario_id',
] as const

const errors = computed(() => {
  const out: Partial<Record<keyof Form, string>> = {}
  if (!touched.value) return out
  for (const k of REQUIRED) if (!String(form[k]).trim()) out[k] = t('newcomers.launch.required')
  return out
})

// Only versions that exist can be launched: a never-published draft has none.
const scenarioOptions = computed(() =>
  scenarios.value.filter(
    (s) => s.version > 0 && (s.sites.length === 0 || s.sites.includes(form.site)),
  ),
)

// Keeps the picked scenario valid for the site: the first fitting one otherwise.
function pickScenario(): void {
  if (!scenarioOptions.value.some((s) => s.id === form.scenario_id)) {
    form.scenario_id = scenarioOptions.value[0]?.id ?? ''
  }
}

watch(scenarioOptions, () => {
  if (open.value) pickScenario()
})

watch(
  open,
  (isOpen) => {
    if (!isOpen) return
    Object.assign(form, blank())
    touched.value = false
    pickScenario()
    if (recruiterMode.value === 'pick') users.loadRecruiters().catch(() => undefined)
    // Fresh every time: a version published since the last launch must show.
    void scenariosStore.load()
  },
  { immediate: true },
)

watch(
  () => form.site,
  (s) => {
    if (s !== 'GTP') form.shift = ''
  },
)

// Steps of the picked scenario, for the preview only; the server plans from its
// own copy of the version at launch.
const steps = ref<ScenarioStep[] | null>(null)
const stepsLoading = ref(false)
let stepsTicket = 0
watch(
  () => form.scenario_id,
  async (id) => {
    const mine = ++stepsTicket
    steps.value = null
    if (!id) return
    stepsLoading.value = true
    try {
      const s = await apiGetScenario(id)
      if (mine === stepsTicket) steps.value = s.steps
    } catch {
      // The preview is a hint; the launch itself does not depend on it.
      if (mine === stepsTicket) steps.value = null
    } finally {
      if (mine === stepsTicket) stepsLoading.value = false
    }
  },
)

const selected = computed(() => scenarios.value.find((s) => s.id === form.scenario_id) ?? null)
const preview = computed(() =>
  steps.value && form.start_date ? buildPlanPreview(steps.value, form.start_date, form.site) : null,
)

// The picked user's name goes along as recruiter_name: older servers key on it.
function onPickRecruiter(): void {
  form.recruiter_name = recruiters.value.find((u) => u.id === form.recruiter_id)?.full_name ?? ''
}

async function submit(): Promise<void> {
  touched.value = true
  if (Object.keys(errors.value).length > 0) return
  submitting.value = true
  try {
    const created = await apiCreateNewcomer({
      ...form,
      full_name: form.full_name.trim(),
      shift: form.shift || null,
      recruiter_id: form.recruiter_id || undefined,
      tz: form.tz || null,
    })
    toast.success(t('newcomers.launch.launched'))
    open.value = false
    emit('launched', created)
  } catch (e) {
    toast.error(errorMessage(e, 'newcomers.launch.failed'))
  } finally {
    submitting.value = false
  }
}

const SHIFTS: Shift[] = ['1', '2', '3']
const SITE_CODES: readonly SiteCode[] = SITES
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="t('newcomers.launch.title')"
    :description="t('newcomers.launch.description')"
    width="wide"
  >
    <form id="launch-form" class="launch" novalidate @submit.prevent="submit">
      <div class="launch__form">
        <FormField
          :label="t('newcomers.launch.fields.fullName')"
          html-for="lf-name"
          required
          :error="errors.full_name"
          class="span-2"
        >
          <input
            id="lf-name"
            v-model="form.full_name"
            class="input"
            :class="{ err: errors.full_name }"
            autocomplete="off"
            :placeholder="t('newcomers.launch.placeholders.fullName')"
          />
        </FormField>
        <FormField :label="t('newcomers.launch.fields.phone')" html-for="lf-phone">
          <input
            id="lf-phone"
            v-model="form.phone"
            class="input"
            type="tel"
            autocomplete="off"
            :placeholder="t('newcomers.launch.placeholders.phone')"
          />
        </FormField>
        <FormField
          :label="t('newcomers.launch.fields.startDate')"
          html-for="lf-date"
          required
          :error="errors.start_date"
        >
          <input
            id="lf-date"
            v-model="form.start_date"
            class="input"
            :class="{ err: errors.start_date }"
            type="date"
          />
        </FormField>

        <FormField :label="t('newcomers.launch.fields.site')" required class="span-2">
          <div class="radios" role="radiogroup" :aria-label="t('newcomers.launch.fields.site')">
            <label v-for="s in SITE_CODES" :key="s" class="radio">
              <input v-model="form.site" type="radio" name="lf-site" :value="s" />
              {{ t(`common.site.${s}`) }}
            </label>
          </div>
        </FormField>

        <FormField
          v-if="form.site === 'GTP'"
          :label="t('newcomers.launch.fields.shift')"
          html-for="lf-shift"
        >
          <select id="lf-shift" v-model="form.shift" class="input">
            <option value="">{{ t('newcomers.launch.shift.none') }}</option>
            <option v-for="s in SHIFTS" :key="s" :value="s">
              {{ t(`newcomers.launch.shift.${s}`) }}
            </option>
          </select>
        </FormField>
        <FormField
          :label="t('newcomers.launch.fields.department')"
          html-for="lf-dep"
          required
          :error="errors.department"
          :class="{ 'span-2': form.site !== 'GTP' }"
        >
          <input
            id="lf-dep"
            v-model="form.department"
            class="input"
            :class="{ err: errors.department }"
            :placeholder="t('newcomers.launch.placeholders.department')"
          />
        </FormField>
        <FormField
          :label="t('newcomers.launch.fields.position')"
          html-for="lf-pos"
          required
          :error="errors.position"
          class="span-2"
        >
          <input
            id="lf-pos"
            v-model="form.position"
            class="input"
            :class="{ err: errors.position }"
            :placeholder="t('newcomers.launch.placeholders.position')"
          />
        </FormField>
        <FormField
          :label="t('newcomers.launch.fields.manager')"
          html-for="lf-man"
          required
          :error="errors.manager_name"
        >
          <input
            id="lf-man"
            v-model="form.manager_name"
            class="input"
            :class="{ err: errors.manager_name }"
            :placeholder="t('newcomers.launch.placeholders.manager')"
          />
        </FormField>
        <FormField :label="t('newcomers.launch.fields.mentor')" html-for="lf-men">
          <input
            id="lf-men"
            v-model="form.mentor_name"
            class="input"
            :placeholder="t('newcomers.launch.placeholders.mentor')"
          />
        </FormField>
        <FormField
          :label="t('newcomers.launch.fields.recruiter')"
          html-for="lf-rec"
          required
          :error="errors.recruiter_name"
        >
          <select
            v-if="recruiterMode === 'pick'"
            id="lf-rec"
            v-model="form.recruiter_id"
            class="input"
            :class="{ err: errors.recruiter_name }"
            @change="onPickRecruiter"
          >
            <option value="">{{ t('team.pickRecruiter') }}</option>
            <option v-for="u in recruiters" :key="u.id" :value="u.id">{{ u.full_name }}</option>
          </select>
          <input
            v-else
            id="lf-rec"
            v-model="form.recruiter_name"
            class="input"
            :class="{ err: errors.recruiter_name }"
            :readonly="recruiterMode === 'self'"
            :placeholder="t('newcomers.launch.placeholders.recruiter')"
          />
        </FormField>
        <FormField :label="t('tz.label')" html-for="lf-tz" :hint="t('tz.launchHint')">
          <select id="lf-tz" v-model="form.tz" class="input">
            <option value="">{{ t('tz.bySite') }}</option>
            <option v-for="o in TZ_OPTIONS" :key="o.id" :value="o.id">
              {{ zoneLabel(o.id) }}
            </option>
          </select>
        </FormField>
        <FormField
          :label="t('newcomers.launch.fields.scenario')"
          html-for="lf-scn"
          required
          :error="errors.scenario_id"
          :hint="scenarioOptions.length === 0 ? t('newcomers.launch.noScenario') : undefined"
        >
          <select
            id="lf-scn"
            v-model="form.scenario_id"
            class="input"
            :class="{ err: errors.scenario_id }"
            :disabled="scenarioOptions.length === 0"
          >
            <option v-for="s in scenarioOptions" :key="s.id" :value="s.id">
              {{ t('newcomers.launch.scenarioOption', { name: s.name, version: s.version }) }}
            </option>
          </select>
        </FormField>
      </div>

      <aside class="launch__preview" :aria-label="t('newcomers.preview.title')">
        <div class="launch__preview-head">
          <div class="fw-semi">{{ t('newcomers.preview.title') }}</div>
          <div v-if="selected" class="muted txt-sm">
            {{
              t('newcomers.preview.scenario', { name: selected.name, version: selected.version })
            }}
          </div>
        </div>
        <LoadingState v-if="stepsLoading" :rows="6" />
        <PlanPreview v-else-if="preview" :preview="preview" />
        <p v-else class="muted txt-sm">{{ t('newcomers.preview.pickDate') }}</p>
      </aside>
    </form>

    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="launch-form" variant="primary" :disabled="submitting">
        {{ submitting ? t('newcomers.launch.submitting') : t('newcomers.launch.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.launch {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr);
  gap: 20px;
}
.launch__form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px 12px;
  align-content: start;
}
.launch__form .span-2 {
  grid-column: span 2;
}
.launch__preview {
  border-left: 1px solid var(--border-subtle);
  padding-left: 20px;
  max-height: 560px;
  overflow-y: auto;
}
.launch__preview-head {
  margin-bottom: 12px;
}
.radios {
  display: flex;
  gap: 16px;
  min-height: var(--h-input);
  align-items: center;
}
.radio {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
@media (max-width: 860px) {
  .launch {
    grid-template-columns: 1fr;
  }
  .launch__preview {
    border-left: 0;
    padding-left: 0;
    border-top: 1px solid var(--border-subtle);
    padding-top: 16px;
  }
}
</style>
