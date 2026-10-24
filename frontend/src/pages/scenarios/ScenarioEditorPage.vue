<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  CircleAlert,
  FileDown,
  FileSpreadsheet,
  ListPlus,
  Plus,
  Save,
  Send,
  Trash2,
  TriangleAlert,
} from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ImportReport from '@/components/scenarios/ImportReport.vue'
import { rememberImportReport, useImportReport } from '@/components/scenarios/importReport'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import ScenarioStatus from '@/components/scenarios/ScenarioStatus.vue'
import StepPanel from '@/components/scenarios/StepPanel.vue'
import StepsTable from '@/components/scenarios/StepsTable.vue'
import ScenarioTimeline from '@/components/scenarios/ScenarioTimeline.vue'
import PhonePreview from '@/components/scenarios/PhonePreview.vue'
import PublishDialog from '@/components/scenarios/PublishDialog.vue'
import ScenarioRules from '@/components/scenarios/ScenarioRules.vue'
import RewriteVyButton from '@/components/scenarios/RewriteVyButton.vue'
import { Button } from '@/components/ui/button'
import {
  apiDeleteScenario,
  apiGetScenario,
  apiImportScenarioSteps,
  apiPatchScenario,
  apiPublishPreview,
  apiPublishScenario,
  apiPutScenarioSteps,
  apiScenarioStats,
  apiSendTest,
} from '@/api/scenarios'
import { SITES, useSiteStore } from '@/stores/site'
import { useMediaStore } from '@/stores/media'
import { apiListNewcomers } from '@/api/newcomers'
import { useBreadcrumbTail } from '@/composables/useBreadcrumbs'
import { useToast } from '@/composables/useToast'
import { useScenarioExport } from '@/composables/useScenarioExport'
import { errorMessage, isApiError } from '@/lib/apiError'
import { formatDayOffset } from '@/lib/calendar'
import { uuidV4 } from '@/lib/ids'
import {
  createStep,
  insertAfter,
  moveInBlock,
  moveToStep,
  relocateStep,
  sortSteps,
  stepIssues,
  withPositions,
} from '@/lib/scenarioSteps'
import { countIssues, groupBlocks, validateScenario } from '@/lib/scenarioBranches'
import type {
  PublishPreview,
  PublishRequest,
  ScenarioDetail,
  ScenarioPatch,
  ScenarioStep,
  StepStats,
} from '@/types/api'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const toast = useToast()
const { setBreadcrumbTail } = useBreadcrumbTail()

const id = computed(() => String(route.params.id))
// Report of the last Excel import of this scenario: warnings open their step.
const { report: importReport, clear: clearImportReport } = useImportReport(id)
const scenario = ref<ScenarioDetail | null>(null)
const error = ref<unknown>(null)
// The draft being edited: a copy, so "dirty" is a plain comparison and a
// failed save leaves the edits on screen.
const steps = ref<ScenarioStep[]>([])
const savedJson = ref('[]')
const selectedId = ref<string | null>(null)
const saving = ref(false)
const publishOpen = ref(false)
const { exporting, download: downloadExcel } = useScenarioExport()
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
    const imported = await apiImportScenarioSteps(scenario.value.id, file)
    adopt(imported)
    rememberImportReport(imported.id, imported)
    toast.success(t('scenarios.editor.imported'))
  } catch (err) {
    toast.error(errorMessage(err, 'scenarios.import.failed'))
  } finally {
    importing.value = false
  }
}

const selected = computed(() => steps.value.find((s) => s.id === selectedId.value) ?? null)
// Steps as a table or a timeline (one source, the same step panel), or the
// scenario's commands and rules.
const view = ref<'table' | 'timeline' | 'rules'>('table')
const savingRules = ref(false)
// Only commands and rules are taken from the answer: unsaved step edits stay.
async function onSaveRules(body: ScenarioPatch): Promise<void> {
  if (!scenario.value) return
  savingRules.value = true
  try {
    const res = await apiPatchScenario(scenario.value.id, body)
    scenario.value = {
      ...scenario.value,
      commands: res.commands ?? body.commands,
      rules: res.rules ?? body.rules,
    }
    toast.success(t('scenarios.rules.saved'))
  } catch (e) {
    toast.error(
      isApiError(e) && (e.status === 405 || (e.status === 404 && !e.serverMessage))
        ? t('scenarios.rules.notReady')
        : errorMessage(e, 'scenarios.rules.saveFailed'),
    )
  } finally {
    savingRules.value = false
  }
}

// The open tab of the step panel: null — the message, N — the branch of button N.
const tab = ref<number | null>(null)
watch(selectedId, () => (tab.value = null))

