<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Upload } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import LoadingState from '@/components/common/LoadingState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import { apiDocumentVersions } from '@/api/knowledge'
import { errorMessage } from '@/lib/apiError'
import { sizeParts } from '@/lib/knowledge'
import type { KnowledgeDocument, KnowledgeDocumentVersion } from '@/types/api'

// "Versions" tab (ux-spec 3.6): the document is never edited, it is re-uploaded
// under the same name; the newest version answers, the previous one leaves the
// search at once (the rag replaces a document by name).
const props = defineProps<{ document: KnowledgeDocument }>()
const emit = defineEmits<{ uploadVersion: [] }>()
const { t, d } = useI18n()

const items = ref<KnowledgeDocumentVersion[]>([])
const loading = ref(false)
const error = ref<unknown>(null)

async function load(): Promise<void> {
  if (props.document.source !== 'upload') return
  loading.value = true
  error.value = null
  try {
    items.value = (await apiDocumentVersions(props.document.id)).items
  } catch (e) {
    error.value = e
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.document.id, props.document.version],
  () => void load(),
  { immediate: true },
)

function size(bytes: number): string {
  const p = sizeParts(bytes)
  return t(`knowledge.size.${p.unit}`, { n: p.value })
}
</script>

<template>
  <div class="col gap-4">
    <div class="hint-banner txt-sm">
      {{
        document.source === 'corpus' ? t('knowledge.versions.corpus') : t('knowledge.versions.note')
      }}
    </div>
    <div>
      <Button size="sm" @click="emit('uploadVersion')">
        <Upload :size="14" aria-hidden="true" />{{ t('knowledge.actions.uploadVersion') }}
      </Button>
    </div>

    <template v-if="document.source === 'upload'">
      <LoadingState v-if="loading && items.length === 0" :rows="3" />
      <ErrorState v-else-if="error" :message="errorMessage(error, 'knowledge.versions.failed')">
        <template #retry>
          <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
      <p v-else-if="items.length === 0" class="muted txt-sm">{{ t('knowledge.versions.empty') }}</p>
      <ol v-else class="kb-versions">
        <li
          v-for="v in items"
          :key="v.version"
          :class="['kb-version', { 'kb-version--current': v.current }]"
        >
          <div class="row between center gap-2">
            <div class="row center gap-2">
              <span class="fw-semi">{{ t('knowledge.versions.version', { n: v.version }) }}</span>
              <Badge v-if="v.current" variant="success">{{
                t('knowledge.versions.current')
              }}</Badge>
            </div>
            <span class="txt-sm muted tabular">{{ d(new Date(v.uploaded_at), 'long') }}</span>
          </div>
          <div class="txt-sm">
            <span class="fw-medium">{{ v.filename }}</span>
            <span class="dot-sep" />
            <span class="muted">{{ size(v.size) }}</span>
          </div>
          <div class="txt-xs muted">
            {{
              v.low_confidence_pages.length > 0
                ? t('knowledge.versions.statsReview', {
                    chunks: v.chunks,
                    clauses: v.clauses,
                    pages: v.low_confidence_pages.join(', '),
                  })
                : t('knowledge.versions.stats', { chunks: v.chunks, clauses: v.clauses })
            }}
            <template v-if="v.uploaded_by">
              · {{ t('knowledge.versions.uploadedBy', { name: v.uploaded_by }) }}</template
            >
          </div>
        </li>
      </ol>
    </template>
  </div>
</template>

<style scoped>
.kb-versions {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.kb-version {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}
.kb-version--current {
  border-color: color-mix(in oklab, var(--success) 45%, var(--border));
}
</style>
