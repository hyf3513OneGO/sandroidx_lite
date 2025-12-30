import client from './client'

export const listMappings = () => client.get('/adb-gateway/mappings')
export const getMapping = (id) => client.get(`/adb-gateway/mappings/${id}`)
export const createMapping = (payload) =>
  client.post('/adb-gateway/mappings/create', payload)
export const updateMapping = (payload) =>
  client.post('/adb-gateway/mappings/update', payload)
export const removeMapping = (id) =>
  client.post('/adb-gateway/mappings/remove', { id })

export const listMappingsFromDB = () => client.get('/adb-gateway/mappings/db')
export const syncMappings = () => client.post('/adb-gateway/sync')

