<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleAlert, CircleCheck, FileUp, LoaderCircle, X } from 'lucide-vue-next'
import AppDialog from '@/components/common/AppDialog.vue'
import { Button } from '@/components/ui/button'
import { apiUploadDocument } from '@/api/knowledge'
import { useOperatorStore } from '@/stores/operator'
import { SITES } from '@/stores/site'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import {
  ACCEPT_ATTR,
  MAX_UPLOAD_MB,
  docNameFromFile,
  fileProblem,
  type FileProblem,
} from '@/lib/knowledge'
import type { KnowledgeDocument, SiteCode } from '@/types/api'

// Drag-and-drop upload with per-file progress (ux-spec 3.6). Files go one by
// one: the rag re-indexes synchronously, so parallel uploads would only queue
// up on its side. Phases: transfer (real progress) → parsing and indexing
// (no progress from the server, an indeterminate bar) → result.

const props = withDefaults(
  defineProps<{
    files?: File[]
    // "Upload a new version" from the drawer: the name is fixed.
    presetDoc?: string | null
    presetSites?: SiteCode[]
  }>(),
  { files: () => [], presetDoc: null, presetSites: () => [] },
)
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ uploaded: [doc: KnowledgeDocument] }>()

const { t } = useI18n()
const toast = useToast()
const operator = useOperatorStore()

type Phase = 'queued' | 'uploading' | 'indexing' | 'done' | 'error'
interface Item {
  key: number
  file: File
  problem: FileProblem
  phase: Phase
  pct: number
  result: KnowledgeDocument | null
  error: string
}

const items = ref<Item[]>([])
const docName = ref('')
const sites = ref<SiteCode[]>([])
const running = ref(false)
const dragging = ref(false)
const input = ref<HTMLInputElement | null>(null)
let seq = 0

function add(list: File[]): void {
  for (const file of list) {
    items.value.push({
      key: ++seq,
      file,
      problem: fileProblem(file.name, file.size),
      phase: 'queued',
      pct: 0,
      result: null,
      error: '',
    })
  }
  if (props.presetDoc === null && items.value.length === 1 && !docName.value) {
    docName.value = docNameFromFile(items.value[0].file.name)
  }
}

watch(
  open,
  (v) => {
    if (!v) return
    items.value = []
    running.value = false
    docName.value = props.presetDoc ?? ''
    sites.value = [...props.presetSites]
    add(props.files)
  },
  { immediate: true },
)

const single = computed(() => items.value.length <= 1)
const ready = computed(() => items.value.filter((i) => i.problem === null && i.phase === 'queued'))
const finished = computed(
  () =>
    items.value.length > 0 &&
    items.value.every((i) => i.phase === 'done' || i.problem !== null || i.phase === 'error'),
)
const okCount = computed(() => items.value.filter((i) => i.phase === 'done').length)
const attempted = computed(() => items.value.some((i) => i.phase !== 'queued'))

function toggleSite(s: SiteCode): void {
  sites.value = sites.value.includes(s) ? sites.value.filter((x) => x !== s) : [...sites.value, s]
}

function remove(key: number): void {
  items.value = items.value.filter((i) => i.key !== key)
}

function onDrop(e: DragEvent): void {
  dragging.value = false
  if (running.value) return
  add(Array.from(e.dataTransfer?.files ?? []))
}

function onPick(e: Event): void {
  const el = e.target as HTMLInputElement
  add(Array.from(el.files ?? []))
  el.value = ''
}

async function start(): Promise<void> {
  if (running.value) return
  running.value = true
  for (const item of ready.value) {
    item.phase = 'uploading'
    try {
      const doc = await apiUploadDocument(
        {
          file: item.file,
          doc: props.presetDoc ?? (single.value ? docName.value.trim() : undefined),
          sites: sites.value,
          uploaded_by: operator.name.trim() || undefined,
        },
        (p) => {
          item.pct = p.total > 0 ? Math.min(100, Math.round((p.loaded / p.total) * 100)) : 0
          if (item.pct >= 100) item.phase = 'indexing'
        },
      )
      item.result = doc
      item.phase = 'done'
      emit('uploaded', doc)
    } catch (e) {
      item.error = errorMessage(e, 'knowledge.upload.failed')
      item.phase = 'error'
    }
  }
  running.value = false
  if (okCount.value > 0 && items.value.every((i) => i.phase !== 'error')) {
    toast.success(t('knowledge.upload.toastDone'))
  }
}

