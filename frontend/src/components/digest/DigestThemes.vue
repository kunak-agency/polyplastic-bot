<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { EyeOff, Lock } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Digest, DigestTheme } from '@/types/api'
import { signed, themeBars } from '@/lib/digest'

// Section 4 "themes of the week": clusters the code built from flag codes,
// low-score reasons and free texts. A theme shows only from min_group people;
// examples are anonymised and only for themes of that size.
const props = defineProps<{ digest: Digest }>()
const { t } = useI18n()

const bars = computed(() => themeBars(props.digest.themes))

function dynamics(th: DigestTheme): string {
  const n = props.digest.min_group
  if (th.prev_hidden) return t('analytics.themes.prevHidden', { n })
  if (th.prev_count === 0) return t('analytics.themes.new')
  if (th.delta === null) return ''
  if (th.delta === 0) return t('analytics.themes.same')
  return t('analytics.themes.delta', { delta: signed(th.delta) })
}
</script>

<template>
  <SectionCard>
    <template #title>{{ t('analytics.themes.title') }}</template>
    <template #desc>{{ t('analytics.themes.desc', { n: digest.min_group }) }}</template>

    <p v-if="digest.themes.length === 0" class="muted">
      {{ t('analytics.themes.none', { n: digest.min_group }) }}
    </p>
    <ol v-else class="themes">
      <li v-for="(th, i) in digest.themes" :key="th.key" class="theme">
        <div class="theme__head">
          <span class="theme__label">{{ th.label }}</span>
          <span class="theme__count">{{
            t('analytics.themes.newcomers', { n: th.newcomers }, th.newcomers)
          }}</span>
        </div>
        <div class="bar" aria-hidden="true"><span :style="{ width: `${bars[i]}%` }" /></div>
        <div class="theme__meta txt-sm muted">
          <span v-if="th.category" class="badge">{{ th.category }}</span>
          <span>{{ th.sites.join(', ') }}</span>
          <span>· {{ t('analytics.themes.mentions', { n: th.mentions }) }}</span>
          <span>
            ·
            {{
              th.owner
                ? t('analytics.themes.owner', { owner: th.owner })
                : t('analytics.themes.noOwner')
            }}
          </span>
          <span
            v-if="dynamics(th)"
            :class="['dyn', { 'dyn--up': (th.delta ?? 0) > 0 && !th.prev_hidden }]"
            >· {{ dynamics(th) }}</span
          >
        </div>
        <p v-if="th.keywords.length" class="txt-sm theme__kw">
          {{ t('analytics.themes.keywords', { words: th.keywords.join(', ') }) }}
        </p>
        <details v-if="th.examples.length" class="theme__ex txt-sm">
          <summary>{{ t('analytics.themes.examples') }}</summary>
          <ul>
            <li v-for="(ex, k) in th.examples" :key="k">«{{ ex }}»</li>
          </ul>
        </details>
      </li>
    </ol>

    <p v-if="digest.hidden_themes > 0" class="note txt-sm muted">
      <EyeOff :size="14" />
      {{ t('analytics.themes.hidden', { n: digest.min_group, count: digest.hidden_themes }) }}
    </p>
    <p v-if="digest.confidential.present" class="note txt-sm muted">
      <Lock :size="14" />
      <template v-if="digest.confidential.count !== null">{{
        t('analytics.themes.confidential', { n: digest.confidential.count })
      }}</template>
      <template v-else>{{
        t('analytics.themes.confidentialAny', { n: digest.min_group })
      }}</template>
    </p>
    <div v-if="digest.text.themes" class="llm txt-sm">
      <div class="fw-semi">{{ t('analytics.themes.llmText') }}</div>
      <p>{{ digest.text.themes }}</p>
    </div>
  </SectionCard>
</template>

<style scoped>
.themes {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.theme__head {
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
  align-items: baseline;
}
.theme__label {
  font-weight: 600;
}
.theme__label::first-letter {
  text-transform: uppercase;
}
.theme__count {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.bar {
  height: 6px;
  margin: 6px 0;
  border-radius: 999px;
  background: var(--bg-muted);
  overflow: hidden;
}
.bar span {
  display: block;
  height: 100%;
  border-radius: 999px;
  background: var(--accent);
}
.theme__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 6px;
  align-items: center;
}
.dyn--up {
  color: var(--danger);
}
.theme__kw {
  margin: 6px 0 0;
}
.theme__ex {
  margin-top: 6px;
}
.theme__ex summary {
  cursor: pointer;
  color: var(--fg-muted);
}
.theme__ex ul {
  margin: 6px 0 0;
  padding-left: 18px;
  font-style: italic;
}
.note {
  display: flex;
  gap: 8px;
  align-items: center;
  margin: var(--space-4) 0 0;
}
.llm {
  margin-top: var(--space-4);
  padding: var(--space-3);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
}
.llm p {
  margin: 4px 0 0;
}
</style>
