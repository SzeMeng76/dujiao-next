import DOMPurify from 'dompurify'
import { getImageUrl } from './image'

/**
 * 允许嵌入的播放器域名。
 * 富文本里的 iframe 只可能是视频嵌入，限定域名避免它变成任意 iframe 的注入通道。
 */
const EMBED_ALLOWED_HOSTS = new Set([
    'www.youtube.com',
    'youtube.com',
    'www.youtube-nocookie.com',
    'youtube-nocookie.com',
    'player.bilibili.com',
    'player.vimeo.com',
])

let embedHookInstalled = false

/**
 * 只保留白名单域名的 iframe。
 * DOMPurify 把 iframe 放在 FORBID_CONTENTS 里，非白名单项要手动移除节点；
 * srcdoc 一律清掉，避免绕过域名校验执行内联内容。
 */
function installEmbedHook() {
    if (embedHookInstalled) return
    DOMPurify.addHook('uponSanitizeElement', (node, data) => {
        if (data.tagName !== 'iframe') return
        const element = node as Element
        element.removeAttribute('srcdoc')
        const src = element.getAttribute('src') || ''
        let allowed = false
        try {
            allowed = EMBED_ALLOWED_HOSTS.has(new URL(src).hostname.toLowerCase())
        } catch {
            allowed = false
        }
        if (!allowed) {
            element.parentNode?.removeChild(element)
        }
    })
    embedHookInstalled = true
}

/**
 * 将 HTML 内容中的图片路径转换为显示用的绝对路径，并清除脚本
 * /uploads/xxx -> http://domain/uploads/xxx
 */
export function processHtmlForDisplay(html: string): string {
    if (!html) return ''

    installEmbedHook()

    // 匹配 src="/uploads/..." 与 poster="/uploads/..."（视频封面），支持单引号和双引号
    // 使用非贪婪匹配 .*?
    const withImages = html.replace(/\b(src|poster)=["'](\/uploads\/.*?)["']/g, (_, attr, path) => {
        return `${attr}="${getImageUrl(path)}"`
    })
    // 结果直接进 v-html，且与后台同源（localStorage 里有 admin_token）；上游导入的商品详情也走这里
    return DOMPurify.sanitize(withImages, {
        ADD_ATTR: ['target', 'allow', 'allowfullscreen', 'frameborder', 'referrerpolicy', 'playsinline', 'controls', 'preload', 'poster'],
        ADD_TAGS: ['iframe'],
    })
}

/**
 * 将 HTML 内容中的图片路径转换为存储用的相对路径
 * http://domain/uploads/xxx -> /uploads/xxx
 */
export function processHtmlForStorage(html: string): string {
    if (!html) return ''

    const apiBaseUrl = import.meta.env.VITE_API_BASE_URL || ''
    let apiHost = ''
    try {
        if (apiBaseUrl) {
            // 提取域名 (例如 localhost:8080 或 domain.com)
            apiHost = new URL(apiBaseUrl).host
        } else {
            // 如果未配置 API_BASE_URL，则认为是同源，使用当前域名
            apiHost = window.location.host
        }
    } catch (e) {
        // Fallback
        apiHost = window.location.host
    }

    // 匹配 src="value" 或 src='value'（含视频 poster 封面）
    return html.replace(/\b(src|poster)=["'](.*?)["']/g, (match, attr, src) => {
        try {
            // 如果是完整 URL (http:// 或 https://)
            if (src.startsWith('http://') || src.startsWith('https://')) {
                const url = new URL(src)

                // 检查域名是否匹配 API 域名，且路径以 /uploads/ 开头
                // url.host 会自动处理端口号 (例如 localhost:8080)
                if (url.host === apiHost && url.pathname.startsWith('/uploads/')) {
                    // 返回相对路径，例如 src="/uploads/xxx.png"
                    // 始终标准化为双引号
                    return `${attr}="${url.pathname}"`
                }
            }
        } catch (e) {
            // URL 解析失败，忽略，返回原样
        }
        return match
    })
}