// Verdict §Q3: errors block publishing, warnings do not. The older step checks
// (scenarioSteps.ts) stay warnings.
// The media library knows which names have files; until it loads, a ref
// without media_id counts as "no file".
const mediaStore = useMediaStore()
onMounted(() => void mediaStore.ensureLoaded().catch(() => undefined))
const mediaStatus = computed(() => {
  const m = new Map<string, 'missing' | 'ready'>()
  for (const it of mediaStore.items) {
    const st = it.status === 'ready' ? 'ready' : 'missing'
    m.set(it.name, st)
    for (const a of it.aliases) m.set(a, st)
  }
  return m
})
const issues = computed(() =>
  validateScenario(steps.value, scenario.value?.sites ?? [], SITES, {
    mediaStatus: mediaStatus.value.size > 0 ? mediaStatus.value : undefined,
  }),
)
const legacy = computed(() => new Map(steps.value.map((s) => [s.id, stepIssues(s) as string[]])))
const counts = computed(() => {
  const c = countIssues(issues.value.values())
  for (const l of legacy.value.values()) c.warnings += l.length
  return c
})

const blocks = computed(() => groupBlocks(steps.value))
const selectedBlock = computed(
  () => blocks.value.find((b) => b.steps.some((s) => s.id === selectedId.value))?.steps ?? [],
)
const hasNextInBlock = computed(() => {
  const b = selectedBlock.value
  return b.length > 0 && b[b.length - 1].id !== selectedId.value
})
const gotoOptions = computed(() =>
  blocks.value.map((b) => ({
    value: b.key,
    label: `${formatDayOffset(b.day_offset)} ${b.time} · ${b.steps[0].title || t('scenarios.editor.untitled')}`,
  })),
)
// Names the scenario already uses: the stand-in for the media picker.
const mediaNames = computed(() => {
  const names = new Set<string>()
  for (const s of steps.value) {
    for (const m of s.media ?? []) if (!m.personal) names.add(m.name)
    for (const b of s.buttons) {
      for (const m of b.branch?.messages ?? []) for (const r of m.media) names.add(r.name)
      for (const a of b.branch?.actions ?? []) if (a.media) names.add(a.media)
    }
  }
  return [...names].sort()
})

// ── Statistics (verdict §Q7): badges on buttons, counters on the step ──
const siteStore = useSiteStore()
const stats = ref(new Map<string, StepStats>())
async function loadStats(): Promise<void> {
  if (!scenario.value) return
  try {
    const r = await apiScenarioStats(scenario.value.id, siteStore.apiSite())
    stats.value = new Map(r.items.map((x) => [x.step_id, x]))
  } catch {
    // Statistics are a garnish: the editor works without them.
    stats.value = new Map()
  }
}
watch(
  () => [scenario.value?.id, siteStore.current],
  () => void loadStats(),
)

// ── "Send to myself" / "Walk this path" (bot side, send-test) ──
// The test goes to a connected newcomer (the HR's own test account in the
// demo chat); the choice is remembered per browser.
const RECIPIENT_KEY = 'scenarios.testRecipient'
const recipients = ref<{ id: string; name: string }[]>([])
const recipient = ref<string | null>(null)
watch(recipient, (v) => {
  try {
    if (v) localStorage.setItem(RECIPIENT_KEY, v)
  } catch {
    // storage may be unavailable: the choice just is not remembered
  }
})
onMounted(async () => {
  try {
    const r = await apiListNewcomers()
    recipients.value = r.items
      .filter((n) => n.connection === 'connected')
      .map((n) => ({ id: n.id, name: n.full_name }))
    let saved: string | null = null
    try {
      saved = localStorage.getItem(RECIPIENT_KEY)
    } catch {
      saved = null
    }
    recipient.value =
      recipients.value.find((x) => x.id === saved)?.id ?? recipients.value[0]?.id ?? null
  } catch {
    recipients.value = []
  }
})

const sending = ref(false)
async function onSendTest(button: number | null): Promise<void> {
  const s = selected.value
  if (!scenario.value || !s) return
  if (!recipient.value) {
    toast.error(t('scenarios.test.noRecipient'))
    return
  }
  sending.value = true
  try {
    // The bot sends the saved draft: unsaved edits are saved first.
    if (dirty.value && !(await save())) return
    await apiSendTest(scenario.value.id, s.id, {
      newcomer_id: recipient.value,
      press_button: button,
    })
    toast.success(
      button === null
        ? t('scenarios.test.sent')
        : t('scenarios.test.walked', { label: s.buttons[button]?.label ?? '' }),
    )
  } catch (e) {
    toast.error(
      isApiError(e) &&
        (e.status === 405 || e.status === 501 || (e.status === 404 && !e.serverMessage))
        ? t('scenarios.test.notReady')
        : errorMessage(e, 'scenarios.test.failed'),
    )
  } finally {
    sending.value = false
  }
}

