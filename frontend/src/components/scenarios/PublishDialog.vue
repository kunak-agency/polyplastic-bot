<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleAlert, Minus, Pencil, Plus } from 'lucide-vue-next'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import { formatDayOffset } from '@/lib/calendar'
import type { PublishApply, PublishPreview, PublishRequest } from '@/types/api'

// Publishing (verdict §Q6, after n8n/Voiceflow): the diff of changed steps and
// what it does to people already on the scenario, in numbers. "Only new
// launches" is the default when buttons are removed or renamed; "for active" —
// when only texts change. Errors in the draft block publishing.
const props = defineProps<{
  nextVersion: number
  activeNewcomers: number
  busy: boolean
  preview: PublishPreview | null
  previewError: string | null
  loadingPreview: boolean
  errors: number
  warnings: number
}>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ submit: [body: PublishRequest] }>()
const { t } = useI18n()

const comment = ref('')
const apply = ref<PublishApply>('new')
const touched = ref(false)

watch(open, (v) => {
  if (!v) return
  comment.value = ''
  apply.value = 'new'
  touched.value = false
  showAdded.value = false
})
// The server's recommendation sets the default once the numbers arrive.
watch(
  () => props.preview,
  (p) => {
    if (p) apply.value = p.recommend
  },
)

const active = computed(() => props.preview?.active_newcomers ?? props.activeNewcomers)
const buttonsRemoved = computed(
  () => props.preview?.changes.some((c) => c.buttons_removed.length > 0) ?? false,
)

// The first version (or a big import) adds every step: dozens of "new step"
// lines say nothing, so added steps fold into one summary line.
const FOLD_ADDED_OVER = 10
const added = computed(() => props.preview?.changes.filter((c) => c.change === 'added') ?? [])
const others = computed(() => props.preview?.changes.filter((c) => c.change !== 'added') ?? [])
const foldAdded = computed(
  () => added.value.length > 0 && (props.nextVersion === 1 || added.value.length > FOLD_ADDED_OVER),
)
const addedBlocks = computed(
  () => new Set(added.value.map((c) => `${c.day_offset}|${c.time}`)).size,
)
const showAdded = ref(false)
const listed = computed(() =>
  !foldAdded.value || showAdded.value ? (props.preview?.changes ?? []) : others.value,
)

