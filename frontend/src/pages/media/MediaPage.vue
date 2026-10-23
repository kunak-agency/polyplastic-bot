<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { FileUp, Images, Search, SearchX, Upload } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import AppDialog from '@/components/common/AppDialog.vue'
import MediaTable from '@/components/media/MediaTable.vue'
import { Button } from '@/components/ui/button'
import {
  apiCreateMedia,
  apiDeleteMedia,
  apiDeleteMediaFile,
  apiPutMediaFile,
  apiRenameMedia,
} from '@/api/media'
import { useMediaStore } from '@/stores/media'
import { useOperatorStore } from '@/stores/operator'
import { useSiteStore } from '@/stores/site'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import {
  MAX_MEDIA_MB,
  MEDIA_ACCEPT,
  countMissing,
  filterMedia,
  mediaFileProblem,
  mediaNameFromFile,
  sortMedia,
} from '@/lib/media'
import type { MediaItem, MediaKind, SiteCode } from '@/types/api'

// Media library (api_contract.md v4, verdict Q5): images and PDF maps the
// bot attaches to scenario messages, one file per site plus a common one.
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useMediaStore()
const { items, loaded, loading, error } = storeToRefs(store)
const operator = useOperatorStore()
const toast = useToast()
const siteFilter = useSiteStore()

onMounted(() => void store.load())

// Filters live in the route query: ?missing=1&kind=image&q=...
const q = ref(typeof route.query.q === 'string' ? route.query.q : '')
const missingOnly = computed(() => route.query.missing === '1')
const kind = computed<MediaKind | null>(() =>
  route.query.kind === 'image' || route.query.kind === 'file' ? route.query.kind : null,
)
function setQuery(patch: Record<string, string | undefined>): void {
  void router.replace({ query: { ...route.query, ...patch } })
}
function toggleMissing(): void {
  setQuery({ missing: missingOnly.value ? undefined : '1' })
}
function toggleKind(k: MediaKind): void {
  setQuery({ kind: kind.value === k ? undefined : k })
}
function resetFilters(): void {
  q.value = ''
  void router.replace({ query: {} })
}

const rows = computed(() =>
  sortMedia(
    filterMedia(items.value, { query: q.value, missingOnly: missingOnly.value, kind: kind.value }),
  ),
)
const missing = computed(() => countMissing(items.value))
const busyId = ref<string | null>(null)

function problemText(p: ReturnType<typeof mediaFileProblem>): string {
  return t(`media.problem.${p}`, { mb: MAX_MEDIA_MB })
}

async function run<T>(id: string | null, fn: () => Promise<T>): Promise<T | null> {
  busyId.value = id
  try {
    return await fn()
  } catch (e) {
    toast.error(errorMessage(e, 'media.toast.failed'))
    return null
  } finally {
    busyId.value = null
  }
}

async function onUpload(item: MediaItem, site: SiteCode | null, file: File): Promise<void> {
  const p = mediaFileProblem(file.name, file.size, item, site)
  if (p) {
    toast.error(problemText(p))
    return
  }
  const res = await run(item.id, () =>
    apiPutMediaFile(item.id, { file, site, updated_by: operator.name }),
  )
  if (res) {
    store.upsert(res)
    toast.success(t('media.toast.uploaded', { name: res.name }))
  }
}

async function createFrom(files: File[]): Promise<void> {
  for (const file of files) {
    const p = mediaFileProblem(file.name, file.size)
    if (p) {
      toast.error(`${file.name}: ${problemText(p)}`)
      continue
    }
    const res = await run(null, () =>
      apiCreateMedia({ file, name: mediaNameFromFile(file.name), updated_by: operator.name }),
    )
    if (res) {
      store.upsert(res)
      toast.success(t('media.toast.created', { name: res.name }))
    }
  }
}

async function onRemoveFile(item: MediaItem, site: SiteCode | null): Promise<void> {
  const res = await run(item.id, () => apiDeleteMediaFile(item.id, site))
  if (res) {
    store.upsert(res)
    toast.success(t('media.toast.removedFile'))
  }
}

async function onRename(item: MediaItem, name: string): Promise<void> {
  const res = await run(item.id, () => apiRenameMedia(item.id, name, operator.name))
  if (res) {
    const { updated_refs: refs, ...rest } = res
    store.upsert(rest)
    toast.success(refs > 0 ? t('media.rename.doneRefs', { n: refs }) : t('media.rename.done'))
  }
}

const toDelete = ref<MediaItem | null>(null)
const deleteOpen = computed({
  get: () => toDelete.value !== null,
  set: (v: boolean) => {
    if (!v) toDelete.value = null
  },
})
async function confirmDelete(): Promise<void> {
  const item = toDelete.value
  if (!item) return
  toDelete.value = null
  const ok = await run(item.id, async () => {
    await apiDeleteMedia(item.id)
    return true
  })
  if (ok) {
    store.remove(item.id)
    toast.success(t('media.toast.deleted', { name: item.name }))
  }
}

// Head button and page-wide drop: every file becomes a new entry named after
// it (a stub with that name gets the file).
const input = ref<HTMLInputElement | null>(null)
function onPicked(e: Event): void {
  const el = e.target as HTMLInputElement
  const files = Array.from(el.files ?? [])
  el.value = ''
  if (files.length > 0) void createFrom(files)
}
const dragDepth = ref(0)
function hasFiles(e: DragEvent): boolean {
  return Array.from(e.dataTransfer?.types ?? []).includes('Files')
}
function onDragEnter(e: DragEvent): void {
  if (hasFiles(e)) dragDepth.value++
}
function onDragLeave(e: DragEvent): void {
  if (hasFiles(e)) dragDepth.value = Math.max(0, dragDepth.value - 1)
}
function onDrop(e: DragEvent): void {
  dragDepth.value = 0
  const files = Array.from(e.dataTransfer?.files ?? [])
  if (files.length > 0) void createFrom(files)
}
</script>

