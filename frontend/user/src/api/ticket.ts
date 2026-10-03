import { userApi } from './client'

/** 工单服务类型：售前咨询 / 售后支持 */
export type TicketType = 'pre_sale' | 'after_sale'

export interface CreateTicketPayload {
    title: string
    content: string
    priority?: string
    /** 关联订单号；售后支持必填，售前可选 */
    order_no?: string
    /** 购买凭证/截图 URL，写入首条消息 */
    image_url?: string
    /** 服务类型，不传时后端按售后兼容 */
    ticket_type?: TicketType
    /** 关联商品 ID，售前咨询常用（可选） */
    product_id?: number
}

export const userTicketAPI = {
    list: (params?: any) => userApi.get('/tickets', { params }),
    detail: (id: number) => userApi.get(`/tickets/${id}`),
    create: (data: CreateTicketPayload) => userApi.post('/tickets', data),
    reply: (id: number, data: { content: string; image_url?: string }) =>
        userApi.post(`/tickets/${id}/reply`, data),
    uploadImage: (file: File) => {
        const formData = new FormData()
        formData.append('file', file)
        return userApi.post('/tickets/upload', formData)
    },
    badge: () => userApi.get('/tickets/badge'),
}
