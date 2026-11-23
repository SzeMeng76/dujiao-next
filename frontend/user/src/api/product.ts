import { api } from './client'

export const productAPI = {
    list: (params?: any) => api.get('/public/products', { params }),
    /** 首页分类精选：每类固定取 N 个商品 */
    showcase: (params?: any) => api.get('/public/products/showcase', { params }),
    detail: (slug: string) => api.get(`/public/products/${slug}`),
}

export const postAPI = {
    list: (params?: any) => api.get('/public/posts', { params }),
    detail: (slug: string) => api.get(`/public/posts/${slug}`),
}

export const bannerAPI = {
    list: (params?: any) => api.get('/public/banners', { params }),
}

export const categoryAPI = {
    list: (params?: any) => api.get('/public/categories', { params }),
}

export const memberLevelAPI = {
    list: () => api.get('/public/member-levels'),
}
