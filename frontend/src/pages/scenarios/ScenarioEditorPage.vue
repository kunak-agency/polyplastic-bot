<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { FileSpreadsheet, ListPlus, Plus, Save, Send, Trash2, TriangleAlert } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import ScenarioStatus from '@/components/scenarios/ScenarioStatus.vue'
import StepPanel from '@/components/scenarios/StepPanel.vue'
import PublishDialog from '@/components/scenarios/PublishDialog.vue'
import { Button } from '@/components/ui/button'
import {
  apiDeleteScenario,
  apiGetScenario,
  apiImportScenarioSteps,
  apiPublishScenario,
  apiPutScenarioSteps,
} from '@/api/scenarios'
import { useBreadcrumbTail } from '@/composables/useBreadcrumbs'
import { useToast } from '@/composables/useToast'
import { errorMessage, isApiError } from '@/lib/apiError'
import { formatDayOffset } from '@/lib/calendar'
import { uuidV4 } from '@/lib/ids'
import { createStep, sortSteps, stepIssues } from '@/lib/scenarioSteps'
import type { PublishRequest, ScenarioDetail, ScenarioStep } from '@/types/api'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const toast = useToast()
const { setBreadcrumbTail } = useBreadcrumbTail()

const id = computed(() => String(route.params.id))
const scenario = ref<ScenarioDetail | null>(null)
const error = ref<unknown>(null)
// The draft being edited: a copy, so "dirty" is a plain comparison and a
// failed save leaves the edits on screen.
const steps = ref<ScenarioStep[]>([])
const savedJson = ref('[]')
const selectedId = ref<string | null>(null)
const saving = ref(false)
const publishOpen = ref(false)
const publishing = ref(false)

const dirty = computed(() => JSON.stringify(steps.value) !== savedJson.value)

function adopt(s: ScenarioDetail): void {
  scenario.value = s
  steps.value = sortSteps(structuredClone(s.steps))
  savedJson.value = JSON.stringify(steps.value)
  setBreadcrumbTail(s.name)
}

async function load(): Promise<void> {
  error.value = null
  scenario.value = null
  selectedId.value = null
  try {
    adopt(await apiGetScenario(id.value))
  } catch (e) {
    error.value = e
  }
}
watch(id, load, { immediate: true })

// Set after the draft is deleted: there is nothing left to lose on the way out.
const gone = ref(false)
onBeforeRouteLeave(() => gone.value || !dirty.value || window.confirm(t('common.unsavedLeave')))

// Only a never-published draft nobody runs on can go (DELETE answers 409 otherwise).
const deletable = computed(
  () => !!scenario.value && scenario.value.version === 0 && scenario.value.active_newcomers === 0,
)
const deleting = ref(false)

async function onDeleteDraft(): Promise<void> {
  const s = scenario.value
  if (!s || !window.confirm(t('scenarios.editor.deleteDraftConfirm', { name: s.name }))) return
  deleting.value = true
  try {
    await apiDeleteScenario(s.id)
    gone.value = true
    toast.success(t('scenarios.editor.deleted'))
    void router.push({ name: 'scenarios' })
  } catch (e) {
    toast.error(errorMessage(e, 'scenarios.editor.deleteFailed'))
  } finally {
    deleting.value = false
  }
}

// The empty editor's "Upload Excel": the file's steps replace this draft.
const fileInput = ref<HTMLInputElement | null>(null)
const importing = ref(false)

async function onFile(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !scenario.value) return
  importing.value = true
  try {
    adopt(await apiImportScenarioSteps(scenario.value.id, file))
    toast.success(t('scenarios.editor.imported'))
  } catch (err) {
    toast.error(errorMessage(err, 'scenarios.import.failed'))
  } finally {
    importing.value = false
  }
}

const selected = computed(() => steps.value.find((s) => s.id === selectedId.value) ?? null)
const issues = computed(() => new Map(steps.value.map((s) => [s.id, stepIssues(s)])))
const issueTotal = computed(() => [...issues.value.values()].reduce((n, l) => n + l.length, 0))

function patchSelected(p: Partial<ScenarioStep>): void {
  const s = selected.value
  if (!s) return
  Object.assign(s, p)
  // Day and time decide the row's place; re-sort so the table stays in plan order.
  if ('day_offset' in p || 'time' in p) steps.value = sortSteps(steps.value)
}

function addStep(): void {
  const day = selected.value?.day_offset ?? 0
  // New step ids are made here: the contract sends the whole list with ids and
  // has no "create step" call.
  const step = createStep(uuidV4(), day)
  steps.value = sortSteps([...steps.value, step])
  selectedId.value = step.id
}

