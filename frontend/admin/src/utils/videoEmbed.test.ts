import { describe, expect, it } from 'vitest'
import { resolveVideoUrl } from './videoEmbed'

describe('resolveVideoUrl', () => {
  describe('直链文件', () => {
    it('识别 mp4 直链', () => {
      const result = resolveVideoUrl('https://cdn.example.com/videos/demo.mp4')
      expect(result?.kind).toBe('file')
      expect(result?.url).toBe('https://cdn.example.com/videos/demo.mp4')
    })

    it('保留直链上的查询参数（签名链接）', () => {
      const signed = 'https://drive.example.com/d/Dropbox/demo.mp4?sign=abc%3D%3D:0'
      const result = resolveVideoUrl(signed)
      expect(result?.kind).toBe('file')
      expect(result?.url).toBe(signed)
    })

    it('识别带查询串的 webm', () => {
      const result = resolveVideoUrl('https://cdn.example.com/a.webm?t=1')
      expect(result?.kind).toBe('file')
    })

    it('识别大写扩展名', () => {
      expect(resolveVideoUrl('https://cdn.example.com/A.MP4')?.kind).toBe('file')
    })

    it('缺协议时自动补 https', () => {
      const result = resolveVideoUrl('cdn.example.com/demo.mp4')
      expect(result?.kind).toBe('file')
      expect(result?.url).toBe('https://cdn.example.com/demo.mp4')
    })
  })

  describe('YouTube', () => {
    it('watch?v=', () => {
      const r = resolveVideoUrl('https://www.youtube.com/watch?v=dQw4w9WgXcQ')
      expect(r?.kind).toBe('embed')
      expect(r?.provider).toBe('youtube')
      expect(r?.url).toBe('https://www.youtube.com/embed/dQw4w9WgXcQ')
    })

    it('youtu.be 短链', () => {
      const r = resolveVideoUrl('https://youtu.be/dQw4w9WgXcQ')
      expect(r?.url).toBe('https://www.youtube.com/embed/dQw4w9WgXcQ')
    })

    it('shorts', () => {
      const r = resolveVideoUrl('https://youtube.com/shorts/dQw4w9WgXcQ')
      expect(r?.url).toBe('https://www.youtube.com/embed/dQw4w9WgXcQ')
    })

    it('live', () => {
      const r = resolveVideoUrl('https://www.youtube.com/live/dQw4w9WgXcQ')
      expect(r?.url).toBe('https://www.youtube.com/embed/dQw4w9WgXcQ')
    })

    it('已是 embed 地址时保持幂等', () => {
      const r = resolveVideoUrl('https://www.youtube.com/embed/dQw4w9WgXcQ')
      expect(r?.url).toBe('https://www.youtube.com/embed/dQw4w9WgXcQ')
    })

    it('带额外参数时只保留视频 ID', () => {
      const r = resolveVideoUrl('https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=30s&list=PL123')
      expect(r?.url).toBe('https://www.youtube.com/embed/dQw4w9WgXcQ')
    })

    it('无 www 前缀同样识别', () => {
      expect(resolveVideoUrl('https://youtube.com/watch?v=dQw4w9WgXcQ')?.provider).toBe('youtube')
    })

    it('ID 长度不符时不误判', () => {
      const r = resolveVideoUrl('https://www.youtube.com/watch?v=short')
      expect(r?.kind).not.toBe('embed')
    })

    it('真实链接回归：watch?v=11 位 ID', () => {
      const r = resolveVideoUrl('https://www.youtube.com/watch?v=hpdNYrYqcuo')
      expect(r).toEqual({
        kind: 'embed',
        provider: 'youtube',
        url: 'https://www.youtube.com/embed/hpdNYrYqcuo',
        raw: 'https://www.youtube.com/watch?v=hpdNYrYqcuo',
      })
    })
  })

  describe('Bilibili', () => {
    it('/video/BVxxx', () => {
      const r = resolveVideoUrl('https://www.bilibili.com/video/BV1xx411c7mD')
      expect(r?.kind).toBe('embed')
      expect(r?.provider).toBe('bilibili')
      expect(r?.url).toContain('bvid=BV1xx411c7mD')
    })

    it('保留分 P 参数', () => {
      const r = resolveVideoUrl('https://www.bilibili.com/video/BV1xx411c7mD?p=3')
      expect(r?.url).toContain('page=3')
    })

    it('av 号', () => {
      const r = resolveVideoUrl('https://www.bilibili.com/video/av170001')
      expect(r?.url).toContain('aid=170001')
    })
  })

  describe('Vimeo', () => {
    it('数字 ID', () => {
      const r = resolveVideoUrl('https://vimeo.com/123456789')
      expect(r?.url).toBe('https://player.vimeo.com/video/123456789')
    })

    it('已是 player 地址', () => {
      const r = resolveVideoUrl('https://player.vimeo.com/video/123456789')
      expect(r?.url).toBe('https://player.vimeo.com/video/123456789')
    })
  })

  describe('不接受的情况', () => {
    it('空字符串', () => {
      expect(resolveVideoUrl('   ')).toBeNull()
    })

    it('普通网页', () => {
      expect(resolveVideoUrl('https://example.com/article')).toBeNull()
    })

    it('非 http 协议', () => {
      expect(resolveVideoUrl('ftp://example.com/a.mp4')).toBeNull()
      expect(resolveVideoUrl('javascript:alert(1)')).toBeNull()
    })

    it('只允许白名单平台走 embed', () => {
      expect(resolveVideoUrl('https://evil.example.com/video/123456789')).toBeNull()
    })
  })
})
