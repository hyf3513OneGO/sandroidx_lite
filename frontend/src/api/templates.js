import client from './client'

export const listTemplates = () => client.get('/templates/list')
export const getTemplate = (id) => client.get(`/templates/detail/${id}`)
export const createTemplate = (payload) => client.post('/templates/create', payload)
export const updateTemplate = (id, payload) =>
  client.post(`/templates/update/${id}`, payload)
export const deleteTemplate = (id) => client.post(`/templates/delete/${id}`)