function removeSelected(): void {
  const s = selected.value
  if (!s) return
  if (
    !window.confirm(
      t('scenarios.editor.deleteConfirm', { title: s.title || t('scenarios.editor.untitled') }),
    )
  )
    return
  steps.value = steps.value.filter((x) => x.id !== s.id)
  selectedId.value = null
}

async function save(): Promise<boolean> {
  if (!scenario.value) return false
  saving.value = true
  try {
    adopt(await apiPutScenarioSteps(scenario.value.id, steps.value))
    return true
  } catch (e) {
    toast.error(saveError(e))
    return false
  } finally {
    saving.value = false
  }
}

// A 422 names the step as "steps[N].field" (N — index in the list sent): open
// that step and say which one it is.
const STEP_FIELD_RE = /^steps\[(\d+)\]/

function saveError(e: unknown): string {
  const fields = isApiError(e) ? (e.fields ?? []) : []
  for (const f of fields) {
    const m = STEP_FIELD_RE.exec(f.field)
    const step = m ? steps.value[Number(m[1])] : undefined
    if (!step) continue
    selectedId.value = step.id
    return t('scenarios.editor.saveFailedStep', {
      title: step.title || t('scenarios.editor.untitled'),
      message: f.message,
    })
  }
  return errorMessage(e, 'scenarios.editor.saveFailed')
}

async function onSave(): Promise<void> {
  const keep = selectedId.value
  if (await save()) {
    selectedId.value = keep
    toast.success(t('scenarios.editor.saved'))
  }
}

// Unsaved edits are saved first: what gets published is what HR sees.
async function onPublish(body: PublishRequest): Promise<void> {
  if (!scenario.value) return
  publishing.value = true
  try {
    if (dirty.value && !(await save())) return
    const s = await apiPublishScenario(scenario.value.id, body)
    adopt(s)
    publishOpen.value = false
    toast.success(t('scenarios.publish.done', { version: s.version }))
  } catch (e) {
    toast.error(errorMessage(e, 'scenarios.publish.failed'))
  } finally {
    publishing.value = false
  }
}

function sitesLabel(s: ScenarioStep): string {
  return s.sites.length === 0
    ? t('scenarios.allSites')
    : s.sites.map((c) => t(`common.site.${c}`)).join(', ')
}
</script>

