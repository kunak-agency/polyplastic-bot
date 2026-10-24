<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CircleAlert,
  GitBranch,
  ListPlus,
  MessageSquareText,
  Plus,
  Send,
  Trash2,
  TriangleAlert,
  X,
} from 'lucide-vue-next'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import BranchEditor from '@/components/scenarios/BranchEditor.vue'
import MediaField from '@/components/scenarios/MediaField.vue'
import SurveyEditor from '@/components/scenarios/SurveyEditor.vue'
import AiRewriteBadge from '@/components/scenarios/AiRewriteBadge.vue'
import { SITES } from '@/stores/site'
import { useHealthStore } from '@/stores/health'
import { formatDayOffset } from '@/lib/calendar'
import { STAGES, STEP_KINDS, defaultSurvey, textToFirstQuestion } from '@/lib/scenarioSteps'
import {
  labelIsLong,
  asksSomething,
  branchFor,
  branchHasContent,
  labelLength,
  noAnswerView,
  statsBadge,
  type Issue,
} from '@/lib/scenarioBranches'
import type { Branch, ScenarioStep, SiteCode, StepButton, StepKind, StepStats } from '@/types/api'

// Right-hand step panel (verdict §4): message, buttons, then a tab per button
// with what happens after it. It never mutates the step it shows: every edit
// leaves as a patch and the editor applies it to its draft.
const props = defineProps<{
  step: ScenarioStep
  hasNextInBlock: boolean
  issues: Issue[]
  stats: StepStats | null
  gotoOptions: { value: string; label: string }[]
  mediaNames: string[]
  sending: boolean
}>()
const tab = defineModel<number | null>('tab', { required: true })
const emit = defineEmits<{
  patch: [patch: Partial<ScenarioStep>]
  remove: []
  'insert-after': []
  close: []
  'send-test': [button: number | null]
}>()
const { t } = useI18n()

const SITE_CODES: readonly SiteCode[] = SITES
const RECRUITER_FLAG = 'recruiter_enabled'
const REMIND = { remind_after_h: [24, 48], max: 2 }

const heading = computed(() =>
  t('scenarios.panel.title', {
    day: formatDayOffset(props.step.day_offset),
    time: props.step.time,
  }),
)

const errors = computed(() => props.issues.filter((i) => i.level === 'error'))
const warnings = computed(() => props.issues.filter((i) => i.level === 'warning'))
const asks = computed(() => asksSomething(props.step))
// FEATURE_NUDGES_DEFAULT comes from /health (features.nudges_default).
const health = useHealthStore()
if (!health.last) void health.check()
const noAnswer = computed(() => noAnswerView(props.step, health.nudgesDefault))
function setOwnRule(on: boolean): void {
  patch({ no_answer: on ? { ...REMIND } : null })
}
const showAlt = ref(props.step.alt_text != null)

function patch(p: Partial<ScenarioStep>): void {
  emit('patch', p)
}

function toggleSite(code: SiteCode, on: boolean): void {
  const sites = on
    ? SITE_CODES.filter((c) => c === code || props.step.sites.includes(c))
    : props.step.sites.filter((c) => c !== code)
  patch({ sites })
}

function setKind(kind: StepKind): void {
  // The step's text stays the intro of the survey; question 1 starts empty.
  if (kind === 'survey') patch({ kind, survey: props.step.survey ?? defaultSurvey() })
  else patch({ kind, survey: null })
}
// Offered while question 1 is empty and the step has a text.
const textToQuestion = computed(() =>
  props.step.kind === 'survey' ? textToFirstQuestion(props.step) : null,
)

function setButton(i: number, p: Partial<StepButton>): void {
  patch({ buttons: props.step.buttons.map((b, j) => (j === i ? { ...b, ...p } : b)) })
}
function addButton(): void {
  const n = props.step.buttons.length + 1
  patch({ buttons: [...props.step.buttons, { label: '', action: `reply_${n}`, branch: null }] })
  tab.value = props.step.buttons.length
}
function removeButton(i: number): void {
  const b = props.step.buttons[i]
  if (
    branchHasContent(b.branch) &&
    !window.confirm(t('scenarios.branch.removeButtonConfirm', { label: b.label }))
  )
    return
  // Shared branches point at buttons by index: drop i, shift the ones after it.
  const buttons = props.step.buttons
    .filter((_, j) => j !== i)
    .map((x) => {
      if (!x.branch?.on) return x
      const on = x.branch.on.filter((k) => k !== i).map((k) => (k > i ? k - 1 : k))
      return { ...x, branch: { ...x.branch, on: on.length > 0 ? on : undefined } }
    })
  patch({ buttons })
  if (tab.value !== null && tab.value >= i) tab.value = tab.value === i ? null : tab.value - 1
}
function setBranch(i: number, branch: Branch | null): void {
  setButton(i, { branch })
}

