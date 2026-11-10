/**
 * 视频链接识别。
 *
 * 后台「插入视频」只接受两种东西：
 *   1. 能直接喂给 <video src> 的媒体文件地址（mp4 / webm / ogg / mov）
 *   2. 平台播放页地址（YouTube / Bilibili / Vimeo），需要转成官方 embed 地址后用 <iframe> 嵌入
 *
 * 平台播放页不能直接塞进 <video src>：那是 HTML 页面而不是媒体流，播放器只会报格式不支持。
 */

export type VideoKind = 'file' | 'embed'

export type VideoProvider = 'youtube' | 'bilibili' | 'vimeo'

export interface ResolvedVideo {
  kind: VideoKind
  /** 真正写进 src 的地址：file 为原始媒体地址，embed 为官方 embed 地址 */
  url: string
  provider?: VideoProvider
  /** 用户原始输入，用于提示 */
  raw: string
}

const FILE_EXT = /\.(mp4|webm|ogg|ogv|mov|m4v)(\?.*)?$/i

const YOUTUBE_ID = /^[A-Za-z0-9_-]{11}$/
const BILIBILI_BVID = /^BV[0-9A-Za-z]{10}$/

function normalizeHost(host: string): string {
  return host.toLowerCase().replace(/^www\./, '').replace(/^m\./, '')
}

/** 从 YouTube 的各种链接形态里取出视频 ID。 */
function extractYouTubeID(url: URL): string | null {
  const host = normalizeHost(url.hostname)
  if (host === 'youtu.be') {
    const id = url.pathname.replace(/^\//, '').split('/')[0] || ''
    return YOUTUBE_ID.test(id) ? id : null
  }
  if (host !== 'youtube.com' && host !== 'youtube-nocookie.com' && host !== 'music.youtube.com') {
    return null
  }
  const v = url.searchParams.get('v')
  if (v && YOUTUBE_ID.test(v)) return v
  // /embed/ID、/shorts/ID、/live/ID、/v/ID
  const match = url.pathname.match(/^\/(?:embed|shorts|live|v)\/([A-Za-z0-9_-]{11})(?:\/|$)/)
  return match ? match[1]! : null
}

/** 从 Bilibili 链接里取出 BV 号或 av 号。 */
function extractBilibili(url: URL): { bvid?: string; aid?: string; page: string } | null {
  const host = normalizeHost(url.hostname)
  if (host !== 'bilibili.com' && host !== 'b23.tv') return null

  const page = url.searchParams.get('p') || '1'

  const bv = url.pathname.match(/(BV[0-9A-Za-z]{10})/)
  if (bv && BILIBILI_BVID.test(bv[1]!)) {
    return { bvid: bv[1], page }
  }
  const av = url.pathname.match(/\/av(\d+)/i)
  if (av) {
    return { aid: av[1], page }
  }
  // player.bilibili.com 已经带了 bvid 参数
  const qbvid = url.searchParams.get('bvid')
  if (qbvid && BILIBILI_BVID.test(qbvid)) {
    return { bvid: qbvid, page: url.searchParams.get('p') || page }
  }
  const qaid = url.searchParams.get('aid')
  if (qaid) {
    return { aid: qaid, page: url.searchParams.get('p') || page }
  }
  // b23.tv 短链要服务端跟随跳转才能拿到 BV 号，浏览器端受 CORS 限制做不到
  return null
}

function buildBilibiliEmbed(info: { bvid?: string; aid?: string; page: string }): string {
  const params = new URLSearchParams()
  if (info.bvid) params.set('bvid', info.bvid)
  if (info.aid) params.set('aid', info.aid)
  params.set('page', info.page)
  params.set('autoplay', '0')
  params.set('high_quality', '1')
  return `https://player.bilibili.com/player.html?${params.toString()}`
}

/**
 * 把用户输入的地址解析成可嵌入的形式。
 * 无法识别时返回 null，由调用方提示。
 */
export function resolveVideoUrl(input: string): ResolvedVideo | null {
  const raw = input.trim()
  if (!raw) return null

  // 没有协议时补 https，方便直接粘贴 youtu.be/xxx 这种
  let candidate = raw
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(candidate)) {
    candidate = `https://${candidate.replace(/^\/+/, '')}`
  }

  let url: URL
  try {
    url = new URL(candidate)
  } catch {
    return null
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return null

  const youtubeID = extractYouTubeID(url)
  if (youtubeID) {
    return {
      kind: 'embed',
      provider: 'youtube',
      url: `https://www.youtube.com/embed/${youtubeID}`,
      raw,
    }
  }

  const bili = extractBilibili(url)
  if (bili) {
    return { kind: 'embed', provider: 'bilibili', url: buildBilibiliEmbed(bili), raw }
  }

  const host = normalizeHost(url.hostname)
  if (host === 'vimeo.com' || host === 'player.vimeo.com') {
    if (host === 'player.vimeo.com' && /^\/video\/\d+/.test(url.pathname)) {
      return { kind: 'embed', provider: 'vimeo', url: url.toString(), raw }
    }
    const id = url.pathname.match(/\/(\d{6,})/)
    if (id) {
      return { kind: 'embed', provider: 'vimeo', url: `https://player.vimeo.com/video/${id[1]}`, raw }
    }
  }

  if (FILE_EXT.test(url.pathname)) {
    return { kind: 'file', url: candidate, raw }
  }

  return null
}

/** 已嵌入的 iframe 是否属于受支持的平台。前台净化时复用这份白名单。 */
export const EMBED_ALLOWED_HOSTS = [
  'www.youtube.com',
  'youtube.com',
  'www.youtube-nocookie.com',
  'youtube-nocookie.com',
  'player.bilibili.com',
  'player.vimeo.com',
] as const
