#!/bin/bash

set -e

echo "开始构建前端..."

# 进入前端目录
cd "$(dirname "$0")/frontend"

# 检查 node_modules 是否存在，如果不存在则安装依赖
if [ ! -d "node_modules" ]; then
    echo "检测到缺少 node_modules，正在安装依赖..."
    npm install
fi

# 构建前端
echo "正在编译前端..."
npm run build

# 返回项目根目录
cd ..

echo "前端构建完成！输出目录: frontend/dist"
echo ""
echo "运行以下命令启动 Go 服务器："
echo "  go run main.go"
echo ""
echo "或编译后运行："
echo "  go build -o sandroidx_lite main.go"
echo "  ./sandroidx_lite"

