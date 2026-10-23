<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Pencil, Plus, Trash2, TriangleAlert, X } from 'lucide-vue-next'
import MediaThumb from '@/components/media/MediaThumb.vue'
import {
  MEDIA_ACCEPT,
  canDelete,
  mediaSize,
  mediaSlots,
  nameTaken,
  uncoveredSites,
} from '@/lib/media'
import type { MediaFile, MediaItem, SiteCode } from '@/types/api'

// Media library table: name (renamed in place), preview, kind, one chip per
// file slot (common + sites: click or drop a file to upload, x to remove),
// usage count and status. Dropping a file on a row makes it the common file;
// dropping it on a chip targets that site.
const props = defineProps<{
  items: MediaItem[]
  all: MediaItem[]
  busyId?: string | null
  // Site whose file the previews show (the topbar filter); null: common.
  site?: SiteCode | null
}>()
const emit = defineEmits<{
  upload: [item: MediaItem, site: SiteCode | null, file: File]
  removeFile: [item: MediaItem, site: SiteCode | null]
  rename: [item: MediaItem, name: string]
  delete: [item: MediaItem]
}>()
const { t, d } = useI18n()

function slotLabel(site: SiteCode | null): string {
  return site ? t(`common.site.${site}`) : t('media.slot.common')
}

function fileTitle(f: MediaFile): string {
  const s = mediaSize(f.size)
  return `${f.filename} · ${t(`media.size.${s.unit}`, { n: s.value })}`
}

// ── upload through a shared hidden input ──
const input = ref<HTMLInputElement | null>(null)
let target: { item: MediaItem; site: SiteCode | null } | null = null
function pick(item: MediaItem, site: SiteCode | null): void {
  target = { item, site }
  input.value?.click()
}
function onPicked(e: Event): void {
  const el = e.target as HTMLInputElement
  const file = el.files?.[0]
  if (file && target) emit('upload', target.item, target.site, file)
  el.value = ''
  target = null
}

// ── drag and drop ──
const dropRow = ref<string | null>(null)
const dropSlot = ref<string | null>(null)
function hasFiles(e: DragEvent): boolean {
  return Array.from(e.dataTransfer?.types ?? []).includes('Files')
}
function onRowOver(e: DragEvent, item: MediaItem): void {
  if (!hasFiles(e)) return
  e.preventDefault()
  e.stopPropagation()
  dropRow.value = item.id
}
function onRowLeave(e: DragEvent, item: MediaItem): void {
  const to = e.relatedTarget as Node | null
  if (to && (e.currentTarget as HTMLElement).contains(to)) return
  if (dropRow.value === item.id) dropRow.value = null
}
function onSlotOver(e: DragEvent, item: MediaItem, site: SiteCode | null): void {
  if (!hasFiles(e)) return
  e.preventDefault()
  dropSlot.value = `${item.id}:${site ?? ''}`
}
function onDrop(e: DragEvent, item: MediaItem, site: SiteCode | null): void {
  if (!hasFiles(e)) return
  e.preventDefault()
  e.stopPropagation()
  dropRow.value = null
  dropSlot.value = null
  const file = e.dataTransfer?.files?.[0]
  if (file) emit('upload', item, site, file)
}

// ── rename in place ──
const editingId = ref<string | null>(null)
const draft = ref('')
const renameError = ref<string | null>(null)
const nameInput = ref<HTMLInputElement[]>([])
async function startRename(item: MediaItem): Promise<void> {
  editingId.value = item.id
  draft.value = item.name
  renameError.value = null
  await nextTick()
  nameInput.value[0]?.select()
}
function cancelRename(): void {
  editingId.value = null
  renameError.value = null
}
function saveRename(item: MediaItem): void {
  const name = draft.value.replace(/\s+/g, ' ').trim()
  if (!name) {
    renameError.value = t('media.rename.empty')
    return
  }
  if (name === item.name) {
    cancelRename()
    return
  }
  if (nameTaken(props.all, name, item.id)) {
    renameError.value = t('media.rename.taken')
    return
  }
  emit('rename', item, name)
  cancelRename()
}
</script>

