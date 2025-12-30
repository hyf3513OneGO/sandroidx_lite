import client from './client'

export const listUsers = () => client.get('/users')
export const getUser = (id) => client.get(`/users/${id}`)
export const createUser = (payload) => client.post('/users', payload)
export const updateUser = (id, payload) => client.put(`/users/${id}`, payload)
export const deleteUser = (id) => client.delete(`/users/${id}`)
export const updateUserRole = (id, role) =>
  client.put(`/users/${id}/role`, { role })

