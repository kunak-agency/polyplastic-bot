<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useId } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { usePeopleStore } from '@/stores/people'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import { exactPerson, filterPeople } from '@/lib/people'
import type { Person } from '@/types/api'

// Manager or mentor from the people directory (api_contract_auth.md, "People"):
// a combobox with search and a "+ New ..." item that opens a small form; the
// created person is picked at once. v-model is the person id ('' — none).
const props = withDefaults(
  defineProps<{
    role: 'manager' | 'mentor'
    // Shown while the id is not in the directory (an old record by name).
    name?: string
    inputId?: string
    invalid?: boolean
    disabled?: boolean
    clearable?: boolean
  }>(),
  { name: '', inputId: undefined, invalid: false, disabled: false, clearable: false },
)
const model = defineModel<string>({ required: true })
const emit = defineEmits<{ pick: [person: Person | null] }>()

const { t } = useI18n()
const toast = useToast()
const people = usePeopleStore()
const { byRole } = storeToRefs(people)
const uid = useId()
const listId = `${uid}-list`

const loadFailed = ref(false)
onMounted(() => {
  people.load(props.role).catch(() => (loadFailed.value = true))
})

const all = computed(() => byRole.value[props.role])
const selected = computed(() => all.value.find((p) => p.id === model.value) ?? null)
// An old record has only the name: it is shown until a person is picked.
const selectedName = computed(() => selected.value?.full_name ?? props.name)

const open = ref(false)
const query = ref('')
const active = ref(0)
const options = computed(() => filterPeople(all.value, query.value, model.value))

// Rows of the list: people, then "none" (clearable) and "+ new".
type Row = { kind: 'person'; person: Person } | { kind: 'none' } | { kind: 'new' }
const rows = computed<Row[]>(() => [
  ...options.value.map((person) => ({ kind: 'person' as const, person })),
  ...(props.clearable && model.value ? [{ kind: 'none' as const }] : []),
  { kind: 'new' as const },
])

const text = computed({
  get: () => (open.value ? query.value : selectedName.value),
  set: (v: string) => {
    query.value = v
    open.value = true
    active.value = 0
  },
})

function onFocus(): void {
  if (props.disabled || creating.value) return
  query.value = ''
  active.value = 0
  open.value = true
}

function close(): void {
  open.value = false
  query.value = ''
}

function choose(p: Person | null): void {
  model.value = p?.id ?? ''
  emit('pick', p)
  close()
}

function pickRow(r: Row): void {
  if (r.kind === 'person') choose(r.person)
  else if (r.kind === 'none') choose(null)
  else startCreate()
}

function onKey(e: KeyboardEvent): void {
  if (!open.value && (e.key === 'ArrowDown' || e.key === 'Enter')) {
    onFocus()
    e.preventDefault()
    return
  }
  if (!open.value) return
  if (e.key === 'ArrowDown') active.value = Math.min(active.value + 1, rows.value.length - 1)
  else if (e.key === 'ArrowUp') active.value = Math.max(active.value - 1, 0)
  else if (e.key === 'Enter') {
    const r = rows.value[active.value]
    if (r) pickRow(r)
  } else if (e.key === 'Escape') close()
  else return
  e.preventDefault()
}

// ── "+ New manager / mentor" ────────────────────────────────────────────────
const creating = ref(false)
const newName = ref('')
const busy = ref(false)
const createError = ref('')
const nameInput = ref<HTMLInputElement | null>(null)
const duplicate = computed(() => exactPerson(all.value, newName.value))

function startCreate(): void {
  newName.value = query.value.trim()
  createError.value = ''
  close()
  creating.value = true
  void nextTick(() => nameInput.value?.focus())
}

function cancelCreate(): void {
  creating.value = false
  newName.value = ''
}

async function create(): Promise<void> {
  const full_name = newName.value.trim().replace(/\s+/g, ' ')
  if (!full_name) {
    createError.value = t('people.picker.required')
    return
  }
  busy.value = true
  try {
    const p = await people.create({ full_name, role: props.role })
    toast.success(t('people.picker.created', { name: p.full_name }))
    creating.value = false
    newName.value = ''
    choose(p)
  } catch (e) {
    createError.value = errorMessage(e, 'people.picker.failed')
  } finally {
    busy.value = false
  }
}

