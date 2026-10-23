<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import MediaField from '@/components/scenarios/MediaField.vue'
import type { BranchAction, EscalateRoute, HandoffTo } from '@/types/api'

// One action of a branch, edited with selects only (verdict §Q2: a closed list,
// no "variables" in the UI).
const props = defineProps<{ action: BranchAction; mediaNames: string[]; invalid?: boolean }>()
const emit = defineEmits<{ update: [action: BranchAction]; remove: [] }>()
const { t } = useI18n()

const CATEGORIES = ['F0', 'F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9', 'F10']
const ROUTES: EscalateRoute[] = ['hr', 'recruiter', 'security']
const PEOPLE: HandoffTo[] = ['recruiter', 'hr', 'security']

function set(p: Partial<BranchAction>): void {
  emit('update', { ...props.action, ...p })
}
function val(e: Event): string {
  return (e.target as HTMLInputElement | HTMLSelectElement).value
}
function checked(e: Event): boolean {
  return (e.target as HTMLInputElement).checked
}
</script>

<template>
  <div :class="['act', { 'act--err': invalid }]">
    <div class="act__head">
      <span class="act__type">{{ t(`scenarios.action.type.${action.type}`) }}</span>
      <Button
        variant="ghost"
        size="icon-sm"
        :aria-label="t('scenarios.action.remove')"
        @click="emit('remove')"
      >
        <X :size="14" />
      </Button>
    </div>

    <div v-if="action.type === 'escalate'" class="act__fields">
      <label class="act__field">
        <span>{{ t('scenarios.action.category') }}</span>
        <select
          class="input"
          :value="action.category ?? ''"
          @change="set({ category: val($event) })"
        >
          <option v-for="c in CATEGORIES" :key="c" :value="c">
            {{ c }} · {{ t(`tickets.category.${c}`) }}
          </option>
        </select>
      </label>
      <label class="act__field">
        <span>{{ t('scenarios.action.route') }}</span>
        <select
          class="input"
          :value="action.route ?? ''"
          @change="set({ route: val($event) as EscalateRoute })"
        >
          <option v-for="r in ROUTES" :key="r" :value="r">{{ t(`tickets.route.${r}`) }}</option>
        </select>
      </label>
      <label class="check">
        <input
          type="checkbox"
          :checked="!!action.notify_manager"
          @change="set({ notify_manager: checked($event) })"
        />
        {{ t('scenarios.action.notifyManager') }}
      </label>
    </div>

    <div v-else-if="action.type === 'handoff'" class="act__fields">
      <label class="act__field">
        <span>{{ t('scenarios.action.to') }}</span>
        <select
          :class="['input', { err: !action.to }]"
          :value="action.to ?? ''"
          @change="set({ to: val($event) as HandoffTo })"
        >
          <option value="">{{ t('scenarios.action.toNone') }}</option>
          <option v-for="p in PEOPLE" :key="p" :value="p">
            {{ t(`scenarios.action.people.${p}`) }}
          </option>
        </select>
      </label>
    </div>

    <div v-else-if="action.type === 'set_field'" class="act__fields">
      <label class="act__field">
        <span>{{ t('scenarios.action.addressForm') }}</span>
        <select
          class="input"
          :value="action.value ?? 'vy'"
          @change="set({ field: 'address_form', value: val($event) })"
        >
          <option value="ty">{{ t('scenarios.action.ty') }}</option>
          <option value="vy">{{ t('scenarios.action.vy') }}</option>
        </select>
      </label>
    </div>

    <div v-else-if="action.type === 'set_flag'" class="act__fields">
      <label class="act__field">
        <span>{{ t('scenarios.action.flag') }}</span>
        <input
          class="input mono"
          :value="action.flag ?? ''"
          :placeholder="t('scenarios.action.flagPlaceholder')"
          @input="set({ flag: val($event) })"
        />
      </label>
    </div>

    <div v-else-if="action.type === 'send_file'" class="act__fields">
      <div class="act__field">
        <span>{{ t('scenarios.action.file') }}</span>
        <MediaField
          mode="single"
          :model-value="action.media ? [{ name: action.media }] : []"
          :names="mediaNames"
          @update:model-value="set({ media: $event[0]?.name ?? '' })"
        />
      </div>
      <label class="check">
        <input
          type="checkbox"
          :checked="!!action.per_tag"
          @change="set({ per_tag: checked($event) })"
        />
        {{ t('scenarios.action.perTag') }}
      </label>
    </div>

    <div v-else-if="action.type === 'open_free_text'" class="act__fields">
      <label class="act__field">
        <span>{{ t('scenarios.action.prompt') }}</span>
        <input
          class="input"
          :value="action.prompt ?? ''"
          :placeholder="t('scenarios.action.promptPlaceholder')"
          @input="set({ prompt: val($event) })"
        />
      </label>
    </div>
  </div>
</template>

<style scoped>
.act {
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: 6px 8px 8px;
  background: var(--bg-subtle);
}
.act--err {
  border-color: var(--danger);
}
.act__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.act__type {
  font-size: var(--text-sm);
  font-weight: 600;
}
.act__fields {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.act__field {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-sm);
  cursor: pointer;
}
.err {
  border-color: var(--danger);
}
</style>