<template>
  <PageContainer width="wide">
    <LoadingState v-if="!scenario && !error" variant="card" :rows="2" />
    <ErrorState v-else-if="error" :message="errorMessage(error, 'scenarios.editor.loadFailed')">
      <template #retry>
        <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
      </template>
    </ErrorState>
    <template v-else-if="scenario">
      <PageHead :title="scenario.name">
        <template #subtitle>
          <span class="head-meta">
            <ScenarioStatus :scenario="scenario" />
            <span>{{ t('scenarios.editor.stepsCount', steps.length) }}</span>
            <span v-if="issueTotal > 0" class="head-meta__warn">
              <TriangleAlert :size="12" />{{ t('scenarios.editor.issuesCount', issueTotal) }}
            </span>
            <span v-if="dirty" class="head-meta__dirty">{{ t('scenarios.editor.unsaved') }}</span>
          </span>
        </template>
        <template #actions>
          <Button
            v-if="deletable"
            variant="ghost"
            :disabled="deleting"
            :title="t('scenarios.editor.deleteDraftHint')"
            @click="onDeleteDraft"
          >
            <Trash2 :size="14" />{{ t('scenarios.editor.deleteDraft') }}
          </Button>
          <Button @click="addStep"><Plus :size="14" />{{ t('scenarios.editor.addStep') }}</Button>
          <Button :disabled="!dirty || saving" @click="onSave">
            <Save :size="14" />{{
              saving ? t('common.actions.saving') : t('scenarios.editor.save')
            }}
          </Button>
          <Button variant="primary" :disabled="steps.length === 0" @click="publishOpen = true">
            <Send :size="14" />{{ t('scenarios.editor.publish') }}
          </Button>
        </template>
      </PageHead>

      <div class="views" role="tablist" :aria-label="t('scenarios.editor.views.label')">
        <span class="tabs">
          <span class="tab active" role="tab" aria-selected="true">{{
            t('scenarios.editor.views.table')
          }}</span>
          <span
            class="tab tab--disabled"
            role="tab"
            aria-selected="false"
            aria-disabled="true"
            :title="t('scenarios.editor.views.timelineSoon')"
          >
            {{ t('scenarios.editor.views.timeline') }}
          </span>
        </span>
      </div>

      <div :class="['editor', { 'editor--with-panel': selected }]">
        <SectionCard body-padding="none">
          <EmptyState
            v-if="steps.length === 0"
            :icon="ListPlus"
            :title="t('scenarios.editor.noSteps')"
            :subtitle="t('scenarios.editor.emptyHint')"
          >
            <template #action>
              <div class="empty-actions">
                <Button variant="primary" @click="addStep">
                  <Plus :size="14" />{{ t('scenarios.editor.addStep') }}
                </Button>
                <input
                  ref="fileInput"
                  type="file"
                  class="sr-only"
                  accept=".xlsx,.xls,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-excel"
                  tabindex="-1"
                  aria-hidden="true"
                  @change="onFile"
                />
                <Button
                  :disabled="importing"
                  :title="t('scenarios.import.hint')"
                  @click="fileInput?.click()"
                >
                  <FileSpreadsheet :size="14" />{{
                    importing ? t('scenarios.editor.importing') : t('scenarios.editor.importExcel')
                  }}
                </Button>
              </div>
            </template>
          </EmptyState>
          <div v-else class="tbl-scroll">
            <table class="tbl steps">
              <thead>
                <tr>
                  <th>{{ t('scenarios.editor.columns.day') }}</th>
                  <th>{{ t('scenarios.editor.columns.time') }}</th>
                  <th class="col-stage">{{ t('scenarios.editor.columns.stage') }}</th>
                  <th>{{ t('scenarios.editor.columns.sites') }}</th>
                  <th>{{ t('scenarios.editor.columns.kind') }}</th>
                  <th>{{ t('scenarios.editor.columns.text') }}</th>
                  <th class="td-num">{{ t('scenarios.editor.columns.buttons') }}</th>
                  <th class="col-condition">{{ t('scenarios.editor.columns.condition') }}</th>
                  <th>{{ t('scenarios.editor.columns.status') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="s in steps"
                  :key="s.id"
                  :class="['steps__row', { 'steps__row--on': s.id === selectedId }]"
                  tabindex="0"
                  :aria-selected="s.id === selectedId"
                  @click="selectedId = s.id"
                  @keydown.enter.prevent="selectedId = s.id"
                >
                  <td class="tabular fw-medium">{{ formatDayOffset(s.day_offset) }}</td>
                  <td class="tabular">{{ s.time }}</td>
                  <td class="cell-muted col-stage">{{ t(`common.stage.${s.stage}`) }}</td>
                  <td class="cell-muted">{{ sitesLabel(s) }}</td>
                  <td class="cell-muted">{{ t(`common.stepKind.${s.kind}`) }}</td>
                  <td class="cell-text">
                    <div class="fw-medium">{{ s.title || t('scenarios.editor.untitled') }}</div>
                    <div class="cell-preview">{{ s.text_vy || s.text_ty }}</div>
                  </td>
                  <td class="td-num tabular">{{ s.buttons.length }}</td>
                  <td class="cell-muted col-condition">{{ s.condition ?? '' }}</td>
                  <td>
                    <span v-if="(issues.get(s.id) ?? []).length === 0" class="badge solid-success">
                      {{ t('scenarios.issues.ok') }}
                    </span>
                    <template v-else>
                      <span
                        v-for="issue in issues.get(s.id) ?? []"
                        :key="issue"
                        class="badge solid-warning issue"
                      >
                        {{ t(`scenarios.issues.${issue}`) }}
                      </span>
                    </template>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </SectionCard>

        <StepPanel
          v-if="selected"
          :key="selected.id"
          :step="selected"
          @patch="patchSelected"
          @remove="removeSelected"
          @close="selectedId = null"
        />
      </div>

      <PublishDialog
        v-model:open="publishOpen"
        :next-version="scenario.version + 1"
        :active-newcomers="scenario.active_newcomers"
        :busy="publishing"
        @submit="onPublish"
      />
    </template>
  </PageContainer>
</template>

<style scoped>
.head-meta {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}
.head-meta__warn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--warning);
}
.head-meta__dirty {
  color: var(--fg-muted);
  font-style: italic;
}
.views {
  margin-bottom: 12px;
}
.empty-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
.tab--disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.editor {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.editor--with-panel {
  grid-template-columns: minmax(0, 1fr) 400px;
}
/* With the panel open the table loses its secondary columns: the panel shows
   both fields for the selected step anyway. */
.editor--with-panel .col-stage,
.editor--with-panel .col-condition {
  display: none;
}
.tbl-scroll {
  overflow-x: auto;
}
.steps__row {
  cursor: pointer;
}
.steps__row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.steps__row--on td {
  background: var(--accent-soft);
}
.steps td {
  padding-top: 8px;
  padding-bottom: 8px;
}
.cell-muted {
  color: var(--fg-muted);
  font-size: var(--text-sm);
}
.cell-text {
  max-width: 320px;
}
.cell-preview {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.issue + .issue {
  margin-left: 4px;
}
@media (max-width: 1100px) {
  .editor--with-panel {
    grid-template-columns: 1fr;
  }
}
</style>
