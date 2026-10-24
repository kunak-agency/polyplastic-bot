<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDown, ArrowUp, Flag, GitBranch, Plus, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import BranchEditor from '@/components/scenarios/BranchEditor.vue'
import OutcomesEditor from '@/components/scenarios/OutcomesEditor.vue'
import { newQuestion, questionType, typedQuestion } from '@/lib/scenarioSteps'
import {
  branchHasContent,
  optionKeys,
  optionsAreKeys,
  reorderOptions,
  type Issue,
} from '@/lib/scenarioBranches'
import type {
  Branch,
  FlagSeverity,
  OptionFlag,
  QuestionType,
  StepSurvey,
  SurveyQuestion,
} from '@/types/api'

// Survey settings, all per question: type, scale, threshold, comment, options,
// "own answer", "required"; per option a branch (text and actions before the
// next question) and a risk flag; the rules after the survey (06-excel-tz
// §1.1–1.3, §2). There is no survey-wide scale on screen: a question of the old
// format (no type) shows the type it takes from `survey.scale` and keeps it
// until HR picks a type for that question.
const props = defineProps<{
  survey: StepSurvey
  issues: Issue[]
  mediaNames: string[]
}>()
const emit = defineEmits<{ update: [survey: StepSurvey] }>()
const { t } = useI18n()

const CATEGORIES = ['F0', 'F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9', 'F10']
const SEVERITIES: FlagSeverity[] = ['low', 'medium', 'high']
// The option open below its question: question index and answer key.
const picked = ref<{ q: number; key: string } | null>(null)

function optLabel(key: string, label: string | null): string {
  if (label !== null) return label
  return key === '1' ? t('scenarios.survey.yes') : t('scenarios.survey.no')
}
function pick(q: number, key: string): void {
  picked.value = picked.value?.q === q && picked.value.key === key ? null : { q, key }
}
function setOptionBranch(qi: number, key: string, branch: Branch | null): void {
  const q = props.survey.questions[qi]
  const branches = { ...(q.branches ?? {}) }
  if (branch) branches[key] = branch
  else delete branches[key]
  setQ(qi, { branches: Object.keys(branches).length > 0 ? branches : undefined })
}
function setOptionFlag(qi: number, key: string, flag: OptionFlag | null): void {
  const q = props.survey.questions[qi]
  const flags = { ...(q.option_flags ?? {}) }
  if (flag) flags[key] = flag
  else delete flags[key]
  setQ(qi, { option_flags: Object.keys(flags).length > 0 ? flags : undefined })
}
function flagOf(qi: number, key: string): OptionFlag | undefined {
  return props.survey.questions[qi]?.option_flags?.[key]
}

const TYPES: QuestionType[] = ['scale', 'yesno', 'choice', 'multi', 'text']

