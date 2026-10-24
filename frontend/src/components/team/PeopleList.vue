<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { UserPlus, UsersRound } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import PersonDialog from '@/components/team/PersonDialog.vue'
import { Button } from '@/components/ui/button'
import { apiListUsers, apiPatchUser } from '@/api/team'
import { usePeopleStore } from '@/stores/people'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import type { User } from '@/types/api'

// Managers or mentors of the people directory in Settings (hr_lead): who can
// sign in, "grant sign-in" to a manager, add, deactivate.
const props = defineProps<{ role: 'manager' | 'mentor' }>()
const { t } = useI18n()
const toast = useToast()
const people = usePeopleStore()

const items = ref<User[] | null>(null)
const error = ref<unknown>(null)
const loading = ref(false)
const busyId = ref<string | null>(null)
const dialogOpen = ref(false)
const dialogMode = ref<'create' | 'grant'>('create')
const dialogPerson = ref<User | null>(null)

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    items.value = await apiListUsers({ role: props.role })
  } catch (e) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(
  () => props.role,
  () => {
    items.value = null
    void load()
  },
)

function openCreate(): void {
  dialogMode.value = 'create'
  dialogPerson.value = null
  dialogOpen.value = true
}

function openGrant(u: User): void {
  dialogMode.value = 'grant'
  dialogPerson.value = u
  dialogOpen.value = true
}

function onDone(): void {
  void people.load(props.role, true).catch(() => undefined)
  void load()
}

async function toggleActive(u: User): Promise<void> {
  const on = u.active !== false
  if (on && !window.confirm(t('team.deactivate.confirm', { name: u.full_name }))) return
  busyId.value = u.id
  try {
    await apiPatchUser(u.id, { active: !on })
    toast.success(on ? t('team.deactivate.done') : t('team.activate.done'))
    onDone()
  } catch (e) {
    toast.error(errorMessage(e, 'team.deactivate.failed'))
  } finally {
    busyId.value = null
  }
}
</script>

<template>
  <div class="people-list">
    <div class="people-list__head">
      <Button variant="primary" @click="openCreate">
        <UserPlus :size="14" aria-hidden="true" />{{ t(`people.users.add.${role}`) }}
      </Button>
    </div>

    <SectionCard v-if="!items && !error">
      <LoadingState variant="table" :rows="4" />
    </SectionCard>
    <SectionCard v-else-if="!items">
      <ErrorState :message="errorMessage(error, 'people.users.loadError')">
        <template #retry>
          <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>
    <SectionCard v-else body-padding="none" :class="{ 'is-stale': loading }">
      <EmptyState
        v-if="items.length === 0"
        :icon="UsersRound"
        :title="t(`people.users.empty.${role}`)"
      />
      <div v-else class="tbl-scroll">
        <table class="tbl">
          <thead>
            <tr>
              <th>{{ t('people.users.columns.name') }}</th>
              <th>{{ t('people.users.columns.login') }}</th>
              <th>{{ t('people.users.columns.status') }}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in items" :key="u.id" :class="{ 'row-off': u.active === false }">
              <td class="fw-medium">{{ u.full_name }}</td>
              <td>
                <span v-if="role === 'mentor'" class="muted txt-sm">
                  {{ t('people.users.mentorNoLogin') }}
                </span>
                <span v-else-if="u.has_login" class="badge solid-success">
                  {{ t('people.users.login.yes', { login: u.login }) }}
                </span>
                <span v-else class="badge">{{ t('people.users.login.no') }}</span>
              </td>
              <td>
                <span :class="['badge', u.active !== false ? 'solid-success' : '']">
                  {{ u.active !== false ? t('team.status.active') : t('team.status.inactive') }}
                </span>
              </td>
              <td class="td-actions">
                <Button
                  v-if="role === 'manager' && !u.has_login && u.active !== false"
                  size="sm"
                  variant="outline"
                  @click="openGrant(u)"
                >
                  {{ t('people.users.grant.button') }}
                </Button>
                <Button
                  size="sm"
                  :variant="u.active !== false ? 'ghost' : 'outline'"
                  :disabled="busyId === u.id"
                  @click="toggleActive(u)"
                >
                  {{ u.active !== false ? t('team.deactivate.button') : t('team.activate.button') }}
                </Button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>

    <PersonDialog
      v-model:open="dialogOpen"
      :mode="dialogMode"
      :role="role"
      :person="dialogPerson"
      @done="onDone"
    />
  </div>
</template>

<style scoped>
.people-list__head {
  display: flex;
  justify-content: flex-end;
  margin-bottom: var(--space-3);
}
.tbl-scroll {
  overflow-x: auto;
}
.row-off {
  opacity: 0.55;
}
.td-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.is-stale {
  opacity: 0.6;
}
</style>
