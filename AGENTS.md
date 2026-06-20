# Repository Guidelines

## Project Structure & Module Organization

本仓库是 Wails 3 桌面应用。`main.go` 负责注册服务并控制启动、关闭顺序；涉及数据库、写入队列、代理服务或健康检查时，不要随意调整初始化顺序。Go 业务代码集中在 `services/`，同目录放置 `*_test.go`；前端在 `frontend/src/`，类型绑定在 `frontend/bindings/`。模型价格数据和相关测试在 `resources/model-pricing/`，应用图标与打包资源在 `assets/`、`build/`。发布工作流位于 `.github/workflows/`，`scripts/` 中脚本需先确认用途再复用。

## Build, Test, and Development Commands

```bash
wails3 task dev
wails3 task build
wails3 task package
go test ./...
go vet ./...
cd frontend && npm run build
wails3 task common:generate:bindings
wails3 task common:update:build-assets
```

`wails3 task dev` 启动本地开发环境，默认前端端口为 `9245`。`go test ./...` 运行全部 Go 测试，`go vet ./...` 做静态检查。`npm run build` 会执行前端类型检查和生产构建。修改 Go service 的导出方法后，必须刷新 `frontend/bindings/`；修改图标或构建资产后，先更新 build assets。

## Coding Style & Naming Conventions

Go 代码使用 `gofmt`，保持包名短小、服务类型以 `Service` 结尾，测试函数使用 `TestXxx`。Vue 单文件组件使用 PascalCase 文件名，组合式函数使用 `useXxx`，普通 TypeScript 工具函数使用 camelCase。前端调用后端能力时优先封装在 `frontend/src/services/`，避免组件直接堆叠调用细节。

## Testing Guidelines

优先补与改动同包的 Go 测试。代理、供应商配置、价格计算、数据库写入队列等共享逻辑变更后，应运行 `go test ./...`。前端 UI 或类型改动后运行 `cd frontend && npm run build`。测试夹具放在 `services/testdata/` 或对应包的明确测试目录，不要把临时实验文件放入正式测试集合。

## Commit & Pull Request Guidelines

提交信息使用 Conventional Commits，例如 `feat: 完善定价规则`、`fix: 修复发布构建依赖`、`chore: 清理测试产物`。PR 需说明变更范围、验证命令、是否更新绑定或构建资产；UI 变化附截图。不要提交 `*.exe`、`test-results-*.json`、`nul`、`%s`、`frontend/dist/`、`frontend/node_modules/`、本地数据库、日志或密钥配置，也不要添加生成署名或共同作者尾注。
