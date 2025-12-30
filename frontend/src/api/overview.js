import client from './client'

// 系统总览：统计数量 + 主机资源指标
export const getOverview = () => client.get('/overview')