function set(p: Partial<StepSurvey>): void {
  emit('update', { ...props.survey, ...p })
}
function setQ(i: number, p: Partial<SurveyQuestion>): void {
  set({ questions: props.survey.questions.map((q, j) => (j === i ? { ...q, ...p } : q)) })
}
function addQ(): void {
  set({ questions: [...props.survey.questions, newQuestion(props.survey)] })
}
function removeQ(i: number): void {
  const q = props.survey.questions[i]
  const rich = Object.values(q.branches ?? {}).some((b) => branchHasContent(b))
  if (rich && !window.confirm(t('scenarios.survey.removeQuestionConfirm', { n: i + 1 }))) return
  picked.value = null
  set({ questions: props.survey.questions.filter((_, j) => j !== i) })
}
// Picking a type gives the question its own settings; the old format is
// never picked back, only kept while HR leaves it alone.
function setType(i: number, raw: string): void {
  if (raw === '') return
  const q = typedQuestion(props.survey.questions[i], raw as QuestionType, props.survey)
  set({ questions: props.survey.questions.map((x, j) => (j === i ? q : x)) })
}
// The old format's threshold: the question's own, else the survey's.
function legacyThreshold(q: SurveyQuestion): number | null {
  return q.threshold ?? props.survey.threshold
}
function legacyScaleLabel(): string {
  const sc = props.survey.scale
  return sc === '1-10' || sc === '1-5' ? t(`scenarios.panel.scales.${sc}`) : ''
}
function num(raw: string): number | null {
  const v = Number(raw)
  return raw.trim() === '' || !Number.isFinite(v) ? null : v
}
// Options are a list of fields: an option may hold a comma. Branches and
// flags are keyed by the option number, so a move or a removal carries them
// along (reorderOptions); the open option follows its option too.
function setOptionText(qi: number, oi: number, text: string): void {
  const q = props.survey.questions[qi]
  setQ(qi, { options: q.options.map((o, j) => (j === oi ? text : o)) })
}
// The formal (vy) label of an option: kept as long as the options list (empty — the informal (ty) label goes).
function setOptionVy(qi: number, oi: number, text: string): void {
  const q = props.survey.questions[qi]
  const vy = q.options.map((_, j) => (j === oi ? text : (q.options_vy?.[j] ?? '')))
  setQ(qi, { options_vy: vy.some((x) => x.trim() !== '') ? vy : undefined })
}
function applyOrder(qi: number, options: string[], order: number[]): void {
  const keyed = optionsAreKeys(props.survey.questions[qi], props.survey.scale)
  if (keyed && picked.value?.q === qi) {
    const at = order.indexOf(Number(picked.value.key) - 1)
    picked.value = at >= 0 ? { q: qi, key: String(at + 1) } : null
  }
  emit('update', reorderOptions(props.survey, qi, options, order))
}
function addOption(qi: number): void {
  const q = props.survey.questions[qi]
  applyOrder(qi, [...q.options, ''], [...q.options.map((_, j) => j), -1])
}
function removeOption(qi: number, oi: number): void {
  const q = props.survey.questions[qi]
  const key = String(oi + 1)
  const keyed = optionsAreKeys(q, props.survey.scale)
  const rich = keyed && (branchHasContent(q.branches?.[key]) || !!q.option_flags?.[key])
  if (rich && !window.confirm(t('scenarios.survey.removeOptionConfirm', { label: q.options[oi] })))
    return
  const order = q.options.map((_, j) => j).filter((j) => j !== oi)
  applyOrder(
    qi,
    order.map((j) => q.options[j]),
    order,
  )
}
function moveOption(qi: number, oi: number, by: -1 | 1): void {
  const q = props.survey.questions[qi]
  const to = oi + by
  if (to < 0 || to >= q.options.length) return
  const order = q.options.map((_, j) => j)
  ;[order[oi], order[to]] = [order[to], order[oi]]
  applyOrder(
    qi,
    order.map((j) => q.options[j]),
    order,
  )
}
function effType(q: SurveyQuestion): QuestionType {
  return questionType(q, props.survey.scale)
}
</script>