function patchSelected(p: Partial<ScenarioStep>): void {
  const s = selected.value
  if (!s) return
  const when =
    ('day_offset' in p && p.day_offset !== s.day_offset) || ('time' in p && p.time !== s.time)
  if (!when) {
    Object.assign(s, p)
    return
  }
  // Day and time decide the block: the step goes to the end of the block of
  // its new day and time (or a block of its own), and HR hears about it.
  const r = relocateStep(steps.value, s.id, p)
  steps.value = r.steps
  if (!r.moved) return
  const { from, to, place } = r.moved
  const where = (b: { day_offset: number; time: string }) =>
    `${formatDayOffset(b.day_offset)} ${b.time}`
  toast.warning(
    to.size > 1
      ? t('scenarios.order.joined', { block: where(to), place, size: to.size })
      : t('scenarios.order.left', { from: where(from), block: where(to) }),
  )
}

// ── Order inside a block: "up / down", drag and drop, "insert after" ──
function onMove(stepId: string, by: -1 | 1): void {
  steps.value = moveInBlock(steps.value, stepId, by)
}
function onReorder(stepId: string, targetId: string): void {
  steps.value = moveToStep(steps.value, stepId, targetId)
  selectedId.value = stepId
}
function onInsertAfter(stepId: string): void {
  const r = insertAfter(steps.value, stepId, uuidV4())
  if (!r) return
  steps.value = r.steps
  selectedId.value = r.step.id
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

// How much of the v4 model a step list carries: a server that does not keep
// branches yet answers with fewer, and HR must hear it before trusting a save.
function v4Weight(list: ScenarioStep[]): number {
  return list.reduce(
    (n, s) =>
      n +
      s.buttons.filter((b) => !!b.branch).length +
      (s.media?.length ?? 0) +
      (s.hr_task ? 1 : 0) +
      (s.advance ? 1 : 0),
    0,
  )
}

async function save(): Promise<boolean> {
  if (!scenario.value) return false
  saving.value = true
  try {
    const sent = v4Weight(steps.value)
    const res = await apiPutScenarioSteps(scenario.value.id, withPositions(steps.value))
    if (v4Weight(res.steps) < sent) toast.warning(t('scenarios.editor.v4Lost'))
    adopt(res)
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

// The dialog opens on the saved draft: unsaved edits are saved first, then the
// server counts the consequences (publish/preview).
const preview = ref<PublishPreview | null>(null)
const previewError = ref<string | null>(null)
const loadingPreview = ref(false)

async function openPublish(): Promise<void> {
  if (!scenario.value) return
  if (dirty.value && !(await save())) return
  preview.value = null
  previewError.value = null
  publishOpen.value = true
  loadingPreview.value = true
  try {
    preview.value = await apiPublishPreview(scenario.value.id)
  } catch (e) {
    previewError.value = errorMessage(e, 'scenarios.publish.previewFailed')
  } finally {
    loadingPreview.value = false
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
    void loadStats()
    publishOpen.value = false
    toast.success(t('scenarios.publish.done', { version: s.version }))
  } catch (e) {
    toast.error(errorMessage(e, 'scenarios.publish.failed'))
  } finally {
    publishing.value = false
  }
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
            <span v-if="counts.errors > 0" class="head-meta__err">
              <CircleAlert :size="12" />{{ t('scenarios.table.errors', counts.errors) }}
            </span>
            <span v-if="counts.warnings > 0" class="head-meta__warn">
              <TriangleAlert :size="12" />{{ t('scenarios.table.warnings', counts.warnings) }}
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
          <RewriteVyButton
            v-if="steps.length > 0"
            :scenario-id="scenario.id"
            :steps="steps"
            :dirty="dirty"
            @done="adopt"
          />
          <Button @click="addStep"><Plus :size="14" />{{ t('scenarios.editor.addStep') }}</Button>
          <Button :disabled="!dirty || saving" @click="onSave">
            <Save :size="14" />{{
              saving ? t('common.actions.saving') : t('scenarios.editor.save')
            }}
          </Button>
          <Button
            :disabled="steps.length === 0 || exporting"
            :title="t('scenarios.export.hint')"
            @click="downloadExcel(scenario, { dirty })"
          >
            <FileDown :size="14" />{{
              exporting ? t('scenarios.export.downloading') : t('scenarios.export.button')
            }}
          </Button>
          <Button
            variant="primary"
            :disabled="steps.length === 0 || saving"
            :title="counts.errors > 0 ? t('scenarios.publish.blockedHint') : undefined"
            @click="openPublish"
          >
            <Send :size="14" />{{ t('scenarios.editor.publish') }}
          </Button>
        </template>
      </PageHead>

      <div class="views" role="tablist" :aria-label="t('scenarios.editor.views.label')">
        <span class="tabs">
          <button
            type="button"
            :class="['tab', { active: view === 'table' }]"
            role="tab"
            :aria-selected="view === 'table'"
            @click="view = 'table'"
          >
            {{ t('scenarios.editor.views.table') }}
          </button>
          <button
            type="button"
            :class="['tab', { active: view === 'timeline' }]"
            role="tab"
            :aria-selected="view === 'timeline'"
            @click="view = 'timeline'"
          >
            {{ t('scenarios.editor.views.timeline') }}
          </button>
          <button
            type="button"
            :class="['tab', { active: view === 'rules' }]"
            role="tab"
            :aria-selected="view === 'rules'"
            @click="view = 'rules'"
          >
            {{ t('scenarios.editor.views.rules') }}
          </button>
        </span>
      </div>

      <ImportReport
        v-if="importReport"
        :report="importReport"
        :steps="steps"
        :scenario-id="id"
        :dirty="dirty"
        @open="selectedId = $event"
        @close="clearImportReport"
        @rewritten="adopt"
      />

      <ScenarioRules
        v-if="view === 'rules'"
        :commands="scenario.commands ?? []"
        :rules="scenario.rules ?? []"
        :media-names="mediaNames"
        :saving="savingRules"
        @save="onSaveRules"
      />
      <div v-else :class="['editor', { 'editor--with-panel': selected }]">
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
          <ScenarioTimeline
            v-else-if="view === 'timeline'"
            :steps="steps"
            :selected-id="selectedId"
            :issues="issues"
            :legacy="legacy"
            @select="selectedId = $event"
            @move="onMove"
            @reorder="onReorder"
            @insert-after="onInsertAfter"
          />
          <StepsTable
            v-else
            :steps="steps"
            :selected-id="selectedId"
            :issues="issues"
            :legacy="legacy"
            :stats="stats"
            :compact="!!selected"
            @select="selectedId = $event"
            @move="onMove"
            @insert-after="onInsertAfter"
          />
        </SectionCard>

        <div v-if="selected" class="side">
          <StepPanel
            :key="selected.id"
            v-model:tab="tab"
            v-model:recipient="recipient"
            :step="selected"
            :has-next-in-block="hasNextInBlock"
            :issues="issues.get(selected.id) ?? []"
            :stats="stats.get(selected.id) ?? null"
            :goto-options="gotoOptions"
            :media-names="mediaNames"
            :sending="sending"
            :recipients="recipients"
            @patch="patchSelected"
            @remove="removeSelected"
            @insert-after="onInsertAfter(selected.id)"
            @close="selectedId = null"
            @send-test="onSendTest"
          />
          <PhonePreview :block="selectedBlock" :selected-id="selected.id" :tab="tab" />
        </div>
      </div>

      <PublishDialog
        v-model:open="publishOpen"
        :next-version="scenario.version + 1"
        :active-newcomers="scenario.active_newcomers"
        :busy="publishing"
        :preview="preview"
        :preview-error="previewError"
        :loading-preview="loadingPreview"
        :errors="counts.errors"
        :warnings="counts.warnings"
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
.head-meta__err {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--danger);
}
.editor {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.editor--with-panel {
  grid-template-columns: minmax(0, 1fr) minmax(0, 700px);
}
/* Panel and phone side by side; the phone drops under the panel when narrow.
   The whole side sticks under the top bar: a step clicked at the bottom of a
   long table opens in view, and the side scrolls inside itself. */
.side {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 260px;
  gap: 12px;
  align-items: start;
  align-self: start;
  position: sticky;
  top: 68px;
  max-height: calc(100vh - 84px);
  overflow-y: auto;
  overscroll-behavior: contain;
}
@media (max-width: 1500px) {
  .editor--with-panel {
    grid-template-columns: minmax(0, 1fr) 440px;
  }
  .side {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 1100px) {
  .editor--with-panel {
    grid-template-columns: 1fr;
  }
  /* One column: the panel goes under the table, in the page flow. */
  .side {
    position: static;
    max-height: none;
    overflow: visible;
  }
}
</style>
