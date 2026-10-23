<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { FileSpreadsheet, FileText, Presentation, Database } from 'lucide-vue-next'
import DocStatus from '@/components/knowledge/DocStatus.vue'
import { kindLabel, pagesToReview } from '@/lib/knowledge'
import type { KnowledgeDocument } from '@/types/api'

defineProps<{ documents: KnowledgeDocument[] }>()
const emit = defineEmits<{ open: [doc: KnowledgeDocument] }>()
const { t, d } = useI18n()

function icon(doc: KnowledgeDocument) {
  if (doc.source === 'corpus') return Database
  if (doc.kind === 'pptx') return Presentation
  if (doc.kind === 'xlsx') return FileSpreadsheet
  return FileText
}

function onKey(e: KeyboardEvent, doc: KnowledgeDocument): void {
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    emit('open', doc)
  }
}
</script>

<template>
  <div class="table-wrap kb-table">
    <table class="tbl">
      <caption class="sr-only">
        {{
          t('knowledge.table.caption')
        }}
      </caption>
      <thead>
        <tr>
          <th scope="col">{{ t('knowledge.table.name') }}</th>
          <th scope="col">{{ t('knowledge.table.kind') }}</th>
          <th scope="col">{{ t('knowledge.table.sites') }}</th>
          <th scope="col" class="td-num">{{ t('knowledge.table.version') }}</th>
          <th scope="col">{{ t('knowledge.table.status') }}</th>
          <th scope="col" class="td-num">{{ t('knowledge.table.chunks') }}</th>
          <th scope="col" class="td-num">{{ t('knowledge.table.clauses') }}</th>
          <th scope="col" class="td-num">{{ t('knowledge.table.review') }}</th>
          <th scope="col">{{ t('knowledge.table.updated') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="doc in documents"
          :key="doc.id"
          class="kb-row"
          tabindex="0"
          :aria-label="`${t('knowledge.actions.open')}: ${doc.doc}`"
          @click="emit('open', doc)"
          @keydown="onKey($event, doc)"
        >
          <td>
            <div class="kb-name">
              <component :is="icon(doc)" :size="16" class="kb-name__ico" aria-hidden="true" />
              <div class="kb-name__text">
                <div class="fw-medium kb-name__title">{{ doc.doc }}</div>
                <div class="txt-xs muted kb-name__file">
                  <template v-if="doc.source === 'corpus'">{{
                    t('knowledge.table.corpus')
                  }}</template>
                  <template v-else>{{ doc.filename }}</template>
                </div>
              </div>
            </div>
          </td>
          <td class="td-mono">{{ kindLabel(doc.kind, doc.filename) }}</td>
          <td>
            <span v-if="doc.sites.length === 0" class="muted">{{
              t('knowledge.table.allSites')
            }}</span>
            <span v-else class="kb-sites">
              <span v-for="s in doc.sites" :key="s" class="badge">{{ t(`common.site.${s}`) }}</span>
            </span>
          </td>
          <td class="td-num">
            <template v-if="doc.source === 'corpus'">—</template>
            <template v-else>v{{ doc.version }}</template>
          </td>
          <td><DocStatus :status="doc.status" /></td>
          <td class="td-num">{{ doc.chunks }}</td>
          <td class="td-num">{{ doc.clauses }}</td>
          <td class="td-num">
            <span v-if="pagesToReview(doc) > 0" class="kb-review">{{ pagesToReview(doc) }}</span>
            <span v-else class="faint">{{ t('knowledge.table.noReview') }}</span>
          </td>
          <td>
            <div v-if="doc.updated_at" class="tabular txt-sm">
              {{ d(new Date(doc.updated_at), 'long') }}
            </div>
            <div v-if="doc.uploaded_by" class="txt-xs muted">{{ doc.uploaded_by }}</div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.kb-table {
  overflow-x: auto;
}
.kb-row {
  cursor: pointer;
}
.kb-row:focus-visible td {
  background: var(--surface-hover);
}
.kb-row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.kb-name {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 220px;
  max-width: 360px;
}
.kb-name__ico {
  flex-shrink: 0;
  margin-top: 2px;
  color: var(--fg-subtle);
}
.kb-name__text {
  min-width: 0;
}
.kb-name__title {
  line-height: 1.3;
}
.kb-name__file {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.kb-sites {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
.kb-review {
  color: var(--warning);
  font-weight: 600;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
</style>