function submit(): void {
  touched.value = true
  if (props.errors > 0 || !comment.value.trim()) return
  emit('submit', { comment: comment.value.trim(), apply: apply.value })
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    width="wide"
    :title="t('scenarios.publish.title')"
    :description="t('scenarios.publish.description', { version: nextVersion })"
  >
    <form id="publish-form" class="publish" novalidate @submit.prevent="submit">
      <div v-if="errors > 0" class="publish__block">
        <CircleAlert :size="14" />{{ t('scenarios.publish.blocked', errors) }}
      </div>
      <div v-else-if="warnings > 0" class="publish__warn">
        {{ t('scenarios.publish.warnings', warnings) }}
      </div>

      <section class="publish__section">
        <div class="fw-semi txt-sm">{{ t('scenarios.publish.diff') }}</div>
        <p v-if="loadingPreview" class="muted txt-sm">
          {{ t('scenarios.publish.loadingPreview') }}
        </p>
        <p v-else-if="previewError" class="muted txt-sm">{{ previewError }}</p>
        <template v-else-if="preview">
          <p v-if="preview.changes.length === 0" class="muted txt-sm">
            {{ t('scenarios.publish.noChanges') }}
          </p>
          <div v-if="foldAdded" class="diff__summary txt-sm">
            <Plus :size="12" />
            <span
              >{{ t('scenarios.publish.addedSteps', added.length) }}
              {{ t('scenarios.publish.inBlocks', addedBlocks) }}</span
            >
            <button
              type="button"
              class="diff__toggle"
              :aria-expanded="showAdded"
              @click="showAdded = !showAdded"
            >
              {{ showAdded ? t('scenarios.publish.hideAll') : t('scenarios.publish.showAll') }}
            </button>
          </div>
          <ul v-if="listed.length > 0" class="diff">
            <li v-for="c in listed" :key="c.step_id" :class="`diff--${c.change}`">
              <Plus v-if="c.change === 'added'" :size="12" />
              <Minus v-else-if="c.change === 'removed'" :size="12" />
              <Pencil v-else :size="12" />
              <span class="tabular diff__when"
                >{{ formatDayOffset(c.day_offset) }} {{ c.time }}</span
              >
              <span class="diff__title">{{ c.title || t('scenarios.editor.untitled') }}</span>
              <span class="diff__fields">
                <template v-if="c.change === 'changed'">
                  {{ c.fields.map((f) => t(`scenarios.publish.fields.${f}`)).join(', ') }}
                </template>
                <template v-else>{{ t(`scenarios.publish.change.${c.change}`) }}</template>
              </span>
              <span v-if="c.buttons_removed.length > 0" class="diff__removed">
                {{
                  t('scenarios.publish.buttonsRemoved', { labels: c.buttons_removed.join(', ') })
                }}
              </span>
            </li>
          </ul>
        </template>
      </section>

      <section v-if="preview" class="publish__nums">
        <div class="num">
          <span class="num__v tabular">{{ preview.active_newcomers }}</span>
          <span class="num__l">{{ t('scenarios.publish.numActive') }}</span>
        </div>
        <div class="num">
          <span class="num__v tabular">{{ preview.inside_survey }}</span>
          <span class="num__l">{{ t('scenarios.publish.numSurvey') }}</span>
        </div>
        <div class="num">
          <span class="num__v tabular">{{ preview.removed_branches_live }}</span>
          <span class="num__l">{{ t('scenarios.publish.numBranches') }}</span>
        </div>
      </section>
      <p v-if="preview" class="muted txt-sm">
        {{
          t('scenarios.publish.consequences', {
            n: preview.active_newcomers,
            m: preview.inside_survey,
            k: preview.removed_branches_live,
          })
        }}
      </p>

      <FormField :label="t('scenarios.publish.apply')">
        <div class="publish__radios" role="radiogroup" :aria-label="t('scenarios.publish.apply')">
          <label class="publish__radio">
            <input v-model="apply" type="radio" name="pub-apply" value="new" />
            {{ t('scenarios.publish.applyNew') }}
            <span v-if="preview?.recommend === 'new'" class="badge solid-info">
              {{ t('scenarios.publish.recommended') }}
            </span>
          </label>
          <label class="publish__radio">
            <input v-model="apply" type="radio" name="pub-apply" value="active" />
            {{
              t('scenarios.publish.applyActive', {
                n: t('scenarios.publish.activeCount', active),
              })
            }}
            <span v-if="preview?.recommend === 'active'" class="badge solid-info">
              {{ t('scenarios.publish.recommended') }}
            </span>
          </label>
        </div>
        <span v-if="buttonsRemoved" class="field-hint">{{ t('scenarios.publish.whyNew') }}</span>
        <span v-else class="field-hint">{{ t('scenarios.publish.snapshotHint') }}</span>
      </FormField>

      <FormField
        :label="t('scenarios.publish.comment')"
        html-for="pub-comment"
        required
        :error="touched && !comment.trim() ? t('scenarios.publish.commentRequired') : undefined"
      >
        <textarea
          id="pub-comment"
          v-model="comment"
          class="textarea publish__comment"
          :class="{ err: touched && !comment.trim() }"
          :placeholder="t('scenarios.publish.commentPlaceholder')"
        />
      </FormField>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="publish-form" variant="primary" :disabled="busy || errors > 0">
        {{ busy ? t('scenarios.publish.submitting') : t('scenarios.publish.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.diff__summary {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--success);
}
.diff__summary span {
  color: var(--fg);
}
.diff__toggle {
  margin-left: auto;
  border: 0;
  background: none;
  padding: 0;
  color: var(--accent);
  font-size: var(--text-sm);
  cursor: pointer;
}
.diff__toggle:hover {
  text-decoration: underline;
}
.publish {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.publish__block,
.publish__warn {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 8px 10px;
  border-radius: var(--radius-md);
  font-size: var(--text-sm);
}
.publish__block {
  background: var(--danger-soft);
  color: var(--danger);
}
.publish__warn {
  background: var(--warning-soft);
}
.publish__section {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.diff {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 220px;
  overflow-y: auto;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
}
.diff li {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 8px;
  align-items: center;
  padding: 6px 10px;
  font-size: var(--text-sm);
  border-bottom: 1px solid var(--border-subtle);
}
.diff li:last-child {
  border-bottom: none;
}
.diff--added {
  color: var(--success);
}
.diff--removed {
  color: var(--danger);
}
.diff__when {
  color: var(--fg-muted);
  min-width: 80px;
}
.diff__title {
  color: var(--fg);
  font-weight: 500;
}
.diff__fields {
  color: var(--fg-muted);
}
.diff__removed {
  flex-basis: 100%;
  padding-left: 20px;
  color: var(--danger);
}
.publish__nums {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}
.num {
  display: flex;
  flex-direction: column;
  padding: 8px 10px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
}
.num__v {
  font-size: var(--text-xl);
  font-weight: 600;
}
.num__l {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.publish__comment {
  height: 72px;
  padding: 8px 10px;
  resize: vertical;
}
.publish__radios {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.publish__radio {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  cursor: pointer;
}
</style>
