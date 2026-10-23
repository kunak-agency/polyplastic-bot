<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import BranchEditor from '@/components/scenarios/BranchEditor.vue'
import { branchHasContent, optionKeys } from '@/lib/scenarioBranches'
import type { Branch, OutcomeCondition, StepSurvey, SurveyOutcome } from '@/types/api'

// "After the survey" (r57, 06-excel-tz §2): the thanks always goes; then rules
// on the answers. Rules of one group exclude each other ("OR", the first that
// matches wins), different groups are independent ("+").
const props = defineProps<{ survey: StepSurvey; mediaNames: string[] }>()
const emit = defineEmits<{ update: [outcomes: SurveyOutcome[]] }>()
const { t } = useI18n()

const GROUPS = ['A', 'B', 'C', 'D', 'E']
const outcomes = computed(() => props.survey.outcomes ?? [])

function emitAll(list: SurveyOutcome[]): void {
  emit('update', list)
}
function setRule(i: number, p: Partial<SurveyOutcome>): void {
  emitAll(outcomes.value.map((o, j) => (j === i ? { ...o, ...p } : o)))
}
function addRule(): void {
  const group = GROUPS[Math.min(outcomes.value.length, GROUPS.length - 1)]
  emitAll([
    ...outcomes.value,
    { if: [{ q: 1, in: [] }], match: 'all', group, messages: [], actions: [] },
  ])
}
function removeRule(i: number): void {
  const o = outcomes.value[i]
  const rich = branchHasContent({ messages: o.messages, actions: o.actions, then: 'continue' })
  if (rich && !window.confirm(t('scenarios.outcomes.removeConfirm', { n: i + 1 }))) return
  emitAll(outcomes.value.filter((_, j) => j !== i))
}

function setCond(i: number, ci: number, p: Partial<OutcomeCondition>): void {
  const o = outcomes.value[i]
  setRule(i, { if: o.if.map((c, j) => (j === ci ? { ...c, ...p } : c)) })
}
function addCond(i: number): void {
  const o = outcomes.value[i]
  setRule(i, { if: [...o.if, { q: 1, in: [] }] })
}
function removeCond(i: number, ci: number): void {
  const o = outcomes.value[i]
  setRule(i, { if: o.if.filter((_, j) => j !== ci) })
}

function questionType(q: number) {
  const qq = props.survey.questions[q - 1]
  return qq?.type ?? (props.survey.scale === 'choice' ? 'choice' : props.survey.scale)
}
function keys(q: number) {
  const qq = props.survey.questions[q - 1]
  return qq ? optionKeys(qq, props.survey.scale) : []
}
function values(c: OutcomeCondition): number[] {
  return c.contains ?? c.in ?? []
}
function toggleValue(i: number, ci: number, v: number, on: boolean): void {
  const c = outcomes.value[i].if[ci]
  const cur = new Set(values(c))
  if (on) cur.add(v)
  else cur.delete(v)
  const list = [...cur].sort((a, b) => a - b)
  setCond(i, ci, c.contains ? { contains: list } : { in: list })
}
function setOp(i: number, ci: number, op: 'in' | 'contains'): void {
  const c = outcomes.value[i].if[ci]
  const v = values(c)
  setCond(
    i,
    ci,
    op === 'contains' ? { contains: v, in: undefined } : { in: v, contains: undefined },
  )
}
function setQuestion(i: number, ci: number, q: number): void {
  setCond(i, ci, { q, in: [], contains: undefined })
}
function valueLabel(q: number, key: string, label: string | null): string {
  if (label !== null) return label
  void q
  return key === '1' ? t('scenarios.survey.yes') : t('scenarios.survey.no')
}

function asBranch(o: SurveyOutcome): Branch | null {
  if (o.messages.length === 0 && o.actions.length === 0) return null
  return { messages: o.messages, actions: o.actions, then: 'continue' }
}
function setBranch(i: number, b: Branch | null): void {
  setRule(i, { messages: b?.messages ?? [], actions: b?.actions ?? [] })
}
// "OR" between neighbours of one group, "+" between groups.
function joiner(i: number): 'or' | 'and' | null {
  if (i === 0) return null
  return outcomes.value[i - 1].group === outcomes.value[i].group ? 'or' : 'and'
}
</script>