<template>
  <fieldset class="survey">
    <legend class="fw-semi">{{ t('scenarios.panel.survey') }}</legend>
    <div class="qs">
      <div v-for="(q, i) in survey.questions" :key="i" class="q">
        <div class="row">
          <span class="q__n tabular">{{ i + 1 }}</span>
          <input
            class="input"
            :aria-label="t('scenarios.panel.question', { n: i + 1 })"
            :placeholder="t('scenarios.panel.questionText')"
            :value="q.text"
            @input="setQ(i, { text: ($event.target as HTMLInputElement).value })"
          />
          <Button
            variant="ghost"
            size="icon-sm"
            :aria-label="t('scenarios.panel.removeQuestion')"
            @click="removeQ(i)"
          >
            <X :size="14" />
          </Button>
        </div>
        <div class="row row--vy">
          <span class="q__n q__n--vy txt-sm muted">{{ t('scenarios.survey.vyShort') }}</span>
          <input
            class="input"
            :aria-label="t('scenarios.survey.questionVy', { n: i + 1 })"
            :placeholder="t('scenarios.survey.questionVyHint')"
            :value="q.text_vy ?? ''"
            @input="setQ(i, { text_vy: ($event.target as HTMLInputElement).value || undefined })"
          />
        </div>
        <div class="q__grid">
          <label :class="['q__f', { 'q__f--wide': !q.type }]">
            <span>{{ t('scenarios.survey.type') }}</span>
            <select
              class="input"
              :aria-label="t('scenarios.survey.typeOf', { n: i + 1 })"
              :value="q.type ?? ''"
              @change="setType(i, ($event.target as HTMLSelectElement).value)"
            >
              <option v-if="!q.type" value="">
                {{
                  t('scenarios.survey.typeLegacy', {
                    type: t(`scenarios.survey.types.${effType(q)}`),
                  })
                }}
              </option>
              <option v-for="ty in TYPES" :key="ty" :value="ty">
                {{ t(`scenarios.survey.types.${ty}`) }}
              </option>
            </select>
          </label>
          <template v-if="q.type === 'scale'">
            <label class="q__f">
              <span>{{ t('scenarios.panel.scale') }}</span>
              <select
                class="input"
                :value="q.scale ?? '1-10'"
                @change="
                  setQ(i, {
                    scale: ($event.target as HTMLSelectElement).value as '1-10' | '1-5',
                  })
                "
              >
                <option value="1-10">1–10</option>
                <option value="1-5">1–5</option>
              </select>
            </label>
            <label class="q__f">
              <span>{{ t('scenarios.survey.threshold') }}</span>
              <input
                class="input tabular"
                type="number"
                min="1"
                :max="q.scale === '1-5' ? 5 : 10"
                :value="q.threshold ?? ''"
                @change="setQ(i, { threshold: num(($event.target as HTMLInputElement).value) })"
              />
            </label>
          </template>
          <label v-else-if="!q.type && effType(q) === 'scale'" class="q__f">
            <span>{{ t('scenarios.survey.threshold') }}</span>
            <input
              class="input tabular"
              type="number"
              min="1"
              :max="survey.scale === '1-5' ? 5 : 10"
              :value="legacyThreshold(q) ?? ''"
              @change="setQ(i, { threshold: num(($event.target as HTMLInputElement).value) })"
            />
          </label>
          <label v-if="q.type && effType(q) !== 'text'" class="q__f">
            <span>{{ t('scenarios.survey.comment') }}</span>
            <select
              class="input"
              :value="q.comment ?? ''"
              @change="
                setQ(i, {
                  comment: ($event.target as HTMLSelectElement).value as SurveyQuestion['comment'],
                })
              "
            >
              <option value="">{{ t('scenarios.survey.commentNone') }}</option>
              <option value="optional">{{ t('scenarios.survey.commentOptional') }}</option>
              <option value="below">{{ t('scenarios.survey.commentBelow') }}</option>
              <option value="on_no">{{ t('scenarios.survey.commentOnNo') }}</option>
            </select>
          </label>
        </div>
        <p v-if="!q.type" class="muted txt-sm legacy">
          {{
            effType(q) === 'scale'
              ? t('scenarios.survey.legacyScale', { scale: legacyScaleLabel() })
              : t('scenarios.survey.legacy')
          }}
        </p>
        <div
          v-if="effType(q) === 'choice' || effType(q) === 'multi' || effType(q) === 'scale'"
          class="optlist"
        >
          <span class="txt-sm muted">{{
            effType(q) === 'scale' ? t('scenarios.survey.reasons') : t('scenarios.panel.options')
          }}</span>
          <div v-for="(o, oi) in q.options" :key="oi" class="optlist__row">
            <span class="q__n tabular">{{ oi + 1 }}</span>
            <input
              class="input"
              :aria-label="t('scenarios.survey.optionN', { n: oi + 1 })"
              :placeholder="t('scenarios.survey.optionN', { n: oi + 1 })"
              :value="o"
              @input="setOptionText(i, oi, ($event.target as HTMLInputElement).value)"
            />
            <input
              class="input input--vy"
              :aria-label="t('scenarios.survey.optionVyN', { n: oi + 1 })"
              :placeholder="t('scenarios.survey.optionVyHint')"
              :value="q.options_vy?.[oi] ?? ''"
              @input="setOptionVy(i, oi, ($event.target as HTMLInputElement).value)"
            />
            <Button
              variant="ghost"
              size="icon-sm"
              :disabled="oi === 0"
              :aria-label="t('scenarios.survey.moveUp', { n: oi + 1 })"
              @click="moveOption(i, oi, -1)"
            >
              <ArrowUp :size="14" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              :disabled="oi === q.options.length - 1"
              :aria-label="t('scenarios.survey.moveDown', { n: oi + 1 })"
              @click="moveOption(i, oi, 1)"
            >
              <ArrowDown :size="14" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              :aria-label="t('scenarios.survey.removeOption', { n: oi + 1 })"
              @click="removeOption(i, oi)"
            >
              <X :size="14" />
            </Button>
          </div>
          <Button size="sm" variant="ghost" class="add" @click="addOption(i)">
            <Plus :size="14" />{{ t('scenarios.survey.addOption') }}
          </Button>
        </div>
        <label v-if="effType(q) === 'choice' || effType(q) === 'multi'" class="check">
          <input
            type="checkbox"
            :checked="!!q.other"
            @change="setQ(i, { other: ($event.target as HTMLInputElement).checked })"
          />
          {{ t('scenarios.survey.other') }}
        </label>
        <p v-if="effType(q) === 'multi'" class="muted txt-sm">
          {{ t('scenarios.survey.multiHint') }}
        </p>
        <label class="check">
          <input
            type="checkbox"
            :checked="!!q.required"
            @change="setQ(i, { required: ($event.target as HTMLInputElement).checked })"
          />
          {{ t('scenarios.survey.required') }}
        </label>
        <div v-if="optionKeys(q, survey.scale).length > 0" class="opts">
          <span class="txt-sm muted">{{ t('scenarios.survey.optionsBranches') }}</span>
          <div class="opts__chips" role="tablist">
            <button
              v-for="o in optionKeys(q, survey.scale)"
              :key="o.key"
              type="button"
              role="tab"
              :aria-selected="picked?.q === i && picked.key === o.key"
              :class="[
                'opt',
                {
                  'opt--on': picked?.q === i && picked.key === o.key,
                  'opt--rich': !!q.branches?.[o.key] || !!q.option_flags?.[o.key],
                },
              ]"
              @click="pick(i, o.key)"
            >
              {{ optLabel(o.key, o.label) }}
              <GitBranch v-if="q.branches?.[o.key]" :size="11" />
              <span v-if="q.option_flags?.[o.key]" class="opt__flag">
                <Flag :size="10" />{{ q.option_flags[o.key].code }}
              </span>
            </button>
          </div>
          <div v-if="picked?.q === i" class="opt-body">
            <div class="opt-body__title txt-sm fw-semi">
              {{
                t('scenarios.survey.optionTab', {
                  label: optLabel(
                    picked.key,
                    optionKeys(q, survey.scale).find((o) => o.key === picked!.key)?.label ?? null,
                  ),
                })
              }}
            </div>
            <div class="opt-flag">
              <label class="q__f">
                <span>{{ t('scenarios.survey.flag') }}</span>
                <select
                  class="input"
                  :value="flagOf(i, picked.key)?.code ?? ''"
                  @change="
                    setOptionFlag(
                      i,
                      picked!.key,
                      ($event.target as HTMLSelectElement).value
                        ? {
                            code: ($event.target as HTMLSelectElement).value,
                            severity: flagOf(i, picked!.key)?.severity ?? 'medium',
                          }
                        : null,
                    )
                  "
                >
                  <option value="">{{ t('scenarios.survey.flagNone') }}</option>
                  <option v-for="c in CATEGORIES" :key="c" :value="c">
                    {{ c }} · {{ t(`tickets.category.${c}`) }}
                  </option>
                </select>
              </label>
              <label v-if="flagOf(i, picked.key)" class="q__f">
                <span>{{ t('scenarios.survey.severity') }}</span>
                <select
                  class="input"
                  :value="flagOf(i, picked.key)?.severity ?? 'medium'"
                  @change="
                    setOptionFlag(i, picked!.key, {
                      code: flagOf(i, picked!.key)!.code,
                      severity: ($event.target as HTMLSelectElement).value as FlagSeverity,
                    })
                  "
                >
                  <option v-for="sv in SEVERITIES" :key="sv" :value="sv">
                    {{ t(`scenarios.survey.severities.${sv}`) }}
                  </option>
                </select>
              </label>
            </div>
            <BranchEditor
              :key="`${i}-${picked.key}`"
              :branch="q.branches?.[picked.key] ?? null"
              :label="picked.key"
              :issues="issues.filter((x) => x.question === i)"
              :goto-options="[]"
              :media-names="mediaNames"
              fixed-then
              :walkable="false"
              @update="setOptionBranch(i, picked!.key, $event)"
            />
          </div>
        </div>
      </div>
      <Button size="sm" variant="ghost" class="add" @click="addQ">
        <Plus :size="14" />{{ t('scenarios.panel.addQuestion') }}
      </Button>
    </div>
    <OutcomesEditor
      :survey="survey"
      :media-names="mediaNames"
      @update="set({ outcomes: $event.length > 0 ? $event : undefined })"
    />
  </fieldset>
