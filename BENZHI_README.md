# BENZHI_README

## 项目说明
- 项目：benzhi-project-45d4623e-4119-4b7f-a2ba-dcc7eb85ecbf
- 项目用途：社区设施巡检闭环台是基于 Go 1.23 标准库的本地浏览器工作台，模块路径为 community-inspection，覆盖计划创建、按序巡检、异常复核、关闭报告和筛选查询，数据保存于本地 JSON 快照。
- Go 工具链：`golang:1.23`
- 前端工具链：原生 HTML、CSS 和 JavaScript，由 Go 服务直接提供

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run . selfcheck
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-45d4623e-4119-4b7f-a2ba-dcc7eb85ecbf-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-45d4623e-4119-4b7f-a2ba-dcc7eb85ecbf-arm64 linux/arm64
docker run -it benzhi-project-45d4623e-4119-4b7f-a2ba-dcc7eb85ecbf-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run . selfcheck`
