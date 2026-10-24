<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { LogOut } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'

// Who is signed in and "Sign out", at the foot of the sidebar. Absent while auth is off on
// the server. Collapsed rail: initials only, sign-out on click.
const props = defineProps<{ collapsed?: boolean }>()
const { t } = useI18n()
const router = useRouter()
const toast = useToast()
const auth = useAuthStore()
const { user } = storeToRefs(auth)
const busy = ref(false)
const initials = computed(() =>
  (user.value?.full_name ?? '')
    .replace(/\(.*?\)/g, '')
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((w) => w[0] ?? '')
    .join('')
    .toUpperCase(),
)

async function logout(): Promise<void> {
  busy.value = true
  try {
    await auth.logout()
  } catch (e) {
    // The local session is dropped anyway; say the server did not confirm.
    toast.error(errorMessage(e, 'auth.logout.failed'))
  } finally {
    busy.value = false
    void router.replace({ name: 'login' })
  }
}
</script>

<template>
  <div v-if="user" :class="['user-menu', { 'user-menu--rail': props.collapsed }]">
    <span class="user-menu__avatar" :title="props.collapsed ? user.full_name : undefined">{{
      initials
    }}</span>
    <div v-if="!props.collapsed" class="user-menu__who">
      <span class="user-menu__name">{{ user.full_name }}</span>
      <span class="user-menu__role">{{ t(`auth.roles.${user.role}`) }}</span>
    </div>
    <button
      type="button"
      class="btn btn-ghost btn-icon btn-sm user-menu__out"
      :disabled="busy"
      :title="t('auth.logout.button')"
      :aria-label="t('auth.logout.button')"
      @click="logout"
    >
      <LogOut :size="16" aria-hidden="true" />
    </button>
  </div>
</template>

<style scoped>
.user-menu {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  padding: 4px 4px 4px 8px;
}
.user-menu--rail {
  flex-direction: column;
  gap: 4px;
  padding: 4px 0;
}
.user-menu__avatar {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--bg-muted);
  color: var(--fg);
  font-size: 11px;
  font-weight: 600;
}
.user-menu__who {
  display: flex;
  flex-direction: column;
  min-width: 0;
  line-height: 1.25;
}
.user-menu__name {
  overflow: hidden;
  font-size: var(--text-sm);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.user-menu__role {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.user-menu__out {
  margin-left: auto;
  color: var(--fg-muted);
}
.user-menu--rail .user-menu__out {
  margin-left: 0;
}
</style>
