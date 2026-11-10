import { Node, VueNodeViewRenderer } from '@tiptap/vue-3'
import TiptapEmbedView from './TiptapEmbed.vue'
import { EMBED_ALLOWED_HOSTS } from '@/utils/videoEmbed'

function isAllowedEmbedHost(src: string): boolean {
  try {
    const host = new URL(src).hostname.toLowerCase()
    return (EMBED_ALLOWED_HOSTS as readonly string[]).includes(host)
  } catch {
    return false
  }
}

/**
 * 平台视频嵌入节点（YouTube / Bilibili / Vimeo）。
 *
 * 与 video 节点的区别：这些平台的播放页是 HTML 而不是媒体流，只能通过 iframe embed 播放。
 * 只接受白名单域名，避免富文本被当作任意 iframe 的注入通道。
 */
export const Embed = Node.create({
  name: 'embed',
  group: 'block',
  atom: true,
  draggable: true,
  selectable: true,

  addAttributes() {
    return {
      src: {
        default: null,
        parseHTML: (element: HTMLElement) => element.getAttribute('src'),
      },
    }
  },

  parseHTML() {
    return [
      {
        tag: 'iframe[src]',
        getAttrs: (element) => {
          const src = (element as HTMLElement).getAttribute('src') || ''
          return isAllowedEmbedHost(src) ? null : false
        },
      },
    ]
  },

  renderHTML({ node }) {
    return [
      'iframe',
      {
        src: String(node.attrs.src || ''),
        frameborder: '0',
        allow: 'accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share',
        allowfullscreen: 'true',
        referrerpolicy: 'strict-origin-when-cross-origin',
        class: 'w-full aspect-video rounded-lg',
      },
    ]
  },

  addNodeView() {
    return VueNodeViewRenderer(TiptapEmbedView)
  },
})
