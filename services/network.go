package services

import "sandroidx.com/sandroidx_lite/configs"

// getNetworkName 获取Docker网络名称，如果配置为空则使用默认值
func getNetworkName() string {
	if configs.AppConfig != nil && configs.AppConfig.Server.NetworkName != "" {
		return configs.AppConfig.Server.NetworkName
	}
	// 默认网络名称
	return "sandroidx_lite_network"
}
