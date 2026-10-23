<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { BookOpen, CloudOff, FileUp, SearchX, Upload } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import DocumentsTable from '@/components/knowledge/DocumentsTable.vue'
import DocumentDrawer from '@/components/knowledge/DocumentDrawer.vue'
import UploadDialog from '@/components/knowledge/UploadDialog.vue'
import UnansweredPanel from '@/components/knowledge/UnansweredPanel.vue'
import { Button } from '@/components/ui/button'
import { useKnowledgeStore } from '@/stores/knowledge'
import { useSiteStore } from '@/stores/site'
import { errorMessage } from '@/lib/apiError'
import { appliesToSite, sortDocuments } from '@/lib/knowledge'
import type { KnowledgeDocument, SiteCode } from '@/types/api'

// Knowledge base (ux-spec 3.6): documents the bot answers from, upload with
// progress, a document drawer (check + versions) and the unanswered questions.
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useKnowledgeStore()
const { documents, ragUp, loaded, loading, error, clusters, unansweredLoaded } = storeToRefs(store)
// The global site filter narrows the table on the client: a document without
// sites answers everywhere.
const site = useSiteStore()

type Tab = 'documents' | 'unanswered'
const tab = computed<Tab>(() => (route.query.tab === 'unanswered' ? 'unanswered' : 'documents'))
function setTab(v: Tab): void {
  void router.replace({ query: { ...route.query, tab: v === 'documents' ? undefined : v } })
}

onMounted(() => void store.loadDocuments())

const rows = computed(() =>
  sortDocuments(documents.value.filter((d) => appliesToSite(d, site.apiSite()))),
)

// Drawer: the row is looked up by id, so a fresh list (after an upload) shows
// the new version in an open drawer.
const drawerOpen = ref(false)
const selectedId = ref<string | null>(null)
const selected = computed(() => documents.value.find((d) => d.id === selectedId.value) ?? null)
function openDoc(doc: KnowledgeDocument): void {
  selectedId.value = doc.id
  drawerOpen.value = true
}

// Upload dialog: from the head button, a page-wide drop, a document's
// "new version" or an unanswered cluster.
const uploadOpen = ref(false)
const uploadFiles = ref<File[]>([])
const presetDoc = ref<string | null>(null)
const presetSites = ref<SiteCode[]>([])
function openUpload(files: File[] = [], doc: KnowledgeDocument | null = null): void {
  uploadFiles.value = files
  presetDoc.value = doc ? doc.doc : null
  presetSites.value = doc ? [...doc.sites] : []
  drawerOpen.value = false
  uploadOpen.value = true
}
function onUploaded(doc: KnowledgeDocument): void {
  store.upsert(doc)
}
watch(uploadOpen, (v) => {
  if (!v) void store.loadDocuments()
})

// Page-wide drag-and-drop: dropping files anywhere opens the dialog with them.
const dragDepth = ref(0)
function hasFiles(e: DragEvent): boolean {
  return Array.from(e.dataTransfer?.types ?? []).includes('Files')
}
function onDragEnter(e: DragEvent): void {
  if (!hasFiles(e) || uploadOpen.value) return
  dragDepth.value++
}
function onDragLeave(e: DragEvent): void {
  if (!hasFiles(e) || uploadOpen.value) return
  dragDepth.value = Math.max(0, dragDepth.value - 1)
}
function onDrop(e: DragEvent): void {
  if (uploadOpen.value) return
  dragDepth.value = 0
  const files = Array.from(e.dataTransfer?.files ?? [])
  if (files.length > 0) openUpload(files)
}
</script>

