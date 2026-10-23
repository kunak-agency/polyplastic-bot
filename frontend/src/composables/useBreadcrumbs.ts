import { onBeforeUnmount, readonly, ref } from 'vue'

// The last crumb of a detail screen is data, not a route label: the route name
// says nothing the H1 does not, while the field's own name places the user.
// Routes carry the fixed part (router/index.ts, meta.crumbs); the page sets the
// tail here and the topbar reads it.
//
// A module-level ref rather than a store: this is one string owned by whichever
// screen is mounted, and it must not survive that screen.
const tail = ref<string | null>(null)

export const breadcrumbTail = readonly(tail)

export function useBreadcrumbTail() {
  onBeforeUnmount(() => {
    tail.value = null
  })
  return {
    setBreadcrumbTail(value: string | null): void {
      tail.value = value
    },
  }
}