</template>

<style scoped>
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
.check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-sm);
  cursor: pointer;
}
.legacy {
  margin: 0;
}
.qs {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.q {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-bottom: 10px;
  border-bottom: 1px dashed var(--border-subtle);
}
.row {
  display: flex;
  gap: 6px;
  align-items: center;
}
.row--vy {
  padding-right: 34px; /* under the question, not under its remove button */
}
.row--vy .input {
  flex: 1;
  min-width: 0;
}
.q__n {
  width: 16px;
  color: var(--fg-subtle);
  font-size: var(--text-sm);
}
.q__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 6px;
}
.q__f {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: var(--text-xs);
  color: var(--fg-muted);
}
.q__f--wide {
  grid-column: span 2;
}
.add {
  align-self: flex-start;
}
.optlist {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.optlist__row {
  display: flex;
  gap: 4px;
  align-items: center;
}
.optlist__row .input {
  flex: 1;
  min-width: 0;
}
.opts {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.opts__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.opt {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  font-size: var(--text-sm);
  background: var(--surface);
  cursor: pointer;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.opt--rich {
  border-color: var(--accent-border);
}
.opt--on {
  background: var(--accent-soft);
  border-color: var(--accent);
}
.opt__flag {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  color: var(--danger);
  font-size: var(--text-xs);
  font-weight: 600;
}
.opt-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px;
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
}
.opt-flag {
  display: grid;
  grid-template-columns: 1fr 110px;
  gap: 6px;
}
</style>
