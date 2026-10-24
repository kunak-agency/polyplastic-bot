<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Plus, Save, Terminal, X, Zap } from 'lucide-vue-next'
import FormField from '@/components/common/FormField.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import { Button } from '@/components/ui/button'
import ActionRow from '@/components/scenarios/ActionRow.vue'
import MediaField from '@/components/scenarios/MediaField.vue'
import { defaultAction, labelIsLong, labelLength } from '@/lib/scenarioBranches'
import type {
  ActionType,
  BranchAction,
  ScenarioCommand,
  ScenarioPatch,
  ScenarioRule,
  ScenarioRuleKind,
  StepButton,
} from '@/types/api'

// "Commands and rules" of a scenario (r25, r49, r69, r70): bot commands such as
// /food and /hr, the refusal for a free question the bot cannot answer, and
// the reminder about an unfinished survey. Edited as a draft of its own and
// saved with PATCH /api/scenarios/{id}.
const props = defineProps<{
  commands: ScenarioCommand[]
  rules: ScenarioRule[]
  mediaNames: string[]
  saving: boolean
}>()
const emit = defineEmits<{ save: [body: ScenarioPatch] }>()
const { t } = useI18n()

const COMMAND_ACTIONS: ActionType[] = ['send_file', 'handoff', 'escalate', 'open_free_text']
const BUTTON_ACTIONS: ActionType[] = ['open_free_text', 'handoff', 'escalate']
const REMIND_HOURS = [24, 48]

const cmds = ref<ScenarioCommand[]>([])
const rls = ref<ScenarioRule[]>([])
const savedJson = ref('')
function reset(): void {
  // Props are reactive proxies, which structuredClone refuses: a JSON copy.
  cmds.value = JSON.parse(JSON.stringify(props.commands)) as ScenarioCommand[]
  rls.value = JSON.parse(JSON.stringify(props.rules)) as ScenarioRule[]
  savedJson.value = JSON.stringify([cmds.value, rls.value])
}
watch(() => [props.commands, props.rules], reset, { immediate: true })
const dirty = computed(() => JSON.stringify([cmds.value, rls.value]) !== savedJson.value)
defineExpose({ dirty })
// Edits here are a draft of their own, apart from the steps: switching to the
// table or the timeline used to drop them silently. Leaving the tab saves
// them; leaving the page asks first.
onBeforeUnmount(() => {
  if (dirty.value) save()
})
onBeforeRouteLeave(() => !dirty.value || window.confirm(t('common.unsavedLeave')))

// ── Commands ──
function addCommand(preset?: 'food' | 'hr'): void {
  const c: ScenarioCommand =
    preset === 'food'
      ? {
          name: '/food',
          text_ty: '',
          text_vy: '',
          actions: [{ type: 'send_file', media: '', per_tag: true }],
        }
      : preset === 'hr'
        ? { name: '/hr', text_ty: '', text_vy: '', actions: [{ type: 'handoff', to: 'hr' }] }
        : { name: '/', text_ty: '', text_vy: '', actions: [] }
  cmds.value.push(c)
}
const has = (name: string) => cmds.value.some((c) => c.name.toLowerCase() === name)
function removeCommand(i: number): void {
  if (!window.confirm(t('scenarios.rules.removeCommandConfirm', { name: cmds.value[i].name })))
    return
  cmds.value.splice(i, 1)
}
const picking = ref<string | null>(null)
function addCommandAction(i: number, type: ActionType): void {
  cmds.value[i].actions.push(defaultAction(type))
  picking.value = null
}
function badName(name: string): boolean {
  return !/^\/[a-z0-9_]{1,32}$/i.test(name.trim())
}

// ── Rules ──
function ruleOf(kind: ScenarioRuleKind): ScenarioRule | undefined {
  return rls.value.find((r) => r.kind === kind)
}
function addRule(kind: ScenarioRuleKind): void {
  if (kind === 'free_question')
    rls.value.push({
      key: 'free_question',
      kind,
      trigger: '',
      text_ty: '',
      text_vy: '',
      media: [],
      buttons: [
        {
          label: '',
          action: '',
          branch: { messages: [], actions: [defaultAction('open_free_text')], then: 'continue' },
        },
        {
          label: '',
          action: '',
          branch: {
            messages: [],
            actions: [{ type: 'handoff', to: 'recruiter' }],
            then: 'continue',
          },
        },
      ],
    })
  else
    rls.value.push({
      key: 'survey_reminder',
      kind,
      trigger: '',
      text_ty: '',
      text_vy: '',
      media: [],
      buttons: [],
      remind_after_h: [24, 48],
    })
}
function removeRule(kind: ScenarioRuleKind): void {
  if (!window.confirm(t('scenarios.rules.removeRuleConfirm'))) return
  rls.value = rls.value.filter((r) => r.kind !== kind)
}
function buttonAction(b: StepButton): BranchAction | null {
  return b.branch?.actions[0] ?? null
}
function setButtonAction(b: StepButton, type: ActionType): void {
  b.branch = { messages: [], actions: [defaultAction(type)], then: 'continue' }
}
function toggleHour(r: ScenarioRule, h: number, on: boolean): void {
  const cur = new Set(r.remind_after_h ?? [])
  if (on) cur.add(h)
  else cur.delete(h)
  r.remind_after_h = [...cur].sort((a, b) => a - b)
}

