<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Trash2, X } from 'lucide-vue-next'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import { SITES } from '@/stores/site'
import { formatDayOffset } from '@/lib/calendar'
import { STAGES, STEP_KINDS, defaultSurvey } from '@/lib/scenarioSteps'
import type {
  ScenarioStep,
  SiteCode,
  StepButton,
  StepKind,
  StepSurvey,
  SurveyQuestion,
  SurveyScale,
} from '@/types/api'

// Right-hand step panel (ux-spec 3.5). It never mutates the step it shows:
// every edit leaves as a patch and the editor applies it to its draft.
const props = defineProps<{ step: ScenarioStep }>()
const emit = defineEmits<{
  patch: [patch: Partial<ScenarioStep>]
  remove: []
  close: []
}>()
const { t } = useI18n()

const SCALES: SurveyScale[] = ['1-10', '1-5', 'yesno', 'choice']
const SITE_CODES: readonly SiteCode[] = SITES

const heading = computed(() =>
  t('scenarios.panel.title', {
    day: formatDayOffset(props.step.day_offset),
    time: props.step.time,
  }),
)

function patch(p: Partial<ScenarioStep>): void {
  emit('patch', p)
}

function toggleSite(code: SiteCode, on: boolean): void {
  const sites = on
    ? SITE_CODES.filter((c) => c === code || props.step.sites.includes(c))
    : props.step.sites.filter((c) => c !== code)
  patch({ sites })
}

// A survey needs its settings the moment the type becomes "survey"; leaving
// the type drops them, as the contract has survey: null for other kinds.
function setKind(kind: StepKind): void {
  if (kind === 'survey') {
    patch({ kind, survey: props.step.survey ?? defaultSurvey(props.step) })
  } else {
    patch({ kind, survey: null })
  }
}

function setButton(i: number, p: Partial<StepButton>): void {
  patch({ buttons: props.step.buttons.map((b, j) => (j === i ? { ...b, ...p } : b)) })
}
function addButton(): void {
  const n = props.step.buttons.length + 1
  patch({ buttons: [...props.step.buttons, { label: '', action: `reply_${n}` }] })
}
function removeButton(i: number): void {
  patch({ buttons: props.step.buttons.filter((_, j) => j !== i) })
}

