import client from './client'

export const listVolumes = (type) =>
  client.get('/volumes/list', { params: { type } })

export const getVolume = (id) => client.get(`/volumes/detail/${id}`)

export const createVolume = (payload) => client.post('/volumes/create', payload)

export const deleteVolume = (id, force = false) =>
  client.post(`/volumes/delete/${id}`, null, { params: { force } })