function pickDuplicate(): void {
  if (!duplicate.value) return
  creating.value = false
  choose(duplicate.value)
}
</script>

<template>
  <div class="picker">
    <div v-if="!creating" class="picker__box">
      <input
        :id="inputId"
        v-model="text"
        class="input"
        :class="{ err: invalid }"
        role="combobox"
        autocomplete="off"
        :aria-expanded="open"
        :aria-controls="listId"
        aria-autocomplete="list"
        :aria-activedescendant="open ? `${listId}-${active}` : undefined"
        :disabled="disabled"
        :placeholder="t(`people.picker.placeholder.${role}`)"
        @focus="onFocus"
        @click="!open && onFocus()"
        @blur="close"
        @keydown="onKey"
      />
      <ul v-if="open" :id="listId" class="picker__list" role="listbox">
        <li
          v-for="(r, i) in rows"
          :id="`${listId}-${i}`"
          :key="r.kind === 'person' ? r.person.id : r.kind"
          role="option"
          :aria-selected="r.kind === 'person' && r.person.id === model"
          :class="[
            'picker__opt',
            {
              'picker__opt--active': i === active,
              'picker__opt--new': r.kind === 'new',
              'picker__opt--off': r.kind === 'person' && !r.person.active,
            },
          ]"
          @mousedown.prevent="pickRow(r)"
          @mouseenter="active = i"
        >
          <template v-if="r.kind === 'person'">{{ r.person.full_name }}</template>
          <template v-else-if="r.kind === 'none'">{{ t('people.picker.none') }}</template>
          <template v-else>{{ t(`people.picker.new.${role}`) }}</template>
        </li>
        <li v-if="options.length === 0" class="picker__empty" aria-disabled="true">
          {{ loadFailed ? t('people.picker.loadError') : t('people.picker.empty') }}
        </li>
      </ul>
    </div>

    <div v-else class="picker__new" role="group" :aria-label="t(`people.picker.new.${role}`)">
      <input
        ref="nameInput"
        v-model="newName"
        class="input"
        :class="{ err: createError }"
        autocomplete="off"
        :aria-label="t('people.picker.fullName')"
        :placeholder="t('people.picker.fullNamePlaceholder')"
        @keydown.enter.prevent="create"
        @keydown.esc.prevent="cancelCreate"
      />
      <p v-if="duplicate" class="txt-xs muted picker__dup">
        {{ t('people.picker.exists', { name: duplicate.full_name }) }}
        <button type="button" class="link-btn" @click="pickDuplicate">
          {{ t('people.picker.pickExisting') }}
        </button>
      </p>
      <span v-if="createError" class="field-err">{{ createError }}</span>
      <div class="picker__actions">
        <Button type="button" size="sm" variant="primary" :disabled="busy" @click="create">
          {{ busy ? t('people.picker.creating') : t('people.picker.create') }}
        </Button>
        <Button type="button" size="sm" variant="ghost" :disabled="busy" @click="cancelCreate">
          {{ t('people.picker.cancel') }}
        </Button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.picker,
.picker__box {
  position: relative;
}
.picker__list {
  position: absolute;
  z-index: 30;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 240px;
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg, 8px);
  box-shadow: var(--shadow-md, 0 8px 24px rgb(0 0 0 / 0.12));
}
.picker__opt {
  padding: 6px 8px;
  border-radius: var(--radius-sm, 4px);
  cursor: pointer;
}
.picker__opt--active {
  background: var(--surface-hover, var(--border-subtle));
}
.picker__opt--new {
  color: var(--accent, var(--fg));
  font-weight: 500;
}
.picker__opt--off {
  color: var(--fg-subtle);
}
.picker__empty {
  padding: 6px 8px;
  color: var(--fg-subtle);
  font-size: var(--text-sm);
}
.picker__new {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.picker__actions {
  display: flex;
  gap: 8px;
}
.picker__dup {
  margin: 0;
}
.link-btn {
  padding: 0;
  border: 0;
  background: none;
  color: var(--accent, var(--fg));
  text-decoration: underline;
  cursor: pointer;
  font: inherit;
}
</style>