function save(): void {
  emit('save', { commands: cmds.value, rules: rls.value })
}
</script>

<template>
  <div class="rules">
    <div class="rules__bar">
      <p class="muted txt-sm">{{ t('scenarios.rules.intro') }}</p>
      <span v-if="dirty" class="txt-sm dirty-note">{{ t('scenarios.rules.unsaved') }}</span>
      <Button variant="primary" :disabled="!dirty || saving" @click="save">
        <Save :size="14" />{{ saving ? t('common.actions.saving') : t('scenarios.rules.save') }}
      </Button>
    </div>

    <SectionCard>
      <div class="rules__head">
        <Terminal :size="16" />
        <span class="fw-semi">{{ t('scenarios.rules.commands') }}</span>
      </div>
      <p class="muted txt-sm">{{ t('scenarios.rules.commandsHint') }}</p>
      <div v-for="(c, i) in cmds" :key="i" class="card">
        <div class="row">
          <input
            v-model="c.name"
            :class="['input mono name', { err: badName(c.name) }]"
            :aria-label="t('scenarios.rules.commandName')"
          />
          <span v-if="badName(c.name)" class="txt-sm err-text">{{
            t('scenarios.rules.badName')
          }}</span>
          <span class="grow" />
          <Button
            variant="ghost"
            size="icon-sm"
            :aria-label="t('scenarios.rules.removeCommand')"
            @click="removeCommand(i)"
          >
            <X :size="14" />
          </Button>
        </div>
        <div class="grid2">
          <FormField :label="t('scenarios.panel.textVy')">
            <textarea v-model="c.text_vy" class="textarea text" />
          </FormField>
          <FormField :label="t('scenarios.panel.textTy')">
            <textarea v-model="c.text_ty" class="textarea text" />
          </FormField>
        </div>
        <div class="acts">
          <span class="txt-sm fw-semi">{{ t('scenarios.rules.whatItDoes') }}</span>
          <ActionRow
            v-for="(a, ai) in c.actions"
            :key="ai"
            :action="a"
            :media-names="mediaNames"
            :invalid="a.type === 'handoff' && !a.to"
            @update="c.actions[ai] = $event"
            @remove="c.actions.splice(ai, 1)"
          />
          <Button
            size="sm"
            variant="ghost"
            class="add"
            @click="picking = picking === `c${i}` ? null : `c${i}`"
          >
            <Zap :size="14" />{{ t('scenarios.branch.addAction') }}
          </Button>
          <div v-if="picking === `c${i}`" class="picker">
            <button
              v-for="a in COMMAND_ACTIONS"
              :key="a"
              type="button"
              class="pick"
              @click="addCommandAction(i, a)"
            >
              {{ t(`scenarios.action.type.${a}`) }}
            </button>
          </div>
        </div>
      </div>
      <div class="row">
        <Button v-if="!has('/food')" size="sm" @click="addCommand('food')">
          <Plus :size="14" />{{ t('scenarios.rules.addFood') }}
        </Button>
        <Button v-if="!has('/hr')" size="sm" @click="addCommand('hr')">
          <Plus :size="14" />{{ t('scenarios.rules.addHr') }}
        </Button>
        <Button size="sm" variant="ghost" @click="addCommand()">
          <Plus :size="14" />{{ t('scenarios.rules.addCommand') }}
        </Button>
      </div>
    </SectionCard>

    <SectionCard>
      <div class="rules__head">
        <span class="fw-semi">{{ t('scenarios.rules.freeQuestion') }}</span>
      </div>
      <p class="muted txt-sm">{{ t('scenarios.rules.freeQuestionHint') }}</p>
      <template v-if="ruleOf('free_question')">
        <div class="grid2">
          <FormField :label="t('scenarios.rules.refusalVy')">
            <textarea v-model="ruleOf('free_question')!.text_vy" class="textarea text" />
          </FormField>
          <FormField :label="t('scenarios.rules.refusalTy')">
            <textarea v-model="ruleOf('free_question')!.text_ty" class="textarea text" />
          </FormField>
        </div>
        <div class="grid2">
          <div v-for="(b, bi) in ruleOf('free_question')!.buttons" :key="bi" class="card">
            <div class="row">
              <input
                v-model="b.label"
                :class="['input', { err: !b.label.trim(), warn: labelIsLong(b.label) }]"
                :title="labelIsLong(b.label) ? t('scenarios.issue.labelTooLong') : undefined"
                :placeholder="t('scenarios.rules.buttonN', { n: bi + 1 })"
                :aria-label="t('scenarios.rules.buttonN', { n: bi + 1 })"
              />
              <span class="txt-sm muted tabular">{{ labelLength(b.label) }}</span>
            </div>
            <label class="field-col">
              <span class="txt-sm muted">{{ t('scenarios.rules.buttonDoes') }}</span>
              <select
                class="input"
                :value="buttonAction(b)?.type ?? ''"
                @change="
                  setButtonAction(b, ($event.target as HTMLSelectElement).value as ActionType)
                "
              >
                <option v-for="a in BUTTON_ACTIONS" :key="a" :value="a">
                  {{ t(`scenarios.action.type.${a}`) }}
                </option>
              </select>
            </label>
            <ActionRow
              v-if="buttonAction(b) && b.branch"
              :action="buttonAction(b)!"
              :media-names="mediaNames"
              :invalid="buttonAction(b)!.type === 'handoff' && !buttonAction(b)!.to"
              @update="b.branch!.actions = [$event]"
              @remove="b.branch = null"
            />
          </div>
        </div>
        <Button size="sm" variant="ghost" class="add" @click="removeRule('free_question')">
          <X :size="14" />{{ t('scenarios.rules.removeRule') }}
        </Button>
      </template>
      <Button v-else size="sm" @click="addRule('free_question')">
        <Plus :size="14" />{{ t('scenarios.rules.addRule') }}
      </Button>
    </SectionCard>

    <SectionCard>
      <div class="rules__head">
        <span class="fw-semi">{{ t('scenarios.rules.surveyReminder') }}</span>
      </div>
      <p class="muted txt-sm">{{ t('scenarios.rules.surveyReminderHint') }}</p>
      <template v-if="ruleOf('survey_reminder')">
        <FormField :label="t('scenarios.rules.remindAfter')">
          <div class="row">
            <label v-for="h in REMIND_HOURS" :key="h" class="check">
              <input
                type="checkbox"
                :checked="(ruleOf('survey_reminder')!.remind_after_h ?? []).includes(h)"
                @change="
                  toggleHour(
                    ruleOf('survey_reminder')!,
                    h,
                    ($event.target as HTMLInputElement).checked,
                  )
                "
              />
              {{ t('scenarios.rules.hours', { n: h }) }}
            </label>
          </div>
        </FormField>
        <div class="grid2">
          <FormField :label="t('scenarios.panel.textVy')">
            <textarea v-model="ruleOf('survey_reminder')!.text_vy" class="textarea text" />
          </FormField>
          <FormField :label="t('scenarios.panel.textTy')">
            <textarea v-model="ruleOf('survey_reminder')!.text_ty" class="textarea text" />
          </FormField>
        </div>
        <FormField :label="t('scenarios.rules.picture')">
          <MediaField
            :model-value="ruleOf('survey_reminder')!.media"
            :names="mediaNames"
            mode="single"
            @update:model-value="ruleOf('survey_reminder')!.media = $event"
          />
        </FormField>
        <Button size="sm" variant="ghost" class="add" @click="removeRule('survey_reminder')">
          <X :size="14" />{{ t('scenarios.rules.removeRule') }}
        </Button>
      </template>
      <Button v-else size="sm" @click="addRule('survey_reminder')">
        <Plus :size="14" />{{ t('scenarios.rules.addRule') }}
      </Button>
    </SectionCard>
  </div>
</template>

<style scoped>
.dirty-note {
  color: var(--warning);
  margin-left: auto;
}
.rules {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 980px;
}
.rules__bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
.rules__head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  margin: 10px 0;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
}
.row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.grow {
  flex: 1;
}
.name {
  width: 160px;
}
.grid2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.text {
  height: 64px;
  padding: 8px 10px;
  resize: vertical;
}
.acts {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.add {
  align-self: flex-start;
}
.picker {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.pick {
  padding: 3px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  font-size: var(--text-sm);
  background: var(--surface);
  cursor: pointer;
}
.field-col {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.check {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  cursor: pointer;
}
.err {
  border-color: var(--danger);
}
.err-text {
  color: var(--danger);
}
@media (max-width: 900px) {
  .grid2 {
    grid-template-columns: 1fr;
  }
}
</style>