// Tab of a button answered by another button's shared branch ("if 1–2").
const sharedOwner = computed(() => {
  if (tab.value === null) return null
  const f = branchFor(props.step, tab.value)
  return f && f.owner !== tab.value ? f.owner : null
})
// Buttons a branch may also answer: those without a branch of their own and
// not taken by another shared branch.
function shareWith(i: number): { index: number; label: string }[] {
  return props.step.buttons
    .map((b, j) => ({ index: j, label: b.label, b }))
    .filter(({ index, b }) => {
      if (index === i || b.branch) return false
      const f = branchFor(props.step, index)
      return !f || f.owner === i
    })
    .map(({ index, label }) => ({ index, label }))
}
function detach(i: number, owner: number): void {
  const br = props.step.buttons[owner]?.branch
  if (!br) return
  const on = (br.on ?? []).filter((x) => x !== i)
  setBranch(owner, { ...br, on: on.length > 0 ? on : undefined })
}

// The newcomer's own adaptation plan from their card (r36), not the library.
const personalMedia = computed(() => (props.step.media ?? []).filter((m) => !!m.personal))
function setPersonal(on: boolean): void {
  const rest = (props.step.media ?? []).filter((m) => !m.personal)
  patch({
    media: on
      ? [...rest, { name: t('scenarios.panel.personalPlanName'), personal: 'adaptation_plan' }]
      : rest,
  })
}

function setDay(raw: string): void {
  const v = Number(raw)
  if (Number.isInteger(v)) patch({ day_offset: v })
}

function issueText(i: Issue): string {
  const where =
    i.button !== undefined
      ? t('scenarios.issue.where', { label: props.step.buttons[i.button]?.label || '…' })
      : ''
  return where + t(`scenarios.issue.${i.code}`, { detail: i.detail ?? '' })
}
function tabIssue(i: number): 'error' | 'warning' | null {
  const own = props.issues.filter((x) => x.button === i)
  if (own.some((x) => x.level === 'error')) return 'error'
  return own.length > 0 ? 'warning' : null
}
function badge(i: number) {
  return statsBadge(props.stats?.buttons.find((b) => b.label === props.step.buttons[i]?.label))
}
const tabTickets = computed(() => {
  if (tab.value === null) return null
  const label = props.step.buttons[tab.value]?.label
  return props.stats?.buttons.find((b) => b.label === label)?.tickets ?? null
})
function minutes(s: number | null): string {
  if (s === null) return '—'
  return s < 3600
    ? t('scenarios.stats.minutes', { n: Math.max(1, Math.round(s / 60)) })
    : t('scenarios.stats.hours', { n: Math.round(s / 360) / 10 })
}
</script>

