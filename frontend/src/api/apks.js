import client from './client'

export const listApks = () => client.get('/apks/list')
export const getApk = (id) => client.get(`/apks/detail/${id}`)
export const createApk = (payload) => client.post('/apks/create', payload)
export const updateApk = (id, payload) => client.post(`/apks/update/${id}`, payload)
export const deleteApk = (id) => client.post(`/apks/delete/${id}`)

export const uploadApk = (formData) =>
  client.post('/apks/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })

export const uploadApkPreview = (formData, onProgress) =>
  client.post('/apks/upload-preview', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
    onUploadProgress: (progressEvent) => {
      if (onProgress && progressEvent.total) {
        const percentCompleted = Math.round((progressEvent.loaded * 100) / progressEvent.total)
        onProgress(percentCompleted)
      }
    },
  })

export const downloadApk = (id) => client.post(`/apks/download/${id}`)

export const downloadApkPreview = (url, onProgress) => {
  // 注意：下载预览返回的是 JSON 响应，不是文件流，所以不支持 onDownloadProgress
  // 如果需要显示进度，需要在后端实现流式响应或分块下载
  return client.post('/apks/download-preview', { url })
}


