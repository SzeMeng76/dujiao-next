import { getImageUrl } from './image'

export function processHtmlForDisplay(html: string): string {
  if (!html) return ''

  return html.replace(/\b(src|poster)=["'](\/uploads\/.*?)["']/g, (_, attr, path) => {
    return `${attr}="${getImageUrl(path)}"`
  })
}

export function processHtmlForStorage(html: string): string {
  if (!html) return ''

  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL || ''
  let apiHost = ''
  try {
    if (apiBaseUrl) {
      apiHost = new URL(apiBaseUrl).host
    } else {
      apiHost = window.location.host
    }
  } catch (e) {
    apiHost = window.location.host
  }

  return html.replace(/\b(src|poster)=["'](.*?)["']/g, (match, attr, src) => {
    try {
      if (src.startsWith('http://') || src.startsWith('https://')) {
        const url = new URL(src)
        if (url.host === apiHost && url.pathname.startsWith('/uploads/')) {
          return `${attr}="${url.pathname}"`
        }
      }
    } catch (e) {
      // ignore
    }
    return match
  })
}
