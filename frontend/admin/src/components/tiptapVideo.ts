import { Node, VueNodeViewRenderer } from '@tiptap/vue-3'
import TiptapVideoView from './TiptapVideo.vue'

/**
 * 视频块节点。
 *
 * Tiptap 只保留 schema 里注册过的节点，未注册的 `<video>` 会在 setContent 时被丢弃，
 * 所以商品详情这类富文本必须显式注册本扩展，视频标签才能存进 content 并回显到前台。
 * 只接受 src / poster 两个属性，controls、playsinline、preload 固定输出。
 */
export const Video = Node.create({
  name: 'video',
  group: 'block',
  atom: true,
  draggable: true,
  selectable: true,

  addAttributes() {
    return {
      src: { default: null },
      poster: { default: null },
    }
  },

  parseHTML() {
    return [{ tag: 'video[src]' }]
  },

  renderHTML({ node }) {
    const attrs: Record<string, string> = {
      src: String(node.attrs.src || ''),
      controls: '',
      playsinline: '',
      preload: 'metadata',
      class: 'max-w-full h-auto rounded-lg',
    }
    if (node.attrs.poster) {
      attrs.poster = String(node.attrs.poster)
    }
    return ['video', attrs]
  },

  addNodeView() {
    return VueNodeViewRenderer(TiptapVideoView)
  },
})
