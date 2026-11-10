<script setup lang="ts">
import { NodeViewWrapper, nodeViewProps } from '@tiptap/vue-3'

const props = defineProps(nodeViewProps)
</script>

<template>
  <node-view-wrapper class="tiptap-video-wrapper" :class="{ 'is-selected': selected }">
    <video
      :src="node.attrs.src"
      :poster="node.attrs.poster || undefined"
      controls
      playsinline
      preload="metadata"
      class="tiptap-video"
    ></video>
  </node-view-wrapper>
</template>

<style scoped>
.tiptap-video-wrapper {
  display: flex;
  justify-content: center;
  margin: 1rem 0;
}

.tiptap-video {
  width: 100%;
  max-width: 100%;
  height: auto;
  border-radius: 0.5rem;
  background-color: #000;
  display: block;
}

.tiptap-video-wrapper.is-selected .tiptap-video {
  outline: 2px solid hsl(var(--primary));
  outline-offset: 2px;
}
</style>