<template>
  <PageContainer width="wide">
    <div
      class="kb-page"
      @dragenter.prevent="onDragEnter"
      @dragover.prevent
      @dragleave.prevent="onDragLeave"
      @drop.prevent="onDrop"
    >
      <PageHead :title="t('knowledge.title')">
        <template #subtitle>{{ t('knowledge.subtitle') }}</template>
        <template #actions>
          <Button variant="primary" data-testid="kb-upload" @click="openUpload()">
            <Upload :size="15" aria-hidden="true" />{{ t('knowledge.actions.upload') }}
          </Button>
        </template>
      </PageHead>

      <div v-if="loaded && !ragUp" class="kb-down" role="status">
        <CloudOff :size="16" aria-hidden="true" />
        <span class="txt-sm">{{ t('knowledge.ragDown') }}</span>
      </div>

      <div class="utabs kb-tabs" role="tablist" :aria-label="t('knowledge.tabs.label')">
        <button
          type="button"
          role="tab"
          :aria-selected="tab === 'documents'"
          :class="['utab', { active: tab === 'documents' }]"
          @click="setTab('documents')"
        >
          {{ t('knowledge.tabs.documents') }}
          <span v-if="loaded" class="kb-tab-count tabular">{{ rows.length }}</span>
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="tab === 'unanswered'"
          :class="['utab', { active: tab === 'unanswered' }]"
          data-testid="kb-tab-unanswered"
          @click="setTab('unanswered')"
        >
          {{ t('knowledge.tabs.unanswered') }}
          <span
            v-if="unansweredLoaded && clusters.length > 0"
            class="kb-tab-count kb-tab-count--warn tabular"
          >
            {{ clusters.length }}
          </span>
        </button>
      </div>

      <template v-if="tab === 'documents'">
        <SectionCard v-if="!loaded">
          <LoadingState v-if="loading || !error" variant="table" :rows="5" />
          <ErrorState v-else :message="errorMessage(error, 'knowledge.loadFailed')">
            <template #retry>
              <Button size="sm" @click="store.loadDocuments()">{{
                t('common.actions.retry')
              }}</Button>
            </template>
          </ErrorState>
        </SectionCard>

        <SectionCard v-else-if="documents.length === 0">
          <EmptyState
            :icon="BookOpen"
            :title="t('knowledge.empty.title')"
            :subtitle="t('knowledge.empty.description')"
          >
            <template #action>
              <Button variant="primary" @click="openUpload()">
                <Upload :size="15" aria-hidden="true" />{{ t('knowledge.actions.upload') }}
              </Button>
            </template>
          </EmptyState>
        </SectionCard>

        <SectionCard v-else-if="rows.length === 0">
          <EmptyState :icon="SearchX" :title="t('knowledge.table.noMatches')" />
        </SectionCard>

        <div v-else :class="['col gap-2', { 'is-stale': loading }]">
          <div class="muted txt-sm">{{ t('knowledge.table.count', rows.length) }}</div>
          <DocumentsTable :documents="rows" @open="openDoc" />
        </div>
      </template>

      <UnansweredPanel v-else @upload="openUpload()" />

      <div v-if="dragDepth > 0" class="kb-dropzone" aria-hidden="true">
        <FileUp :size="28" />
        <span class="fw-semi">{{ t('knowledge.upload.dropHere') }}</span>
      </div>
    </div>

    <DocumentDrawer
      v-model:open="drawerOpen"
      :document="selected"
      @upload-version="(d) => openUpload([], d)"
    />
    <UploadDialog
      v-model:open="uploadOpen"
      :files="uploadFiles"
      :preset-doc="presetDoc"
      :preset-sites="presetSites"
      @uploaded="onUploaded"
    />
  </PageContainer>
</template>

<style scoped>
.kb-page {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 60vh;
}
.kb-tabs {
  margin-bottom: 4px;
}
.kb-tab-count {
  margin-left: 6px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--bg-muted);
  font-size: var(--text-xs);
  color: var(--fg-muted);
}
.kb-tab-count--warn {
  background: var(--warning-soft);
  color: var(--warning);
}
.kb-down {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-radius: var(--radius-md);
  background: var(--danger-soft);
  color: var(--fg);
}
.kb-down > svg {
  color: var(--danger);
  flex-shrink: 0;
  margin-top: 2px;
}
.is-stale {
  opacity: 0.6;
}
.kb-dropzone {
  position: absolute;
  inset: -8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border: 2px dashed var(--accent);
  border-radius: var(--radius-lg);
  background: color-mix(in oklab, var(--accent-soft) 85%, transparent);
  color: var(--accent);
  pointer-events: none;
  z-index: 5;
}
</style>
