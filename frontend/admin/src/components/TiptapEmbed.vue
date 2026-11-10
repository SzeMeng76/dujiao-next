<script setup lang="ts">
import { NodeViewWrapper, nodeViewProps } from '@tiptap/vue-3'

const props = defineProps(nodeViewProps)

const providerLabel = () => {
  const src = String(props.node.attrs.src || '')
  if (src.includes('youtube')) return 'YouTube'
  if (src.includes('bilibili')) return 'Bilibili'
  if (src.includes('vimeo')) return 'Vimeo'
  return 'Embed'
}
</script>

<template>
  <node-view-wrapper class="tiptap-embed-wrapper" :class="{ 'is-selected': selected }">
    <div class="embed-frame">
      <iframe
        :src="node.attrs.src"
        frameborder="0"
        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
        allowfullscreen
        referrerpolicy="strict-origin-when-cross-origin"
      ></iframe>
    </div>
    <div class="embed-badge">{{ providerLabel() }}</div>
  </node-view-wrapper>
</template>

<style scoped>
.tiptap-embed-wrapper {
  position: relative;
  margin: 1rem 0;
}

.embed-frame {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  border-radius: 0.5rem;
  overflow: hidden;
  background-color: #000;
}

.embed-frame iframe {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  border: 0;
  display: block;
}

.tiptap-embed-wrapper.is-selected .embed-frame {
  outline: 2px solid hsl(var(--primary));
  outline-offset: 2px;
}

.embed-badge {
  position: absolute;
  left: 0.5rem;
  bottom: 0.5rem;
  padding: 0.125rem 0.4rem;
  border-radius: 0.25rem;
  background-color: rgba(0, 0, 0, 0.65);
  color: #fff;
  font-size: 11px;
  line-height: 1.4;
  pointer-events: none;
}

/* 选中时 iframe 会吃掉点击事件，禁止其交互，让节点能被选中/拖动/删除 */
.tiptap-embed-wrapper:not(.is-selected) .embed-frame iframe {
  pointer-events: none;
}
</style>
