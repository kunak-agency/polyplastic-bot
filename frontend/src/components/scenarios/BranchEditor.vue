<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquarePlus, Plus, Route, Trash2, X, Zap } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import ActionRow from '@/components/scenarios/ActionRow.vue'
import MediaField from '@/components/scenarios/MediaField.vue'
import {
  BRANCH_MESSAGES_MAX,
  LABEL_MAX,
  ROW_MAX,
  branchHasContent,
  defaultAction,
  emptyBranch,
  labelLength,
  type Issue,
} from '@/lib/scenarioBranches'
import type { ActionType, Branch, BranchAction, BranchMessage, BranchThen } from '@/types/api'

// What happens after one button (verdict §4): up to two messages with terminal
// buttons, actions from a closed list, then "continue the block" or "end it".
// A button without a branch just records the answer.
const props = withDefaults(
  defineProps<{
    branch: Branch | null
    label: string
    issues: Issue[]
    gotoOptions: { value: string; label: string }[]
    mediaNames: string[]
    // Other buttons the branch may also answer ("if 1–2"): index and label.
    shareWith?: { index: number; label: string }[]
    // Option branches and survey outcomes always go on to what follows.
    fixedThen?: boolean
    walkable?: boolean
  }>(),
  { shareWith: () => [], fixedThen: false, walkable: true },
)
const emit = defineEmits<{ update: [branch: Branch | null]; 'walk-path': [] }>()
const { t } = useI18n()

const ACTION_TYPES: ActionType[] = [
  'escalate',
  'handoff',
  'set_field',
  'set_flag',
  'send_file',
  'open_free_text',
]

const branch = computed(() => props.branch)
const addingAction = ref(false)

function update(p: Partial<Branch>): void {
  emit('update', { ...(branch.value ?? emptyBranch()), ...p })
}

function startWithMessage(): void {
  emit('update', emptyBranch())
}
function startWithAction(type: ActionType): void {
  emit('update', { ...emptyBranch(), messages: [], actions: [defaultAction(type)] })
  addingAction.value = false
}

function setMessage(i: number, p: Partial<BranchMessage>): void {
  if (!branch.value) return
  update({ messages: branch.value.messages.map((m, j) => (j === i ? { ...m, ...p } : m)) })
}
function addMessage(): void {
  if (!branch.value || branch.value.messages.length >= BRANCH_MESSAGES_MAX) return
  update({
    messages: [...branch.value.messages, { text_ty: '', text_vy: '', media: [], buttons: [] }],
  })
}
function removeMessage(i: number): void {
  if (!branch.value) return
  update({ messages: branch.value.messages.filter((_, j) => j !== i) })
}

function setTerminal(mi: number, bi: number, label: string): void {
  const m = branch.value?.messages[mi]
  if (!m) return
  setMessage(mi, { buttons: m.buttons.map((b, j) => (j === bi ? { ...b, label } : b)) })
}
function addTerminal(mi: number): void {
  const m = branch.value?.messages[mi]
  if (!m || m.buttons.length >= ROW_MAX) return
  setMessage(mi, { buttons: [...m.buttons, { label: '', action: '' }] })
}
function removeTerminal(mi: number, bi: number): void {
  const m = branch.value?.messages[mi]
  if (!m) return
  setMessage(mi, { buttons: m.buttons.filter((_, j) => j !== bi) })
}

function addAction(type: ActionType): void {
  addingAction.value = false
  if (!branch.value) return startWithAction(type)
  update({ actions: [...branch.value.actions, defaultAction(type)] })
}
function setAction(i: number, a: BranchAction): void {
  if (!branch.value) return
  update({ actions: branch.value.actions.map((x, j) => (j === i ? a : x)) })
}
function removeAction(i: number): void {
  if (!branch.value) return
  update({ actions: branch.value.actions.filter((_, j) => j !== i) })
}

function toggleOn(i: number, on: boolean): void {
  if (!branch.value) return
  const cur = new Set(branch.value.on ?? [])
  if (on) cur.add(i)
  else cur.delete(i)
  update({ on: cur.size > 0 ? [...cur].sort((a, b) => a - b) : undefined })
}