<template>
  <div class="table-wrap mt-table">
    <input
      ref="input"
      type="file"
      class="mt-hidden"
      :accept="MEDIA_ACCEPT"
      tabindex="-1"
      aria-hidden="true"
      @change="onPicked"
    />
    <table class="tbl">
      <caption class="mt-sr">
        {{
          t('media.table.caption')
        }}
      </caption>
      <thead>
        <tr>
          <th scope="col">{{ t('media.table.name') }}</th>
          <th scope="col">{{ t('media.table.preview') }}</th>
          <th scope="col">{{ t('media.table.kind') }}</th>
          <th scope="col">{{ t('media.table.files') }}</th>
          <th scope="col" :title="t('media.table.usedHint')">{{ t('media.table.usedIn') }}</th>
          <th scope="col">{{ t('media.table.status') }}</th>
          <th scope="col" class="mt-actions-h">
            <span class="mt-sr">{{ t('media.table.actions') }}</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="item in items"
          :key="item.id"
          :class="[
            'mt-row',
            { 'mt-row--drop': dropRow === item.id, 'is-busy': busyId === item.id },
          ]"
          :data-testid="`media-row-${item.name}`"
          @dragover="onRowOver($event, item)"
          @dragleave="onRowLeave($event, item)"
          @drop="onDrop($event, item, null)"
        >
          <td class="mt-name-cell">
            <form v-if="editingId === item.id" class="mt-rename" @submit.prevent="saveRename(item)">
              <input
                ref="nameInput"
                v-model="draft"
                class="input input-sm"
                :aria-label="t('media.rename.label')"
                :aria-invalid="renameError !== null"
                maxlength="120"
                @keydown.esc.prevent="cancelRename"
              />
              <button
                type="submit"
                class="btn btn-ghost btn-icon btn-sm"
                :aria-label="t('media.rename.save')"
              >
                <Check :size="14" />
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-icon btn-sm"
                :aria-label="t('media.rename.cancel')"
                @click="cancelRename"
              >
                <X :size="14" />
              </button>
              <div v-if="renameError" class="mt-rename__err txt-xs" role="alert">
                {{ renameError }}
              </div>
            </form>
            <div v-else class="mt-name">
              <span class="fw-medium">{{ item.name }}</span>
              <button
                type="button"
                class="btn btn-ghost btn-icon btn-sm mt-name__edit"
                :aria-label="`${t('media.actions.rename')}: ${item.name}`"
                @click="startRename(item)"
              >
                <Pencil :size="13" />
              </button>
            </div>
            <div v-if="item.aliases.length > 0" class="txt-xs faint">
              {{ t('media.table.aliases', { list: item.aliases.join(', ') }) }}
            </div>
            <div class="txt-xs muted tabular">
              {{ d(new Date(item.updated_at), 'long') }}
              <template v-if="item.updated_by"> · {{ item.updated_by }}</template>
            </div>
          </td>
          <td><MediaThumb :item="item" :site="site ?? null" /></td>
          <td class="txt-sm">{{ t(`media.kind.${item.kind}`) }}</td>
          <td>
            <div class="mt-slots">
              <span
                v-for="s in mediaSlots(item)"
                :key="s.site ?? 'common'"
                :class="[
                  'mt-slot',
                  s.file ? 'mt-slot--on' : 'mt-slot--off',
                  { 'mt-slot--drop': dropSlot === `${item.id}:${s.site ?? ''}` },
                ]"
                @dragover="onSlotOver($event, item, s.site)"
                @dragleave="dropSlot = null"
                @drop="onDrop($event, item, s.site)"
              >
                <button
                  type="button"
                  class="mt-slot__main"
                  :title="s.file ? fileTitle(s.file) : undefined"
                  :aria-label="
                    s.file
                      ? t('media.actions.replace', { slot: slotLabel(s.site) })
                      : t('media.actions.uploadTo', { slot: slotLabel(s.site) })
                  "
                  @click="pick(item, s.site)"
                >
                  <Check v-if="s.file" :size="12" aria-hidden="true" />
                  <Plus v-else :size="12" aria-hidden="true" />
                  {{ slotLabel(s.site) }}
                </button>
                <button
                  v-if="s.file"
                  type="button"
                  class="mt-slot__x"
                  :aria-label="t('media.actions.removeFile', { slot: slotLabel(s.site) })"
                  @click="emit('removeFile', item, s.site)"
                >
                  <X :size="11" />
                </button>
              </span>
            </div>
            <div v-if="uncoveredSites(item).length > 0" class="mt-warn txt-xs">
              <TriangleAlert :size="12" aria-hidden="true" />
              {{
                t('media.table.uncovered', {
                  sites: uncoveredSites(item)
                    .map((s) => t(`common.site.${s}`))
                    .join(', '),
                })
              }}
            </div>
          </td>
          <td class="tabular txt-sm" :title="t('media.table.usedHint')">
            <span v-if="item.used_in > 0">{{ t('media.table.usedInN', item.used_in) }}</span>
            <span v-else class="faint">{{ t('media.table.unused') }}</span>
          </td>
          <td>
            <span :class="['badge', item.status === 'ready' ? 'solid-success' : 'solid-warning']">
              <span class="dot" />{{ t(`media.status.${item.status}`) }}
            </span>
          </td>
          <td class="mt-actions">
            <button
              type="button"
              class="btn btn-ghost btn-icon btn-sm"
              :disabled="!canDelete(item)"
              :title="canDelete(item) ? undefined : t('media.confirmDelete.inUse')"
              :aria-label="`${t('media.actions.delete')}: ${item.name}`"
              @click="emit('delete', item)"
            >
              <Trash2 :size="14" />
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.mt-table {
  overflow-x: auto;
}
.mt-hidden {
  display: none;
}
.mt-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
.mt-row td {
  vertical-align: middle;
  transition: background 0.12s;
}
.mt-row--drop td {
  background: var(--accent-soft);
}
.mt-row.is-busy {
  opacity: 0.55;
}
.mt-name-cell {
  min-width: 200px;
  max-width: 320px;
}
.mt-name {
  display: flex;
  align-items: center;
  gap: 2px;
}
.mt-name__edit {
  opacity: 0;
  transition: opacity 0.12s;
}
.mt-row:hover .mt-name__edit,
.mt-name__edit:focus-visible {
  opacity: 1;
}
.mt-rename {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px;
}
.mt-rename .input {
  flex: 1;
  min-width: 140px;
  height: 28px;
}
.mt-rename__err {
  flex-basis: 100%;
  color: var(--danger);
}
.mt-slots {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.mt-slot {
  display: inline-flex;
  align-items: center;
  height: 24px;
  border-radius: 999px;
  border: 1px solid var(--border);
  font-size: var(--text-xs);
  overflow: hidden;
}
.mt-slot--on {
  background: var(--success-soft);
  border-color: transparent;
  color: var(--success);
}
.mt-slot--off {
  border-style: dashed;
  color: var(--fg-subtle);
  background: var(--surface);
}
.mt-slot--drop {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
.mt-slot__main {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  height: 100%;
  padding: 0 8px;
  border: none;
  background: transparent;
  color: inherit;
  font: inherit;
  font-weight: 500;
  cursor: pointer;
}
.mt-slot--on .mt-slot__main {
  padding-right: 2px;
}
.mt-slot__main:hover {
  text-decoration: underline;
}
.mt-slot__x {
  display: inline-grid;
  place-items: center;
  width: 20px;
  height: 100%;
  border: none;
  background: transparent;
  color: inherit;
  cursor: pointer;
  opacity: 0.7;
}
.mt-slot__x:hover {
  opacity: 1;
}
.mt-warn {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 4px;
  color: var(--warning);
}
.mt-actions,
.mt-actions-h {
  width: 44px;
  text-align: right;
}
</style>