<template>
  <aside class="panel" :aria-label="heading">
    <div class="panel__head">
      <div>
        <div class="fw-semi">{{ heading }}</div>
        <div v-if="stats" class="panel__stats tabular">
          {{
            t('scenarios.stats.step', {
              sent: stats.sent,
              answered: stats.answered,
              none: stats.no_answer,
              median: minutes(stats.median_answer_s),
            })
          }}
        </div>
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        :aria-label="t('scenarios.panel.close')"
        @click="emit('close')"
      >
        <X :size="16" />
      </Button>
    </div>

    <div class="panel__body">
      <ul v-if="issues.length > 0" class="issues">
        <li v-for="(i, n) in errors" :key="'e' + n" class="issues__err">
          <CircleAlert :size="13" />{{ issueText(i) }}
        </li>
        <li v-for="(i, n) in warnings" :key="'w' + n" class="issues__warn">
          <TriangleAlert :size="13" />{{ issueText(i) }}
        </li>
      </ul>

      <FormField :label="t('scenarios.panel.name')" html-for="sp-title">
        <input
          id="sp-title"
          class="input"
          :value="step.title"
          @input="patch({ title: ($event.target as HTMLInputElement).value })"
        />
      </FormField>

      <div class="grid3">
        <FormField
          :label="t('scenarios.panel.day')"
          html-for="sp-day"
          :hint="t('scenarios.panel.dayHint')"
        >
          <input
            id="sp-day"
            class="input tabular"
            type="number"
            step="1"
            :value="step.day_offset"
            @change="setDay(($event.target as HTMLInputElement).value)"
          />
        </FormField>
        <FormField :label="t('scenarios.panel.time')" html-for="sp-time">
          <input
            id="sp-time"
            class="input tabular"
            type="time"
            :value="step.time"
            @change="patch({ time: ($event.target as HTMLInputElement).value })"
          />
        </FormField>
        <FormField :label="t('scenarios.panel.stage')" html-for="sp-stage">
          <select
            id="sp-stage"
            class="input"
            :value="step.stage"
            @change="
              patch({ stage: ($event.target as HTMLSelectElement).value as ScenarioStep['stage'] })
            "
          >
            <option v-for="s in STAGES" :key="s" :value="s">{{ t(`common.stage.${s}`) }}</option>
          </select>
        </FormField>
      </div>

      <FormField :label="t('scenarios.panel.sites')" :hint="t('scenarios.panel.sitesHint')">
        <div class="checks">
          <label v-for="code in SITE_CODES" :key="code" class="check">
            <input
              type="checkbox"
              :checked="step.sites.includes(code)"
              @change="toggleSite(code, ($event.target as HTMLInputElement).checked)"
            />
            {{ t(`common.site.${code}`) }}
          </label>
        </div>
      </FormField>

      <FormField :label="t('scenarios.panel.kind')">
        <div class="checks" role="radiogroup" :aria-label="t('scenarios.panel.kind')">
          <label v-for="k in STEP_KINDS" :key="k" class="check">
            <input type="radio" name="sp-kind" :checked="step.kind === k" @change="setKind(k)" />
            {{ t(`common.stepKind.${k}`) }}
          </label>
        </div>
      </FormField>

      <AiRewriteBadge :step="step" />
      <FormField :label="t('scenarios.panel.textVy')" html-for="sp-vy">
        <textarea
          id="sp-vy"
          class="textarea panel__text"
          :value="step.text_vy"
          @input="patch({ text_vy: ($event.target as HTMLTextAreaElement).value })"
        />
      </FormField>
      <FormField
        :label="t('scenarios.panel.textTy')"
        html-for="sp-ty"
        :hint="t('scenarios.panel.placeholders')"
      >
        <textarea
          id="sp-ty"
          class="textarea panel__text"
          :value="step.text_ty"
          @input="patch({ text_ty: ($event.target as HTMLTextAreaElement).value })"
        />
      </FormField>
      <FormField
        v-if="showAlt"
        :label="t('scenarios.panel.altText')"
        html-for="sp-alt"
        :hint="t('scenarios.panel.altTextHint')"
      >
        <textarea
          id="sp-alt"
          class="textarea panel__text panel__text--alt"
          :value="step.alt_text ?? ''"
          @input="patch({ alt_text: ($event.target as HTMLTextAreaElement).value || null })"
        />
      </FormField>
      <Button v-else size="sm" variant="ghost" class="list__add" @click="showAlt = true">
        <Plus :size="14" />{{ t('scenarios.panel.addAltText') }}
      </Button>

      <FormField :label="t('scenarios.panel.media')" :hint="t('scenarios.panel.mediaHint')">
        <MediaField
          :model-value="(step.media ?? []).filter((m) => !m.personal)"
          :names="mediaNames"
          @update:model-value="patch({ media: [...$event, ...personalMedia] })"
        />
        <label class="check check--sm">
          <input
            type="checkbox"
            :checked="personalMedia.length > 0"
            @change="setPersonal(($event.target as HTMLInputElement).checked)"
          />
          {{ t('scenarios.panel.personalPlan') }}
        </label>
      </FormField>

      <section class="buttons">
        <div class="section-title">{{ t('scenarios.panel.buttons') }}</div>
        <p v-if="step.buttons.length === 0" class="muted txt-sm">
          {{ t('scenarios.panel.noButtons') }}
        </p>
        <div v-for="(b, i) in step.buttons" :key="i" class="btnrow">
          <div class="btnrow__main">
            <input
              :class="['input', { err: !b.label.trim(), warn: labelIsLong(b.label) }]"
              :title="labelIsLong(b.label) ? t('scenarios.issue.labelTooLong') : undefined"
              :aria-label="t('scenarios.panel.buttonLabel')"
              :placeholder="t('scenarios.panel.buttonLabel')"
              :value="b.label"
              @input="setButton(i, { label: ($event.target as HTMLInputElement).value })"
            />
            <span :class="['count', { 'count--over': labelIsLong(b.label) }]">
              {{ labelLength(b.label) }}
            </span>
            <Button
              variant="ghost"
              size="icon-sm"
              :aria-label="t('scenarios.panel.removeButton')"
              @click="removeButton(i)"
            >
              <X :size="14" />
            </Button>
          </div>
          <div class="btnrow__meta">
            <label class="check check--sm">
              <input
                type="checkbox"
                :checked="!!b.opens_free_text"
                @change="
                  setButton(i, { opens_free_text: ($event.target as HTMLInputElement).checked })
                "
              />
              {{ t('scenarios.panel.ownAnswer') }}
            </label>
            <span v-if="badge(i)" class="statbadge tabular" :title="t('scenarios.stats.badgeHint')">
              {{ b.label }} · {{ badge(i)!.percent }}% ({{ badge(i)!.picked }})
            </span>
          </div>
        </div>
        <Button
          size="sm"
          variant="ghost"
          class="list__add"
          :disabled="step.buttons.length >= 7"
          @click="addButton"
        >
          <Plus :size="14" />{{ t('scenarios.panel.addButton') }}
        </Button>
      </section>

      <section v-if="step.buttons.length > 0" class="branches">
        <div class="tabs branch-tabs" role="tablist" :aria-label="t('scenarios.branch.tabs')">
          <button
            type="button"
            role="tab"
            :aria-selected="tab === null"
            :class="['tab', { active: tab === null }]"
            @click="tab = null"
          >
            <MessageSquareText :size="12" />{{ t('scenarios.branch.messageTab') }}
          </button>
          <button
            v-for="(b, i) in step.buttons"
            :key="i"
            type="button"
            role="tab"
            :aria-selected="tab === i"
            :class="['tab', { active: tab === i }]"
            @click="tab = i"
          >
            <GitBranch v-if="branchFor(step, i)" :size="12" />
            {{ t('scenarios.branch.tab', { label: b.label || '…' }) }}
            <span v-if="tabIssue(i)" :class="['tabdot', `tabdot--${tabIssue(i)}`]" />
          </button>
        </div>
        <div v-if="tab !== null && step.buttons[tab]" class="branch-body" role="tabpanel">
          <p v-if="tabTickets" class="muted txt-sm">
            {{ t('scenarios.stats.branchTickets', { n: tabTickets }) }}
          </p>
          <div v-if="sharedOwner !== null" class="shared">
            <p class="muted txt-sm">
              {{
                t('scenarios.branch.sharedNote', {
                  label: step.buttons[sharedOwner]?.label || '…',
                })
              }}
            </p>
            <div class="shared__row">
              <Button size="sm" @click="tab = sharedOwner">
                {{ t('scenarios.branch.sharedOpen') }}
              </Button>
              <Button size="sm" variant="ghost" @click="detach(tab!, sharedOwner)">
                {{ t('scenarios.branch.sharedDetach') }}
              </Button>
            </div>
          </div>
          <BranchEditor
            v-else
            :key="tab"
            :branch="step.buttons[tab].branch ?? null"
            :label="step.buttons[tab].label"
            :issues="issues.filter((x) => x.button === tab)"
            :goto-options="gotoOptions"
            :media-names="mediaNames"
            :share-with="shareWith(tab)"
            @update="setBranch(tab!, $event)"
            @walk-path="emit('send-test', tab)"
          />
        </div>
        <p v-else class="muted txt-sm">{{ t('scenarios.branch.pickTab') }}</p>
      </section>

      <FormField v-if="!asks" :label="t('scenarios.advance.label')">
        <div class="radios" role="radiogroup" :aria-label="t('scenarios.advance.label')">
          <label class="check">
            <input
              type="radio"
              name="sp-advance"
              :checked="step.advance !== 'auto'"
              @change="patch({ advance: 'button' })"
            />
            {{ t('scenarios.advance.button') }}
          </label>
          <label class="check">
            <input
              type="radio"
              name="sp-advance"
              :checked="step.advance === 'auto'"
              @change="patch({ advance: 'auto' })"
            />
            {{ t('scenarios.advance.auto') }}
          </label>
        </div>
        <span v-if="!hasNextInBlock" class="field-hint">{{ t('scenarios.advance.last') }}</span>
      </FormField>

      <FormField
        v-if="asks && health.nudgesDefault"
        :label="t('scenarios.noAnswer.label')"
        html-for="sp-noanswer-own"
      >
        <p v-if="noAnswer === 'default'" class="hint-box txt-sm">
          {{ t('scenarios.noAnswer.default') }}
        </p>
        <label class="check check--sm">
          <input
            id="sp-noanswer-own"
            type="checkbox"
            :checked="noAnswer === 'custom'"
            @change="setOwnRule(($event.target as HTMLInputElement).checked)"
          />
          {{ t('scenarios.noAnswer.own') }}
        </label>
        <span v-if="noAnswer === 'custom'" class="field-hint">
          {{ t('scenarios.noAnswer.ownHint') }}
        </span>
      </FormField>
      <FormField v-else-if="asks" :label="t('scenarios.noAnswer.label')" html-for="sp-noanswer">
        <select
          id="sp-noanswer"
          class="input"
          :value="step.no_answer ? 'remind' : 'silent'"
          @change="
            patch({
              no_answer:
                ($event.target as HTMLSelectElement).value === 'remind' ? { ...REMIND } : null,
            })
          "
        >
          <option value="silent">{{ t('scenarios.noAnswer.silent') }}</option>
          <option value="remind">{{ t('scenarios.noAnswer.remind') }}</option>
        </select>
      </FormField>

      <div v-if="step.kind === 'survey' && !step.survey" class="survey--missing">
        <p class="muted txt-sm">{{ t('scenarios.panel.surveyMissing') }}</p>
        <Button size="sm" @click="patch({ survey: defaultSurvey() })">
          <Plus :size="14" />{{ t('scenarios.panel.surveyCreate') }}
        </Button>
      </div>
      <div v-if="step.kind === 'survey' && step.survey" class="hint-box txt-sm intro">
        <span>{{ t('scenarios.survey.introHint') }}</span>
        <Button v-if="textToQuestion" size="sm" @click="patch(textToQuestion)">
          {{ t('scenarios.survey.textToQuestion') }}
        </Button>
      </div>
      <SurveyEditor
        v-if="step.kind === 'survey' && step.survey"
        :survey="step.survey"
        :issues="issues"
        :media-names="mediaNames"
        @update="patch({ survey: $event })"
      />
      <p v-if="step.kind === 'input'" class="hint-box txt-sm">
        {{ t('scenarios.panel.inputHint') }}
      </p>

      <FormField :label="t('scenarios.panel.settings')">
        <div class="flags">
          <label class="check">
            <input
              type="checkbox"
              :checked="!!step.required"
              @change="patch({ required: ($event.target as HTMLInputElement).checked })"
            />
            {{ t('scenarios.panel.required') }}
          </label>
          <label class="check">
            <input
              type="checkbox"
              :checked="!!step.hr_task"
              @change="patch({ hr_task: ($event.target as HTMLInputElement).checked })"
            />
            {{ t('scenarios.panel.hrTask') }}
          </label>
          <label class="check">
            <input
              type="checkbox"
              :checked="step.flag_required === RECRUITER_FLAG"
              @change="
                patch({
                  flag_required: ($event.target as HTMLInputElement).checked
                    ? RECRUITER_FLAG
                    : null,
                })
              "
            />
            {{ t('scenarios.panel.recruiterFlag') }}
          </label>
          <label class="check">
            <input
              type="checkbox"
              :checked="step.work_hours_only"
              @change="patch({ work_hours_only: ($event.target as HTMLInputElement).checked })"
            />
            {{ t('scenarios.panel.workHoursOnly') }}
          </label>
        </div>
      </FormField>

      <FormField :label="t('scenarios.panel.condition')" html-for="sp-cond">
        <input
          id="sp-cond"
          class="input"
          :placeholder="t('scenarios.panel.conditionPlaceholder')"
          :value="step.condition ?? ''"
          @input="patch({ condition: ($event.target as HTMLInputElement).value || null })"
        />
      </FormField>
    </div>

    <div class="panel__foot">
      <Button
        size="sm"
        :disabled="sending || !!step.hr_task"
        :title="step.hr_task ? t('scenarios.test.hrTask') : t('scenarios.test.sendSelfHint')"
        @click="emit('send-test', null)"
      >
        <Send :size="14" />{{
          sending ? t('scenarios.test.sending') : t('scenarios.test.sendSelf')
        }}
      </Button>
      <Button size="sm" :title="t('scenarios.order.insertAfterHint')" @click="emit('insert-after')">
        <ListPlus :size="14" />{{ t('scenarios.order.insertAfter') }}
      </Button>
      <Button variant="danger" size="sm" @click="emit('remove')">
        <Trash2 :size="14" />{{ t('scenarios.editor.deleteStep') }}
      </Button>
    </div>
  </aside>