function phaseText(i: Item): string {
  switch (i.phase) {
    case 'queued':
      return t('knowledge.upload.phase.queued')
    case 'uploading':
      return t('knowledge.upload.phase.uploading', { pct: i.pct })
    case 'indexing':
      return t('knowledge.upload.phase.indexing')
    case 'done':
      return i.result && i.result.low_confidence_pages.length > 0
        ? t('knowledge.upload.phase.doneReview', {
            pages: i.result.low_confidence_pages.join(', '),
          })
        : t('knowledge.upload.phase.done', {
            chunks: i.result?.chunks ?? 0,
            clauses: i.result?.clauses ?? 0,
          })
    default:
      return i.error || t('knowledge.upload.phase.error')
  }
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="presetDoc ? t('knowledge.actions.uploadVersion') : t('knowledge.upload.title')"
    :description="t('knowledge.upload.description')"
  >
    <div class="col gap-4">
      <div
        :class="['kb-drop', { 'kb-drop--over': dragging, 'kb-drop--busy': running }]"
        @dragenter.prevent="dragging = true"
        @dragover.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent="onDrop"
      >
        <FileUp :size="22" class="kb-drop__ico" aria-hidden="true" />
        <div class="fw-medium">{{ t('knowledge.upload.drop') }}</div>
        <div class="txt-sm muted">
          {{ t('knowledge.upload.or') }}
          <button type="button" class="kb-link" :disabled="running" @click="input?.click()">
            {{ t('knowledge.upload.choose') }}
          </button>
        </div>
        <div class="txt-xs faint">{{ t('knowledge.upload.formats', { mb: MAX_UPLOAD_MB }) }}</div>
        <input
          ref="input"
          type="file"
          class="kb-file"
          multiple
          :accept="ACCEPT_ATTR"
          data-testid="kb-file-input"
          @change="onPick"
        />
      </div>
      <div class="hint-banner txt-sm">{{ t('knowledge.upload.hint') }}</div>

      <div v-if="items.length > 0" class="col gap-2">
        <div class="field-label">{{ t('knowledge.upload.files') }}</div>
        <ul class="kb-files">
          <li
            v-for="i in items"
            :key="i.key"
            :class="['kb-item', `kb-item--${i.problem ? 'error' : i.phase}`]"
          >
            <div class="row between center gap-2">
              <div class="kb-item__name">
                <CircleCheck
                  v-if="i.phase === 'done'"
                  :size="15"
                  class="kb-ok"
                  aria-hidden="true"
                />
                <CircleAlert
                  v-else-if="i.problem || i.phase === 'error'"
                  :size="15"
                  class="kb-bad"
                  aria-hidden="true"
                />
                <LoaderCircle
                  v-else-if="i.phase === 'uploading' || i.phase === 'indexing'"
                  :size="15"
                  class="kb-spin"
                  aria-hidden="true"
                />
                <span class="fw-medium">{{ i.file.name }}</span>
              </div>
              <button
                v-if="i.phase === 'queued' && !running"
                type="button"
                class="btn btn-ghost btn-icon btn-sm"
                :aria-label="t('knowledge.upload.remove')"
                @click="remove(i.key)"
              >
                <X :size="14" />
              </button>
            </div>
            <div class="txt-xs muted kb-item__phase" aria-live="polite">
              <template v-if="i.problem">{{
                t(`knowledge.upload.problem.${i.problem}`, { mb: MAX_UPLOAD_MB })
              }}</template>
              <template v-else>{{ phaseText(i) }}</template>
            </div>
            <div
              v-if="i.phase === 'uploading' || i.phase === 'indexing'"
              :class="['progress', 'sz-2', { 'kb-indeterminate': i.phase === 'indexing' }]"
              role="progressbar"
              :aria-valuenow="i.phase === 'uploading' ? i.pct : undefined"
              aria-valuemin="0"
              aria-valuemax="100"
            >
              <i :style="{ width: i.phase === 'uploading' ? `${i.pct}%` : '40%' }" />
            </div>
          </li>
        </ul>
      </div>

      <label v-if="presetDoc === null && single" class="field">
        <span class="field-label">{{ t('knowledge.upload.name') }}</span>
        <input
          v-model="docName"
          class="input"
          maxlength="200"
          :disabled="running || attempted"
          :placeholder="t('knowledge.upload.namePlaceholder')"
        />
        <span class="field-hint">{{ t('knowledge.upload.nameHint') }}</span>
      </label>

      <div class="field">
        <span class="field-label">{{ t('knowledge.upload.sites') }}</span>
        <div class="row gap-2 wrap" role="group" :aria-label="t('knowledge.upload.sites')">
          <button
            v-for="s in SITES"
            :key="s"
            type="button"
            :class="['tag-chip', 'kb-chip', { 'kb-chip--on': sites.includes(s) }]"
            :aria-pressed="sites.includes(s)"
            :disabled="running || attempted"
            @click="toggleSite(s)"
          >
            {{ t(`common.site.${s}`) }}
          </button>
        </div>
        <span class="field-hint">{{ t('knowledge.upload.sitesHint') }}</span>
      </div>
    </div>

    <template #foot>
      <span class="txt-sm muted">
        <template v-if="attempted">{{
          t('knowledge.upload.summary', { ok: okCount, total: items.length })
        }}</template>
      </span>
      <div class="row gap-2">
        <Button
          v-if="!finished || !attempted"
          variant="ghost"
          :disabled="running"
          @click="open = false"
        >
          {{ t('common.actions.cancel') }}
        </Button>
        <Button
          v-if="!attempted || ready.length > 0"
          variant="primary"
          :disabled="running || ready.length === 0"
          @click="start"
        >
          <LoaderCircle v-if="running" :size="14" class="kb-spin" aria-hidden="true" />
          {{ running ? t('knowledge.upload.uploading') : t('knowledge.upload.start') }}
        </Button>
        <Button v-else variant="primary" @click="open = false">{{
          t('knowledge.upload.done')
        }}</Button>
      </div>
    </template>
  </AppDialog>
</template>

<style scoped>
.kb-drop {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 28px 16px;
  border: 1.5px dashed var(--border-strong);
  border-radius: var(--radius-lg);
  background: var(--bg-subtle);
  text-align: center;
  transition:
    border-color 0.12s,
    background 0.12s;
}
.kb-drop--over {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.kb-drop--busy {
  opacity: 0.6;
}
.kb-drop__ico {
  color: var(--fg-subtle);
  margin-bottom: 4px;
}
.kb-link {
  color: var(--accent);
  font-weight: 500;
  text-decoration: underline;
  text-underline-offset: 2px;
  cursor: pointer;
}
.kb-file {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}
.kb-files {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.kb-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}
.kb-item--error {
  border-color: color-mix(in oklab, var(--danger) 40%, var(--border));
}
.kb-item--error .kb-item__phase {
  color: var(--danger);
}
.kb-item__name {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.kb-ok {
  color: var(--success);
  flex-shrink: 0;
}
.kb-bad {
  color: var(--danger);
  flex-shrink: 0;
}
.kb-spin {
  animation: kb-spin 1s linear infinite;
  flex-shrink: 0;
}
@keyframes kb-spin {
  to {
    transform: rotate(360deg);
  }
}
.kb-indeterminate > i {
  animation: kb-slide 1.2s ease-in-out infinite;
}
@keyframes kb-slide {
  from {
    transform: translateX(-100%);
  }
  to {
    transform: translateX(250%);
  }
}
.kb-chip {
  cursor: pointer;
}
.kb-chip--on {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: var(--accent);
}
</style>