<template>
  <section class="oc">
    <div class="oc__head">
      <span class="fw-semi txt-sm">{{ t('scenarios.outcomes.title') }}</span>
      <span class="muted txt-sm">{{ t('scenarios.outcomes.hint') }}</span>
    </div>
    <template v-for="(o, i) in outcomes" :key="i">
      <div v-if="joiner(i)" :class="['oc__join', `oc__join--${joiner(i)}`]">
        {{ t(`scenarios.outcomes.join.${joiner(i)}`) }}
      </div>
      <div class="oc__rule">
        <div class="oc__row">
          <span class="txt-sm fw-semi">{{ t('scenarios.outcomes.rule', { n: i + 1 }) }}</span>
          <label class="oc__f">
            {{ t('scenarios.outcomes.group') }}
            <select
              class="input input--sm"
              :value="o.group ?? ''"
              @change="setRule(i, { group: ($event.target as HTMLSelectElement).value })"
            >
              <option v-for="g in GROUPS" :key="g" :value="g">{{ g }}</option>
            </select>
          </label>
          <select
            v-if="o.if.length > 1"
            class="input input--sm"
            :aria-label="t('scenarios.outcomes.match')"
            :value="o.match ?? 'all'"
            @change="
              setRule(i, { match: ($event.target as HTMLSelectElement).value as 'any' | 'all' })
            "
          >
            <option value="all">{{ t('scenarios.outcomes.matchAll') }}</option>
            <option value="any">{{ t('scenarios.outcomes.matchAny') }}</option>
          </select>
          <Button
            variant="ghost"
            size="icon-sm"
            :aria-label="t('scenarios.outcomes.remove')"
            @click="removeRule(i)"
          >
            <X :size="14" />
          </Button>
        </div>
        <div v-for="(c, ci) in o.if" :key="ci" class="oc__cond">
          <div class="oc__row">
            <span class="txt-sm muted">{{ t('scenarios.outcomes.if') }}</span>
            <select
              class="input input--sm"
              :aria-label="t('scenarios.outcomes.question')"
              :value="c.q"
              @change="setQuestion(i, ci, Number(($event.target as HTMLSelectElement).value))"
            >
              <option v-for="(_, qi) in survey.questions" :key="qi" :value="qi + 1">
                {{ t('scenarios.outcomes.q', { n: qi + 1 }) }}
              </option>
            </select>
            <select
              class="input input--sm"
              :aria-label="t('scenarios.outcomes.op')"
              :value="c.contains ? 'contains' : 'in'"
              @change="
                setOp(i, ci, ($event.target as HTMLSelectElement).value as 'in' | 'contains')
              "
            >
              <option value="in">{{ t('scenarios.outcomes.opIn') }}</option>
              <option v-if="questionType(c.q) === 'multi'" value="contains">
                {{ t('scenarios.outcomes.opContains') }}
              </option>
            </select>
            <Button
              v-if="o.if.length > 1"
              variant="ghost"
              size="icon-sm"
              :aria-label="t('scenarios.outcomes.removeCond')"
              @click="removeCond(i, ci)"
            >
              <X :size="12" />
            </Button>
          </div>
          <div class="oc__vals">
            <label v-for="k in keys(c.q)" :key="k.key" class="oc__val">
              <input
                type="checkbox"
                :checked="values(c).includes(Number(k.key))"
                @change="
                  toggleValue(i, ci, Number(k.key), ($event.target as HTMLInputElement).checked)
                "
              />
              {{ valueLabel(c.q, k.key, k.label) }}
            </label>
          </div>
        </div>
        <Button size="sm" variant="ghost" class="oc__add" @click="addCond(i)">
          <Plus :size="12" />{{ t('scenarios.outcomes.addCond') }}
        </Button>
        <BranchEditor
          :branch="asBranch(o)"
          :label="t('scenarios.outcomes.rule', { n: i + 1 })"
          :issues="[]"
          :goto-options="[]"
          :media-names="mediaNames"
          fixed-then
          :walkable="false"
          @update="setBranch(i, $event)"
        />
      </div>
    </template>
    <Button size="sm" variant="ghost" class="oc__add" @click="addRule">
      <Plus :size="14" />{{ t('scenarios.outcomes.add') }}
    </Button>
  </section>
</template>

<style scoped>
.oc {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border-top: 1px solid var(--border-subtle);
  padding-top: 10px;
}
.oc__head {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.oc__rule {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
}
.oc__row {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.oc__f {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.input--sm {
  height: 26px;
  width: auto;
  padding: 0 6px;
  font-size: var(--text-sm);
}
.oc__cond {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.oc__vals {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 10px;
}
.oc__val {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: var(--text-sm);
  cursor: pointer;
}
.oc__add {
  align-self: flex-start;
}
.oc__join {
  align-self: center;
  font-size: var(--text-xs);
  font-weight: 700;
  letter-spacing: 0.04em;
  padding: 1px 8px;
  border-radius: var(--radius-full);
}
.oc__join--or {
  background: var(--warning-soft);
}
.oc__join--and {
  background: var(--info-soft);
}
</style>
