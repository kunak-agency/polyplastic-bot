<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { TriangleAlert, X } from 'lucide-vue-next'
import DocStatus from '@/components/knowledge/DocStatus.vue'
import AskPanel from '@/components/knowledge/AskPanel.vue'
import VersionsPanel from '@/components/knowledge/VersionsPanel.vue'
import { kindLabel } from '@/lib/knowledge'
import type { KnowledgeDocument } from '@/types/api'

// Document side sheet (ux-spec 3.6): stats, pages to proofread, and two tabs —
// "Check" (question → answer with quotes) and "Versions" (history). The
// "Chunks" tab of the spec waits for a chunk listing endpoint in the rag.
const props = defineProps<{ document: KnowledgeDocument | null }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ uploadVersion: [doc: KnowledgeDocument] }>()
const { t } = useI18n()

type Tab = 'check' | 'versions'
const tab = ref<Tab>('check')
watch(
  () => props.document?.id,
  () => {
    tab.value = 'check'
  },
)

const meta = computed(() => {
  const d = props.document
  if (!d) return ''
  const kind = kindLabel(d.kind, d.filename)
  return d.source === 'corpus'
    ? t('knowledge.drawer.corpusMeta', { kind })
    : t('knowledge.drawer.meta', { kind, version: d.version })
})
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="drawer-backdrop" />
      <DialogContent v-if="document" class="drawer kb-drawer" data-testid="kb-drawer">
        <div class="drawer-main">
          <div class="drawer-head">
            <div class="kb-drawer__title">
              <DialogTitle as="h3">{{ document.doc }}</DialogTitle>
              <DialogDescription as="p" class="row center gap-2 wrap">
                <span>{{ meta }}</span>
                <DocStatus :status="document.status" />
              </DialogDescription>
            </div>
            <DialogClose
              class="btn btn-ghost btn-icon btn-sm"
              :aria-label="t('knowledge.drawer.close')"
            >
              <X :size="16" />
            </DialogClose>
          </div>

          <div class="drawer-body col gap-4">
            <dl class="kb-stats">
              <div>
                <dt>{{ t('knowledge.drawer.chunks') }}</dt>
                <dd class="tabular">{{ document.chunks }}</dd>
              </div>
              <div>
                <dt>{{ t('knowledge.drawer.clauses') }}</dt>
                <dd class="tabular">{{ document.clauses }}</dd>
              </div>
              <div>
                <dt>{{ t('knowledge.drawer.review') }}</dt>
                <dd :class="['tabular', { 'kb-warn': document.low_confidence_pages.length > 0 }]">
                  {{ document.low_confidence_pages.length }}
                </dd>
              </div>
            </dl>

            <div v-if="document.low_confidence_pages.length > 0" class="kb-review">
              <TriangleAlert :size="16" aria-hidden="true" />
              <span class="txt-sm">{{
                t('knowledge.drawer.reviewPages', {
                  pages: document.low_confidence_pages.join(', '),
                })
              }}</span>
            </div>
            <div
              v-if="document.status !== 'ready' && document.status !== 'needs_review'"
              class="kb-review"
            >
              <TriangleAlert :size="16" aria-hidden="true" />
              <span class="txt-sm">{{ t(`knowledge.statusHint.${document.status}`) }}</span>
            </div>

            <div class="utabs" role="tablist">
              <button
                v-for="k in ['check', 'versions'] as const"
                :key="k"
                type="button"
                role="tab"
                :aria-selected="tab === k"
                :class="['utab', { active: tab === k }]"
                @click="tab = k"
              >
                {{ t(`knowledge.drawer.tabs.${k}`) }}
              </button>
            </div>

            <AskPanel v-if="tab === 'check'" :doc="document.doc" />
            <VersionsPanel
              v-else
              :document="document"
              @upload-version="emit('uploadVersion', document)"
            />
          </div>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.kb-drawer {
  grid-template-columns: 1fr;
  width: 640px;
}
.kb-drawer__title {
  min-width: 0;
}
.kb-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  margin: 0;
}
.kb-stats > div {
  padding: 10px 12px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
}
.kb-stats dt {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.kb-stats dd {
  margin: 2px 0 0;
  font-size: var(--text-xl);
  font-weight: 600;
}
.kb-warn {
  color: var(--warning);
}
.kb-review {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-radius: var(--radius-md);
  background: var(--warning-soft);
}
.kb-review > svg {
  color: var(--warning);
  flex-shrink: 0;
  margin-top: 2px;
}
</style>
