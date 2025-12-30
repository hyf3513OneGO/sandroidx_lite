import client from './client'

export const login = (payload) => client.post('/auth/login', payload)
export const register = (payload) => client.post('/auth/register', payload)
export const setupAdmin = (payload) => client.post('/auth/setup-admin', payload)
export const status = () => client.get('/auth/status')

