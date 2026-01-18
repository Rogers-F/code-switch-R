type ToastType = 'success' | 'error' | 'warning'

const TOAST_DURATION = 3200

let toastContainer: HTMLElement | null = null

function getContainer() {
  if (toastContainer) return toastContainer

  toastContainer = document.createElement('div')
  toastContainer.className = 'mac-toast-container'
  toastContainer.setAttribute('aria-live', 'polite')
  document.body.appendChild(toastContainer)
  return toastContainer
}

export function showToast(message: string, type: ToastType = 'success') {
  if (!message) return

  const container = getContainer()
  const toast = document.createElement('div')
  toast.className = `mac-toast mac-toast-${type}`
  toast.setAttribute('role', 'status')

  const icon = document.createElement('span')
  icon.className = 'mac-toast-icon'
  icon.setAttribute('aria-hidden', 'true')

  const iconSvg = (() => {
    if (type === 'success') {
      return '<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M20 6L9 17l-5-5" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg>'
    }
    if (type === 'warning') {
      return '<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M12 9v4m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>'
    }
    return '<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M12 9v4m0 4h.01M12 2a10 10 0 1010 10A10 10 0 0012 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>'
  })()

  icon.innerHTML = iconSvg

  const text = document.createElement('span')
  text.className = 'mac-toast-text'
  text.textContent = message

  toast.appendChild(icon)
  toast.appendChild(text)

  container.appendChild(toast)

  requestAnimationFrame(() => {
    toast.classList.add('mac-toast-visible')
  })

  const remove = () => {
    toast.classList.remove('mac-toast-visible')
    toast.classList.add('mac-toast-hide')
    const handler = () => {
      toast.removeEventListener('transitionend', handler)
      toast.remove()
      if (toastContainer && toastContainer.childElementCount === 0) {
        toastContainer.remove()
        toastContainer = null
      }
    }
    toast.addEventListener('transitionend', handler)
  }

  setTimeout(remove, TOAST_DURATION)
}