function setThen(then: BranchThen): void {
  update({ then, goto: then === 'goto_block' ? (branch.value?.goto ?? '') : undefined })
}

// Deleting a branch with content asks first (verdict §Q3, after Rasa).
function removeBranch(): void {
  if (
    branchHasContent(branch.value) &&
    !window.confirm(t('scenarios.branch.removeConfirm', { label: props.label }))
  )
    return
  emit('update', null)
}

const handoffInvalid = (a: BranchAction) => a.type === 'handoff' && !a.to
const gotoInvalid = computed(() => props.issues.some((i) => i.code === 'gotoNowhere'))
</script>

<template>
  <div class="br">
    <div v-if="!branch" class="br__none">
      <p class="muted txt-sm">{{ t('scenarios.branch.none') }}</p>
      <div class="br__row">
        <Button size="sm" @click="startWithMessage">
          <MessageSquarePlus :size="14" />{{ t('scenarios.branch.addMessage') }}
        </Button>
        <Button size="sm" @click="addingAction = !addingAction">
          <Zap :size="14" />{{ t('scenarios.branch.addAction') }}
        </Button>
      </div>
      <div v-if="addingAction" class="br__picker">
        <button
          v-for="a in ACTION_TYPES"
          :key="a"
          type="button"
          class="br__pick"
          @click="startWithAction(a)"
        >
          {{ t(`scenarios.action.type.${a}`) }}
        </button>
      </div>
    </div>

    <template v-else>
      <div v-for="(m, mi) in branch.messages" :key="mi" class="br__msg">
        <div class="br__msg-head">
          <span class="txt-sm fw-semi">{{ t('scenarios.branch.message', { n: mi + 1 }) }}</span>
          <Button
            variant="ghost"
            size="icon-sm"
            :aria-label="t('scenarios.branch.removeMessage')"
            @click="removeMessage(mi)"
          >
            <X :size="14" />
          </Button>
        </div>
        <textarea
          class="textarea br__text"
          :aria-label="t('scenarios.panel.textVy')"
          :placeholder="t('scenarios.panel.textVy')"
          :value="m.text_vy"
          @input="setMessage(mi, { text_vy: ($event.target as HTMLTextAreaElement).value })"
        />
        <textarea
          class="textarea br__text"
          :aria-label="t('scenarios.panel.textTy')"
          :placeholder="t('scenarios.panel.textTy')"
          :value="m.text_ty"
          @input="setMessage(mi, { text_ty: ($event.target as HTMLTextAreaElement).value })"
        />
        <MediaField
          :model-value="m.media"
          :names="mediaNames"
          @update:model-value="setMessage(mi, { media: $event })"
        />
        <div class="br__terminal">
          <span class="txt-sm muted">{{ t('scenarios.branch.terminal') }}</span>
          <div v-for="(b, bi) in m.buttons" :key="bi" class="br__row">
            <input
              :class="['input', { err: !b.label.trim() || labelLength(b.label) > LABEL_MAX }]"
              :value="b.label"
              :placeholder="t('scenarios.panel.buttonLabel')"
              :aria-label="t('scenarios.panel.buttonLabel')"
              @input="setTerminal(mi, bi, ($event.target as HTMLInputElement).value)"
            />
            <span :class="['br__count', { 'br__count--over': labelLength(b.label) > LABEL_MAX }]"
              >{{ labelLength(b.label) }}/{{ LABEL_MAX }}</span
            >
            <Button
              variant="ghost"
              size="icon-sm"
              :aria-label="t('scenarios.panel.removeButton')"
              @click="removeTerminal(mi, bi)"
            >
              <X :size="14" />
            </Button>
          </div>
          <Button
            size="sm"
            variant="ghost"
            class="br__add"
            :disabled="m.buttons.length >= ROW_MAX"
            @click="addTerminal(mi)"
          >
            <Plus :size="14" />{{ t('scenarios.branch.addTerminal') }}
          </Button>
        </div>
      </div>
      <Button
        v-if="branch.messages.length < BRANCH_MESSAGES_MAX"
        size="sm"
        variant="ghost"
        class="br__add"
        @click="addMessage"
      >
        <MessageSquarePlus :size="14" />{{ t('scenarios.branch.addMessage') }}
      </Button>
      <p v-else class="muted txt-sm">{{ t('scenarios.branch.maxMessages') }}</p>

      <div class="br__actions">
        <span class="txt-sm fw-semi">{{ t('scenarios.branch.actions') }}</span>
        <ActionRow
          v-for="(a, ai) in branch.actions"
          :key="ai"
          :action="a"
          :media-names="mediaNames"
          :invalid="handoffInvalid(a)"
          @update="setAction(ai, $event)"
          @remove="removeAction(ai)"
        />
        <Button size="sm" variant="ghost" class="br__add" @click="addingAction = !addingAction">
          <Zap :size="14" />{{ t('scenarios.branch.addAction') }}
        </Button>
        <div v-if="addingAction" class="br__picker">
          <button
            v-for="a in ACTION_TYPES"
            :key="a"
            type="button"
            class="br__pick"
            @click="addAction(a)"
          >
            {{ t(`scenarios.action.type.${a}`) }}
          </button>
        </div>
      </div>

      <div v-if="shareWith.length > 0" class="br__share">
        <span class="txt-sm fw-semi">{{ t('scenarios.branch.sharedFor') }}</span>
        <label v-for="o in shareWith" :key="o.index" class="br__chip">
          <input
            type="checkbox"
            :checked="!!branch.on?.includes(o.index)"
            @change="toggleOn(o.index, ($event.target as HTMLInputElement).checked)"
          />
          {{ o.label || '…' }}
        </label>
      </div>

      <label v-if="!fixedThen" class="br__then">
        <span class="txt-sm fw-semi">{{ t('scenarios.branch.then') }}</span>
        <select
          class="input"
          :value="branch.then"
          @change="setThen(($event.target as HTMLSelectElement).value as BranchThen)"
        >
          <option value="continue">{{ t('scenarios.branch.thenContinue') }}</option>
          <option value="end_block">{{ t('scenarios.branch.thenEnd') }}</option>
          <option value="goto_block">{{ t('scenarios.branch.thenGoto') }}</option>
        </select>
      </label>
      <select
        v-if="!fixedThen && branch.then === 'goto_block'"
        :class="['input', { err: gotoInvalid }]"
        :aria-label="t('scenarios.branch.gotoTarget')"
        :value="branch.goto ?? ''"
        @change="update({ goto: ($event.target as HTMLSelectElement).value })"
      >
        <option value="">{{ t('scenarios.branch.gotoNone') }}</option>
        <option v-for="o in gotoOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>

      <div class="br__row br__foot">
        <Button v-if="walkable" size="sm" @click="emit('walk-path')">
          <Route :size="14" />{{ t('scenarios.test.walkPath') }}
        </Button>
        <Button size="sm" variant="ghost" @click="removeBranch">
          <Trash2 :size="14" />{{ t('scenarios.branch.remove') }}
        </Button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.br {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.br__none {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.br__row {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.br__row .input {
  flex: 1;
  min-width: 0;
}
.br__picker {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.br__pick {
  padding: 3px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  font-size: var(--text-sm);
  background: var(--surface);
  cursor: pointer;
}
.br__pick:hover {
  background: var(--surface-hover);
}
.br__msg {
  display: flex;
  flex-direction: column;
  gap: 6px;
  border-left: 2px solid var(--accent-border);
  padding-left: 10px;
}
.br__msg-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.br__text {
  height: 56px;
  padding: 6px 8px;
  resize: vertical;
}
.br__terminal,
.br__actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.br__add {
  align-self: flex-start;
}
.br__count {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  font-variant-numeric: tabular-nums;
}
.br__count--over {
  color: var(--danger);
  font-weight: 600;
}
.br__share {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 10px;
  align-items: center;
}
.br__chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--text-sm);
  cursor: pointer;
}
.br__then {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.br__foot {
  justify-content: space-between;
  border-top: 1px solid var(--border-subtle);
  padding-top: 8px;
}
.err {
  border-color: var(--danger);
}
</style>
