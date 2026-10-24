<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { LogOut } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'

// Who is signed in and "Sign out". Absent while auth is off on the server.
const { t } = useI18n()
const router = useRouter()
const toast = useToast()
const auth = useAuthStore()
const { user } = storeToRefs(auth)
const busy = ref(false)

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
  <div v-if="user" class="user-menu">
    <div class="user-menu__who">
      <span class="user-menu__name">{{ user.full_name }}</span>
      <span class="user-menu__role">{{ t(`auth.roles.${user.role}`) }}</span>
    </div>
    <button
      type="button"
      class="btn btn-ghost btn-sm"
      :disabled="busy"
      :title="t('auth.logout.button')"
      @click="logout"
    >
      <LogOut :size="14" aria-hidden="true" />{{ t('auth.logout.button') }}
    </button>
  </div>
</template>

<style scoped>
.user-menu {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding-left: var(--space-3);
  border-left: 1px solid var(--border-subtle);
}
.user-menu__who {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
  text-align: right;
}
.user-menu__name {
  font-size: var(--text-sm);
  font-weight: 500;
}
.user-menu__role {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
</style>
