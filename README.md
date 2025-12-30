# SandroidX Lite: Agent-Centric Sandbox for Android

一个专为 Android Agent 设计的沙盒环境，提供一键启动、可观测、可复现的 Agent 操作体验。

![Overview](https://i.mji.rip/2025/12/30/89c0fbb7bff78102d27539665e4b8868.png)

## ✨ 核心特性

### 🚀 Agent 沙盒一键启动
快速创建和启动 Android Agent 沙盒环境，无需复杂配置即可开始使用。

![Agent Create Scale](https://i.mji.rip/2025/12/30/5e0fecb5b7dc6eb46655fb2e5af38214.gif)

### 📋 可观测、可复现 Agent 操作
实时监控 Agent 的操作过程，支持操作回放，确保每次执行都可追溯、可复现。

![Agent Replay](https://i.mji.rip/2025/12/30/ad6161d50829dacf5d4d9948d63ca40a.gif)

### 📤 轻松分享 Agent Demo
一键生成并分享 Agent 演示，让团队成员快速了解 Agent 的能力和效果。

![Agent Share](https://i.mji.rip/2025/12/30/1cae884c75f91396f7435df5613070a5.png)

### 📱 Agent一键启动
快速将应用推送到 Android 设备，简化应用部署流程。

![Agent Exec](https://i.mji.rip/2025/12/30/26809ec055f9826e6f3823e52e0c1585.png)

## 🎯 功能展示

### Agent 详情页面
查看 Agent 的详细信息和执行状态。

![Agent Detail](https://i.mji.rip/2025/12/30/5b66f1171d3afe24afa107f335433398.png)

### Agent 执行记录
追踪与复现 Agent 的历史执行记录，Agent微调轨迹数据触手可得。

![Agent Record](https://i.mji.rip/2025/12/30/c8f05802b48fb1404263a6642667f77e.png)

### Agent 演示
观看完整的 Agent 演示流程。

![Agent Demo](https://i.mji.rip/2025/12/30/576e814caa5700c47644966cb5ec2372.gif)


## 🎨 Agent范例

### AutoGLM Demo
这个范例是一个基于 [AutoGLM](https://github.com/zai-org/Open-AutoGLM) 构建的 Agent 示例，展示了如何利用SandroidX沙盒环境进行 Agent 开发和测试。

![AutoGLM Share](https://i.mji.rip/2025/12/30/6ff4158ec55f0f50f03711b3789481e1.png)
你可以通过以下在线 Demo 体验 **AutoGLM on Sandroidx**：

[Demo](https://demo.sandroidx.com/share/agents/IVSHU3kVltQV4YNSTZj1QDSa_Wh4JOsPS9fSbQeTK7U)
## 🔧 硬核科技

### 📝 ADB 命令自动化记录与回放
系统自动记录所有通过 ADB Gateway 执行的 ADB 命令，包括：
- **完整命令记录**：记录每个 ADB 命令的时间戳、来源、目标、命令内容等详细信息
- **智能分类存储**：按映射 ID、项目 ID、网关 ID 等维度进行分类存储，便于查询和分析
- **精确回放**：支持基于历史记录的精确命令回放，实现操作的可追溯和可复现
- **多维度查询**：提供丰富的查询接口，可按时间范围、映射、项目等条件灵活查询命令日志

这一功能为 Agent 的调试、优化和审计提供了强大的数据支持，让每一次操作都有据可查。

### 🔄 ADB 热切换（无感知设备切换）
支持在 Agent 运行过程中无缝切换被操控的 Android 设备，Agent 完全无感知：
- **零停机切换**：通过更新映射的 `upstream` 配置，可在不中断 Agent 连接的情况下切换目标设备
- **强制断开控制**：支持 `force_disconnect` 参数，确保旧连接完全断开，避免连接冲突
- **透明代理**：ADB Gateway 作为透明代理层，Agent 始终连接到固定的映射端口，底层设备切换对上层完全透明
- **状态保持**：切换过程中保持映射配置的一致性，确保 Agent 操作连续性

这一技术使得系统可以在设备故障、性能优化或负载均衡等场景下，实现设备的平滑切换，大大提升了系统的可靠性和灵活性。

## 🛠️ 技术栈

- **后端**: Go (Gin Framework)
- **前端**: Vue.js
- **数据库**: SQLite / MySQL
- **容器化**: Docker
- **Android 连接**: ADB Gateway + Scrcpy