function setSurvey(p: Partial<StepSurvey>): void {
  if (!props.step.survey) return
  patch({ survey: { ...props.step.survey, ...p } })
}
function setThreshold(raw: string): void {
  const v = raw.trim() === '' ? null : Number(raw)
  setSurvey({ threshold: v === null || Number.isFinite(v) ? v : null })
}
function setQuestion(i: number, p: Partial<SurveyQuestion>): void {
  const qs = props.step.survey?.questions ?? []
  setSurvey({ questions: qs.map((q, j) => (j === i ? { ...q, ...p } : q)) })
}
function addQuestion(): void {
  setSurvey({ questions: [...(props.step.survey?.questions ?? []), { text: '', options: [] }] })
}
function removeQuestion(i: number): void {
  setSurvey({ questions: (props.step.survey?.questions ?? []).filter((_, j) => j !== i) })
}
function parseOptions(raw: string): string[] {
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function setDay(raw: string): void {
  const v = Number(raw)
  if (Number.isInteger(v)) patch({ day_offset: v })
}
</script>

<template>
  <aside class="panel" :aria-label="heading">
    <div class="panel__head">
      <div class="fw-semi">{{ heading }}</div>
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

      <FormField :label="t('scenarios.panel.textTy')" html-for="sp-ty">
        <textarea
          id="sp-ty"
          class="textarea panel__text"
          :value="step.text_ty"
          @input="patch({ text_ty: ($event.target as HTMLTextAreaElement).value })"
        />
      </FormField>
      <FormField
        :label="t('scenarios.panel.textVy')"
        html-for="sp-vy"
        :hint="t('scenarios.panel.placeholders')"
      >
        <textarea
          id="sp-vy"
          class="textarea panel__text"
          :value="step.text_vy"
          @input="patch({ text_vy: ($event.target as HTMLTextAreaElement).value })"
        />
      </FormField>

      <FormField :label="t('scenarios.panel.buttons')">
        <div class="list">
          <p v-if="step.buttons.length === 0" class="muted txt-sm">
            {{ t('scenarios.panel.noButtons') }}
          </p>
          <div v-for="(b, i) in step.buttons" :key="i" class="list__row">
            <input
              class="input"
              :aria-label="t('scenarios.panel.buttonLabel')"
              :placeholder="t('scenarios.panel.buttonLabel')"
              :value="b.label"
              @input="setButton(i, { label: ($event.target as HTMLInputElement).value })"
            />
            <input
              class="input mono list__action"
              :aria-label="t('scenarios.panel.buttonAction')"
              :placeholder="t('scenarios.panel.buttonAction')"
              :value="b.action"
              @input="setButton(i, { action: ($event.target as HTMLInputElement).value })"
            />
            <Button
              variant="ghost"
              size="icon-sm"
              :aria-label="t('scenarios.panel.removeButton')"
              @click="removeButton(i)"
            >
              <X :size="14" />
            </Button>
          </div>
          <Button size="sm" variant="ghost" class="list__add" @click="addButton">
            <Plus :size="14" />{{ t('scenarios.panel.addButton') }}
          </Button>
        </div>
      </FormField>

      <div v-if="step.kind === 'survey' && !step.survey" class="survey survey--missing">
        <p class="muted txt-sm">{{ t('scenarios.panel.surveyMissing') }}</p>
        <Button size="sm" @click="patch({ survey: defaultSurvey(step) })">
          <Plus :size="14" />{{ t('scenarios.panel.surveyCreate') }}
        </Button>
      </div>
      <fieldset v-if="step.kind === 'survey' && step.survey" class="survey">
        <legend class="fw-semi">{{ t('scenarios.panel.survey') }}</legend>
        <FormField :label="t('scenarios.panel.scale')">
          <div class="checks" role="radiogroup" :aria-label="t('scenarios.panel.scale')">
            <label v-for="sc in SCALES" :key="sc" class="check">
              <input
                type="radio"
                name="sp-scale"
                :checked="step.survey.scale === sc"
                @change="setSurvey({ scale: sc })"
              />
              {{ t(`scenarios.panel.scales.${sc}`) }}
            </label>
          </div>
        </FormField>
        <FormField
          v-if="step.survey.scale === '1-10' || step.survey.scale === '1-5'"
          :label="t('scenarios.panel.threshold')"
          html-for="sp-threshold"
          :hint="t('scenarios.panel.thresholdHint')"
        >
          <input
            id="sp-threshold"
            class="input tabular panel__narrow"
            type="number"
            min="1"
            :max="step.survey.scale === '1-10' ? 10 : 5"
            :value="step.survey.threshold ?? ''"
            @change="setThreshold(($event.target as HTMLInputElement).value)"
          />
        </FormField>
        <FormField :label="t('scenarios.panel.questions')">
          <div class="list">
            <div v-for="(q, i) in step.survey.questions" :key="i" class="list__q">
              <div class="list__row">
                <input
                  class="input"
                  :aria-label="t('scenarios.panel.question', { n: i + 1 })"
                  :placeholder="t('scenarios.panel.questionText')"
                  :value="q.text"
                  @input="setQuestion(i, { text: ($event.target as HTMLInputElement).value })"
                />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="t('scenarios.panel.removeQuestion')"
                  @click="removeQuestion(i)"
                >
                  <X :size="14" />
                </Button>
              </div>
              <input
                v-if="step.survey.scale === 'choice'"
                class="input"
                :aria-label="t('scenarios.panel.options')"
                :placeholder="t('scenarios.panel.options')"
                :value="q.options.join(', ')"
                @change="
                  setQuestion(i, {
                    options: parseOptions(($event.target as HTMLInputElement).value),
                  })
                "
              />
            </div>
            <Button size="sm" variant="ghost" class="list__add" @click="addQuestion">
              <Plus :size="14" />{{ t('scenarios.panel.addQuestion') }}
            </Button>
          </div>
        </FormField>
      </fieldset>

      <FormField :label="t('scenarios.panel.sending')" :hint="t('scenarios.panel.workHoursHint')">
        <label class="check">
          <input
            type="checkbox"
            :checked="step.work_hours_only"
            @change="patch({ work_hours_only: ($event.target as HTMLInputElement).checked })"
          />
          {{ t('scenarios.panel.workHoursOnly') }}
        </label>
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
  /* Below the sticky topbar (52px) with a gap. */
  max-height: calc(100vh - 84px);
  position: sticky;
  top: 68px;
}
.panel__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-subtle);
}
.panel__body {
  padding: 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.panel__foot {
  padding: 10px 16px;
  border-top: 1px solid var(--border-subtle);
  background: var(--bg-subtle);
}
.panel__text {
  height: 72px;
  padding: 8px 10px;
  line-height: 1.45;
  resize: vertical;
}
.panel__narrow {
  width: 100px;
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
.check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-base);
  cursor: pointer;
}
.list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.list__row {
  display: flex;
  gap: 6px;
  align-items: center;
}
.list__action {
  width: 120px;
  flex: none;
}
.list__q {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.list__add {
  align-self: flex-start;
}
.survey--missing {
  align-items: flex-start;
}
.survey {
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 10px 12px 12px;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.survey legend {
  padding: 0 4px;
  font-size: var(--text-sm);
}
</style>
