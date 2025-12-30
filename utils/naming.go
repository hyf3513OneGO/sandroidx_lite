package utils

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GenerateAgentID 生成 Agent ID
// 格式: agent_年_月_日_uuid前4位
// 例如: agent_2025_12_20_a1b2
func GenerateAgentID() string {
	now := time.Now()
	uuidStr := uuid.New().String()
	shortUUID := strings.ReplaceAll(uuidStr, "-", "")[:4]
	return fmt.Sprintf("agent_%04d_%02d_%02d_%s", now.Year(), now.Month(), now.Day(), shortUUID)
}

// GenerateVolumeID 生成用户 Volume ID
// 格式: volume_uuid前12位
// 例如: volume_abc123def456
func GenerateVolumeID() string {
	return fmt.Sprintf("volume_%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:12])
}

// GenerateSystemVolumeID 生成系统 Volume ID
// 格式: volume_system_名称
// 例如: volume_system_apks
func GenerateSystemVolumeID(name string) string {
	return fmt.Sprintf("volume_system_%s", name)
}

// GenerateAdbMappingName 生成 ADB 映射名称
// 格式: agent_agentID_随机后缀
// 例如: agent_agent_2025_12_20_a1b2_xyz9
func GenerateAdbMappingName(agentID string) string {
	randomSuffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:4]
	return fmt.Sprintf("agent_%s_%s", agentID, randomSuffix)
}

// GenerateAdbMappingNote 生成 ADB 映射备注
// 格式: Agent {agentID} 的 ADB 映射
func GenerateAdbMappingNote(agentID string) string {
	return fmt.Sprintf("Agent %s 的 ADB 映射", agentID)
}

// GenerateVolumeDescription 生成 Volume 描述
// 格式: 用户挂载卷（首次由 Agent {agentID} 创建）
func GenerateVolumeDescription(agentID string) string {
	return fmt.Sprintf("用户挂载卷（首次由 Agent %s 创建）", agentID)
}

// GenerateSandboxID 生成 Sandbox ID
// 格式: sandbox_年_月_日_uuid前4位
// 例如: sandbox_2025_12_21_a1b2
func GenerateSandboxID() string {
	now := time.Now()
	uuidStr := uuid.New().String()
	shortUUID := strings.ReplaceAll(uuidStr, "-", "")[:4]
	return fmt.Sprintf("sandbox_%04d_%02d_%02d_%s", now.Year(), now.Month(), now.Day(), shortUUID)
}

// GenerateTemplateID 生成 Template ID
// 格式: template_年_月_日_uuid前4位
// 例如: template_2025_12_26_abcd
func GenerateTemplateID() string {
	now := time.Now()
	uuidStr := uuid.New().String()
	shortUUID := strings.ReplaceAll(uuidStr, "-", "")[:4]
	return fmt.Sprintf("template_%04d_%02d_%02d_%s", now.Year(), now.Month(), now.Day(), shortUUID)
}

// GenerateApkID 生成 Apk ID
// 格式: apk_年_月_日_uuid前4位
// 例如: apk_2025_12_26_abcd
func GenerateApkID() string {
	now := time.Now()
	uuidStr := uuid.New().String()
	shortUUID := strings.ReplaceAll(uuidStr, "-", "")[:4]
	return fmt.Sprintf("apk_%04d_%02d_%02d_%s", now.Year(), now.Month(), now.Day(), shortUUID)
}

func CmdString(cmd *exec.Cmd) string {
	return strings.Join(cmd.Args, " ")
}
