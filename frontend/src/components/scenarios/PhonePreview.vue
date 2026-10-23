<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Paperclip } from 'lucide-vue-next'
import { branchFor, effectiveAdvance, optionKeys, truncateLabel } from '@/lib/scenarioBranches'
import type { BranchAction, BranchMessage, ScenarioStep, StepButton } from '@/types/api'

// Schematic chat preview (verdict §Q3, Should): the block up to the selected
// step, its buttons cut at 18 characters as MAX does, and — when a branch tab is
// open — the path of that branch. Not pixel-exact on purpose (verdict §5).
const props = defineProps<{ block: ScenarioStep[]; selectedId: string; tab: number | null }>()
const { t } = useI18n()

const form = ref<'vy' | 'ty'>('vy')
// The name placeholders of the contract, upper/lower case Cyrillic and {name} (escaped: no Cyrillic in src).
const PLACEHOLDER_NAME = /\{(\u0418\u043c\u044f|\u0438\u043c\u044f|name)\}/g

const shown = computed(() => {
  const i = props.block.findIndex((s) => s.id === props.selectedId)
  return i < 0 ? [] : props.block.slice(0, i + 1)
})
const selected = computed(() => shown.value[shown.value.length - 1] ?? null)
const branchInfo = computed(() =>
  selected.value && props.tab !== null ? branchFor(selected.value, props.tab) : null,
)
const pressed = computed<StepButton | null>(() =>
  selected.value && props.tab !== null ? (selected.value.buttons[props.tab] ?? null) : null,
)

function text(m: Pick<BranchMessage, 'text_ty' | 'text_vy'>): string {
  const raw = form.value === 'ty' ? m.text_ty || m.text_vy : m.text_vy || m.text_ty
  return raw.replace(PLACEHOLDER_NAME, t('scenarios.preview.sampleName'))
}
function advanceOf(s: ScenarioStep, i: number) {
  return effectiveAdvance(s, i < props.block.length - 1)
}
function actionNote(a: BranchAction): string {
  switch (a.type) {
    case 'escalate':
      return t('scenarios.preview.escalate', {
        category: a.category ?? '',
        route: a.route ? t(`tickets.route.${a.route}`) : '—',
      })
    case 'handoff':
      return t('scenarios.preview.handoff', {
        to: a.to ? t(`scenarios.action.people.${a.to}`) : '—',
      })
    case 'set_field':
      return t('scenarios.preview.setField', {
        value: a.value === 'ty' ? t('scenarios.action.ty') : t('scenarios.action.vy'),
      })
    case 'set_flag':
      return t('scenarios.preview.setFlag', { flag: a.flag || '—' })
    case 'send_file':
      return t('scenarios.preview.sendFile', { name: a.media || '—' })
    case 'open_free_text':
      return t('scenarios.preview.freeText')
  }
}
const firstQuestion = computed(() => selected.value?.survey?.questions[0] ?? null)
const surveyKeys = computed(() =>
  selected.value?.survey && firstQuestion.value
    ? optionKeys(firstQuestion.value, selected.value.survey.scale)
    : [],
)
function keyLabel(key: string, label: string | null): string {
  if (label !== null) return label
  return key === '1' ? t('scenarios.survey.yes') : t('scenarios.survey.no')
}
</script>