</template>

<style scoped>
.panel {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  max-height: calc(100vh - 84px);
  min-width: 0;
}
.panel__head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-subtle);
}
.panel__stats {
  font-size: var(--text-sm);
  color: var(--fg-muted);
  margin-top: 2px;
}
.panel__body {
  padding: 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.panel__foot {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: space-between;
  padding: 10px 16px;
  border-top: 1px solid var(--border-subtle);
  background: var(--bg-subtle);
}
.panel__text {
  height: 64px;
  padding: 8px 10px;
  line-height: 1.45;
  resize: vertical;
}
.panel__text--alt {
  background: var(--bg-subtle);
  font-style: italic;
}
.issues {
  list-style: none;
  margin: 0;
  padding: 8px 10px;
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: var(--text-sm);
}
.issues li {
  display: flex;
  gap: 6px;
  align-items: flex-start;
}
.issues li :deep(svg) {
  flex: none;
  margin-top: 2px;
}
.issues__err {
  color: var(--danger);
}
.issues__warn {
  color: var(--warning);
}
.grid3 {
  display: grid;
  grid-template-columns: 1fr 1fr 1.4fr;
  gap: 10px;
}
.checks {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
}
.radios,
.flags {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-base);
  cursor: pointer;
}
.check--sm {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.section-title {
  font-size: var(--text-sm);
  font-weight: 600;
  margin-bottom: 6px;
}
.buttons {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.btnrow {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.btnrow__main {
  display: flex;
  gap: 6px;
  align-items: center;
}
.btnrow__main .input {
  flex: 1;
  min-width: 0;
}
.btnrow__meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 6px;
  padding-left: 2px;
}
.count {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  font-variant-numeric: tabular-nums;
  width: 24px;
  text-align: right;
}
.count--over {
  color: var(--warning);
  font-weight: 600;
}
.statbadge {
  font-size: var(--text-xs);
  padding: 1px 6px;
  border-radius: var(--radius-full);
  background: var(--info-soft);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
}
.err {
  border-color: var(--danger);
}
.list__add {
  align-self: flex-start;
}
.branches {
  display: flex;
  flex-direction: column;
  gap: 10px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 8px 10px 12px;
}
.branch-tabs {
  flex-wrap: wrap;
  height: auto;
  gap: 2px;
}
.branch-tabs .tab {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: var(--text-sm);
}
.tabdot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex: none;
}
.tabdot--error {
  background: var(--danger);
}
.tabdot--warning {
  background: var(--warning);
}
.branch-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.shared {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.shared__row {
  display: flex;
  gap: 6px;
}
.hint-box {
  padding: 8px 10px;
  border-radius: var(--radius-md);
  background: var(--info-soft);
}
.intro {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.survey--missing {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}
</style>
