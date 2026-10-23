import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import type { Density, Theme } from '@/types/ui'

const DENSITY_KEY = 'pp.density'

function readStored<T extends string>(key: string, fallback: T): T {
  try {
    return (localStorage.getItem(key) as T) || fallback
  } catch {
    return fallback
  }
}

export const useThemeStore = defineStore('theme', () => {
  // The product is light-only: a projector on defence day washes out a dark
  // ground, and severity tints read wrong on it.
  //
  // Pinned rather than merely untoggleable: the stored key is deliberately
  // ignored. Anyone who switched to dark before the toggle went away would
  // otherwise stay there forever, with nothing left on screen to switch back.
  const theme = ref<Theme>('light')
  const density = ref<Density>(readStored<Density>(DENSITY_KEY, 'regular'))

  // The accent is pinned in the same way and for the same reason as the theme:
  // it is black (styles/tokens.css), and colour is reserved for status, not for
  // chrome. A stale pp.accent in someone's storage would otherwise repaint every
  // primary button with nothing on screen to undo it.
  function apply() {
    document.documentElement.setAttribute('data-theme', theme.value)
    document.documentElement.setAttribute('data-density', density.value)
    document.documentElement.style.removeProperty('--accent')
    document.documentElement.style.removeProperty('--accent-hover')
    document.documentElement.style.removeProperty('--accent-active')
  }

  apply()

  watch(density, (v) => {
    try {
      localStorage.setItem(DENSITY_KEY, v)
    } catch {}
    apply()
  })
  return { theme, density }
})