<template>
  <aside class="phone" :aria-label="t('scenarios.preview.title')">
    <div class="phone__top">
      <span class="fw-semi">{{ t('scenarios.preview.bot') }}</span>
      <span class="phone__forms" role="radiogroup" :aria-label="t('scenarios.preview.form')">
        <button
          v-for="f in ['vy', 'ty'] as const"
          :key="f"
          type="button"
          role="radio"
          :aria-checked="form === f"
          :class="['phone__form', { on: form === f }]"
          @click="form = f"
        >
          {{ t(`scenarios.action.${f}`) }}
        </button>
      </span>
    </div>
    <div class="phone__chat">
      <template v-for="(s, i) in shown" :key="s.id">
        <div v-if="s.hr_task" class="note note--hr">
          {{ t('scenarios.preview.hrTask', { title: s.title }) }}
        </div>
        <template v-else>
          <div v-if="s.flag_required" class="tagline">{{ t('scenarios.preview.flag') }}</div>
          <div :class="['bubble', { 'bubble--dim': s.id !== selectedId }]">
            <div v-if="(s.media ?? []).length > 0" class="bubble__media">
              <span v-for="m in s.media" :key="m.name"><Paperclip :size="11" />{{ m.name }}</span>
            </div>
            <div class="bubble__text">{{ text(s) || '…' }}</div>
          </div>
          <template v-if="s.id === selectedId">
            <div v-if="s.kind === 'survey' && firstQuestion" class="survey">
              <div class="bubble">{{ firstQuestion.text }}</div>
              <div class="kb kb--grid">
                <span v-for="k in surveyKeys" :key="k.key" class="kb__btn">
                  {{ truncateLabel(keyLabel(k.key, k.label)) }}
                </span>
              </div>
            </div>
            <div v-if="s.buttons.length > 0" class="kb">
              <span
                v-for="(b, bi) in s.buttons"
                :key="bi"
                :class="['kb__btn', { 'kb__btn--on': tab === bi }]"
                :title="b.label"
              >
                {{ truncateLabel(b.label || '…') }}
              </span>
            </div>
            <div v-if="s.kind === 'input'" class="note">{{ t('scenarios.preview.input') }}</div>
          </template>
          <div v-if="advanceOf(s, block.indexOf(s)) === 'button'" class="kb">
            <span class="kb__btn kb__btn--next">{{ t('scenarios.preview.next') }}</span>
          </div>
          <div
            v-else-if="advanceOf(s, block.indexOf(s)) === 'auto' && i < shown.length - 1"
            class="typing"
          >
            {{ t('scenarios.preview.typing') }}
          </div>
        </template>
      </template>

      <template v-if="pressed">
        <div class="mine">{{ pressed.label || '…' }}</div>
        <template v-if="branchInfo">
          <template v-for="(m, mi) in branchInfo.branch.messages" :key="mi">
            <div class="bubble">
              <div v-if="m.media.length > 0" class="bubble__media">
                <span v-for="r in m.media" :key="r.name"><Paperclip :size="11" />{{ r.name }}</span>
              </div>
              <div class="bubble__text">{{ text(m) || '…' }}</div>
            </div>
            <div v-if="m.buttons.length > 0" class="kb">
              <span v-for="(b, bi) in m.buttons" :key="bi" class="kb__btn" :title="b.label">
                {{ truncateLabel(b.label || '…') }}
              </span>
            </div>
          </template>
          <div v-for="(a, ai) in branchInfo.branch.actions" :key="'a' + ai" class="note">
            {{ actionNote(a) }}
          </div>
          <div class="note note--flow">
            {{
              branchInfo.branch.then === 'end_block'
                ? t('scenarios.preview.endBlock')
                : branchInfo.branch.then === 'goto_block'
                  ? t('scenarios.preview.goto', { key: branchInfo.branch.goto || '—' })
                  : t('scenarios.preview.continue')
            }}
          </div>
        </template>
        <div v-else class="note">{{ t('scenarios.preview.recorded') }}</div>
      </template>
    </div>
    <div class="phone__caption">{{ t('scenarios.preview.caption') }}</div>
  </aside>
</template>

<style scoped>
.phone {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border-strong);
  border-radius: 22px;
  background: var(--bg-subtle);
  overflow: hidden;
  position: sticky;
  top: 68px;
  max-height: calc(100vh - 84px);
  min-width: 0;
}
.phone__top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
  font-size: var(--text-sm);
}
.phone__forms {
  display: inline-flex;
  gap: 2px;
}
.phone__form {
  padding: 1px 6px;
  border-radius: var(--radius-sm);
  font-size: var(--text-xs);
  color: var(--fg-muted);
  cursor: pointer;
}
.phone__form.on {
  background: var(--accent-soft);
  color: var(--fg);
}
.phone__chat {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px;
  overflow-y: auto;
  min-height: 320px;
}
.bubble {
  align-self: flex-start;
  max-width: 88%;
  padding: 6px 9px;
  border-radius: 12px 12px 12px 4px;
  background: var(--surface);
  border: 1px solid var(--border);
  font-size: var(--text-sm);
  line-height: 1.4;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.bubble--dim {
  opacity: 0.6;
}
.bubble__media {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 4px;
}
.bubble__media span {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 5px;
  border-radius: var(--radius-sm);
  background: var(--bg-muted);
  font-size: var(--text-xs);
}
.mine {
  align-self: flex-end;
  max-width: 80%;
  padding: 6px 9px;
  border-radius: 12px 12px 4px 12px;
  background: var(--accent);
  color: var(--accent-fg);
  font-size: var(--text-sm);
}
.kb {
  display: flex;
  flex-direction: column;
  gap: 3px;
  max-width: 88%;
}
.kb--grid {
  flex-direction: row;
  flex-wrap: wrap;
}
.kb__btn {
  padding: 4px 8px;
  border-radius: 8px;
  border: 1px solid var(--accent-border);
  background: var(--surface);
  font-size: var(--text-sm);
  text-align: center;
  white-space: nowrap;
}
.kb--grid .kb__btn {
  min-width: 28px;
}
.kb__btn--on {
  background: var(--accent-soft);
  border-color: var(--accent);
  font-weight: 600;
}
.kb__btn--next {
  color: var(--fg-muted);
}
.typing,
.tagline {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  font-style: italic;
}
.note {
  align-self: center;
  max-width: 92%;
  padding: 3px 8px;
  border-radius: var(--radius-full);
  background: var(--info-soft);
  font-size: var(--text-xs);
  text-align: center;
}
.note--hr {
  background: var(--warning-soft);
  border-radius: var(--radius-md);
}
.note--flow {
  background: var(--bg-muted);
}
.survey {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.phone__caption {
  padding: 6px 12px;
  border-top: 1px solid var(--border);
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  text-align: center;
}
</style>
