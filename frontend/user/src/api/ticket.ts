import { userApi } from './client'

export const userTicketAPI = {
    list: (params?: any) => userApi.get('/tickets', { params }),
    detail: (id: number) => userApi.get(`/tickets/${id}`),
    create: (data: { title: string; content: string; priority?: string; order_no?: string; image_url?: string }) =>
        userApi.post('/tickets', data),
    reply: (id: number, data: { content: string; image_url?: string }) =>
        userApi.post(`/tickets/${id}/reply`, data),
    uploadImage: (file: File) => {
        const formData = new FormData()
        formData.append('file', file)
        return userApi.post('/tickets/upload', formData)
    },
    badge: () => userApi.get('/tickets/badge'),
}