<template>
  <PageContainer width="wide">
    <div
      class="mp-page"
      @dragenter.prevent="onDragEnter"
      @dragover.prevent
      @dragleave.prevent="onDragLeave"
      @drop.prevent="onDrop"
    >
      <PageHead :title="t('media.title')">
        <template #subtitle>{{ t('media.subtitle') }}</template>
        <template #actions>
          <input
            ref="input"
            type="file"
            multiple
            class="mp-hidden"
            :accept="MEDIA_ACCEPT"
            tabindex="-1"
            aria-hidden="true"
            @change="onPicked"
          />
          <Button variant="primary" data-testid="media-upload" @click="input?.click()">
            <Upload :size="15" aria-hidden="true" />{{ t('media.actions.upload') }}
          </Button>
        </template>
      </PageHead>

      <div class="mp-toolbar">
        <label class="input-with-icon mp-search">
          <Search class="ico-leading" :size="14" aria-hidden="true" />
          <input
            v-model="q"
            class="input"
            type="search"
            :placeholder="t('media.search.placeholder')"
            :aria-label="t('media.search.label')"
          />
        </label>
        <div class="mp-chips" role="group" :aria-label="t('media.filters.label')">
          <button
            type="button"
            :class="['mp-chip', { 'mp-chip--on': missingOnly, 'mp-chip--warn': missing > 0 }]"
            :aria-pressed="missingOnly"
            data-testid="media-filter-missing"
            @click="toggleMissing"
          >
            {{ t('media.filters.missing') }}
            <span class="mp-chip__count tabular">{{ missing }}</span>
          </button>
          <button
            v-for="k in ['image', 'file'] as const"
            :key="k"
            type="button"
            :class="['mp-chip', { 'mp-chip--on': kind === k }]"
            :aria-pressed="kind === k"
            @click="toggleKind(k)"
          >
            {{ t(`media.filters.${k}`) }}
          </button>
        </div>
      </div>

      <SectionCard v-if="!loaded">
        <LoadingState v-if="loading || !error" variant="table" :rows="5" />
        <ErrorState v-else :message="errorMessage(error, 'media.loadFailed')">
          <template #retry>
            <Button size="sm" @click="store.load()">{{ t('common.actions.retry') }}</Button>
          </template>
        </ErrorState>
      </SectionCard>

      <SectionCard v-else-if="items.length === 0">
        <EmptyState
          :icon="Images"
          :title="t('media.empty.title')"
          :subtitle="t('media.empty.description')"
        >
          <template #action>
            <Button variant="primary" @click="input?.click()">
              <Upload :size="15" aria-hidden="true" />{{ t('media.actions.upload') }}
            </Button>
          </template>
        </EmptyState>
      </SectionCard>

      <SectionCard v-else-if="rows.length === 0">
        <EmptyState :icon="SearchX" :title="t('media.noMatches')">
          <template #action>
            <Button @click="resetFilters">{{ t('media.actions.resetFilters') }}</Button>
          </template>
        </EmptyState>
      </SectionCard>

      <div v-else :class="['col gap-2', { 'is-stale': loading }]">
        <div class="muted txt-sm">{{ t('media.table.count', rows.length) }}</div>
        <MediaTable
          :items="rows"
          :all="items"
          :busy-id="busyId"
          :site="siteFilter.apiSite()"
          @upload="onUpload"
          @remove-file="onRemoveFile"
          @rename="onRename"
          @delete="(item) => (toDelete = item)"
        />
      </div>

      <div v-if="dragDepth > 0" class="mp-dropzone" aria-hidden="true">
        <FileUp :size="28" />
        <span class="fw-semi">{{ t('media.dropHere') }}</span>
      </div>
    </div>

    <AppDialog
      v-model:open="deleteOpen"
      :title="t('media.confirmDelete.title', { name: toDelete?.name ?? '' })"
      :description="t('media.confirmDelete.description')"
    >
      <template #foot>
        <Button @click="toDelete = null">{{ t('common.actions.cancel') }}</Button>
        <Button variant="danger" data-testid="media-delete-confirm" @click="confirmDelete">
          {{ t('common.actions.delete') }}
        </Button>
      </template>
    </AppDialog>
  </PageContainer>
</template>

<style scoped>
.mp-page {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 60vh;
}
.mp-hidden {
  display: none;
}
.mp-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
}
.mp-search {
  width: 280px;
}
.mp-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.mp-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--fg-muted);
  font-size: var(--text-sm);
  cursor: pointer;
}
.mp-chip:hover {
  color: var(--fg);
  border-color: var(--border-strong);
}
.mp-chip--warn .mp-chip__count {
  color: var(--warning);
  opacity: 1;
  font-weight: 600;
}
.mp-chip--on,
.mp-chip--on:hover {
  background: var(--fg);
  border-color: var(--fg);
  color: var(--bg);
}
.mp-chip--on .mp-chip__count {
  color: var(--bg);
}
.mp-chip__count {
  font-size: var(--text-xs);
  opacity: 0.7;
}
.is-stale {
  opacity: 0.6;
}
.mp-dropzone {
  position: absolute;
  top: -8px;
  left: -8px;
  right: -8px;
  height: 120px;
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
