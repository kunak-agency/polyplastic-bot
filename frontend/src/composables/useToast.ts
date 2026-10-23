import { toast } from 'vue-sonner'

export interface ToastOptions {
  description?: string
  duration?: number
}

// Thin wrapper over vue-sonner so components never import the toast library
// directly — swapping the implementation stays a one-file change.
export function useToast() {
  return {
    success: (message: string, options?: ToastOptions) => toast.success(message, options),
    error: (message: string, options?: ToastOptions) => toast.error(message, options),
    info: (message: string, options?: ToastOptions) => toast.info(message, options),
    warning: (message: string, options?: ToastOptions) => toast.warning(message, options),
  }
}
