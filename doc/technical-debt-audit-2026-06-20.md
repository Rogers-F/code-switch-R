# 技术债与潜在问题审计报告

日期：2026-06-20
范围：当前工作树，只做审计与记录，除本文档外未修复代码。

## 验证摘要

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `go test -timeout 60s ./...` | 通过，52s | 注意：此命令触发了 P0 中的真实数据库写入问题。 |
| `go vet ./...` | 通过，10s | 未发现 vet 级别问题。 |
| `cd frontend && npx vue-tsc --noEmit` | 通过，12s | 前端类型检查通过。 |
| `cd frontend && npm run build` | 失败，14s | 当前 Node 为 v21.5.0，Vite 7.2.1 构建时报 `crypto.hash is not a function`。 |
| `go test -race -timeout 60s ./services` | 未覆盖 | 当前 Go 环境未启用 cgo，输出 `go: -race requires cgo`。 |

## P0：测试会改写真实用户数据库

证据：

- [requestlog_mock_test.go](../requestlog_mock_test.go) 第 18-25 行在测试初始化中使用 `os.UserHomeDir()`，连接真实用户目录下的 `.code-switch/app.db`。
- [requestlog_mock_test.go](../requestlog_mock_test.go) 第 32-35 行执行 `xdb.New("request_log").Delete()`，随后写入 16 个月模拟请求日志。
- 本轮执行 `go test -timeout 60s ./...` 后，`C:\Users\Administrator.DESKTOP-BR3KL51\.code-switch\app.db` 的修改时间变为 `2026/6/20 12:25:57`。
- 对比同仓库其他测试，[services/providerservice_rename_test.go](../services/providerservice_rename_test.go) 第 19-28 行已经使用 `t.TempDir()` 隔离数据库，说明该根包测试是不一致的异常点。

影响：

- 开发者按项目文档运行全量测试时，会清空并重灌本机真实请求日志。
- 这不是测试隔离问题那么简单，而是数据完整性风险。

建议：

- 立即将该测试改为临时 HOME / USERPROFILE，并初始化独立数据库。
- 若只是造数脚本，应移出正式 `*_test.go`，放入显式脚本或测试夹具目录。
- 增加防护：任何测试中出现真实用户目录、真实应用数据库、全表删除，都应在评审中阻断。

## P1：前端构建环境不可复现

证据：

- 当前环境：`node -v` 为 `v21.5.0`，`npm -v` 为 `10.2.4`。
- `npm run build` 失败，输出说明当前 Node 版本不满足 Vite 要求，并在处理 [frontend/src/App.vue](../frontend/src/App.vue) 时触发 `crypto.hash is not a function`。
- 项目根目录和 `frontend/` 均不存在 `.nvmrc` / `.node-version`。
- [frontend/package.json](../frontend/package.json) 未声明 `engines` 或 `packageManager`。
- [frontend/package-lock.json](../frontend/package-lock.json) 与 [frontend/pnpm-lock.yaml](../frontend/pnpm-lock.yaml) 同时被跟踪，且锁定的 Vite 版本不同：前者为 7.2.1，后者为 7.2.6。

影响：

- 不同开发机可能出现“类型检查通过，但构建失败”的情况。
- 双锁文件会让依赖解析来源不明确，发布包与本地调试环境容易漂移。

建议：

- 明确唯一包管理器，并删除另一套锁文件。
- 在 `package.json` 中声明 Node 版本范围和包管理器。
- 增加 `.nvmrc` 或 `.node-version`，与发布流水线保持一致。
- 将 `npm install` 改为可复现安装命令。

## P1：发布流水线没有质量门禁

证据：

- [.github/workflows/release.yml](../.github/workflows/release.yml) 只在 tag 推送时打包发布。
- 该文件第 33、108、211 行固定 Go 版本，第 37、112、215 行固定 Node 版本。
- 第 40、115、229 行安装打包工具时使用 `@latest`。
- 第 43、118、232 行使用 `npm install`。
- 对工作流搜索 `go test`、`go vet`、`npm run build`、`vue-tsc`，命中数为 0。

影响：

- 发布流程可以在未跑单元测试、静态检查的情况下产出安装包。
- 打包工具取 latest，发布结果可能随时间变化。

建议：

- 增加 PR / main 分支 CI：`go test`、`go vet`、前端类型检查与构建。
- 发布工作流复用相同质量门禁。
- 固定打包工具版本，避免发布日拉到新版本导致不可复现。

## P1：本地代理启动失败不会反馈给应用初始化

证据：

- [main.go](../main.go) 第 107-110 行硬编码本地代理地址与端口。
- [main.go](../main.go) 第 133-137 行用 goroutine 调用 `providerRelay.Start()`。
- [services/providerrelay.go](../services/providerrelay.go) 第 249-274 行的 `Start()` 内部又启动 goroutine，并立即返回 nil。
- 若 `ListenAndServe` 因端口占用失败，只会在内部打印日志，不会阻止主窗口启动。
- [main.go](../main.go) 第 242 行关闭代理时忽略 `Stop()` 错误。

影响：

- 用户看到应用正常打开，但本地代理可能未实际监听。
- 前端与外部工具后续请求会表现为连接失败，根因被延迟暴露。

建议：

- `Start()` 应同步完成监听建立或返回启动错误。
- 对端口占用给出用户可见错误，并提供端口配置入口。
- 关闭阶段记录并上报 `Stop()` 错误。

## P1：自定义工具标识缺少路径安全校验

证据：

- [services/providerservice.go](../services/providerservice.go) 第 120-131 行将 `custom:<tool-id>` 中的 `tool-id` 直接拼成 `providers/<tool-id>.json`。
- [services/providerrelay.go](../services/providerrelay.go) 第 1887-1895 行从路由参数读取 `toolId` 并拼成 `custom:<toolId>`。
- [services/providerrelay.go](../services/providerrelay.go) 第 2401-2408 行模型列表路径也使用同样拼接。
- [services/customcliservice.go](../services/customcliservice.go) 第 494-495 行同样直接把 `toolId` 拼成文件名。

影响：

- 常规 UI 生成的是 UUID，风险受限。
- 但后端导出方法和本地代理路由没有同层校验，一旦传入包含路径分隔符、`..` 或其他异常字符的标识，配置读写边界就不清晰。

建议：

- 为自定义工具标识建立统一校验函数，只允许 UUID 或明确的安全字符集。
- 拼接路径后用 `filepath.Clean` 和前缀检查确认仍在预期目录内。
- 为异常标识补单元测试，覆盖保存、读取、删除和代理请求路径。

## P2：批量可用性检测会截断单项超时配置

证据：

- [services/healthcheckservice.go](../services/healthcheckservice.go) 第 478 行在批量检测中创建固定 30 秒父级上下文。
- 同文件第 592 行又按单个供应商配置创建请求超时上下文。
- 当单个供应商配置大于 30 秒时，实际会被父级上下文提前截断。

影响：

- 用户配置了更长超时也不生效，结果可能被误判为失败。
- 手动单项检测与批量检测语义不一致。

建议：

- 批量检测父级超时应根据最大单项超时、并发数和安全上限计算。
- 或者每个 goroutine 独立使用自身超时，批量入口只管理总取消信号。

## P2：更新下载的 Range 响应校验不完整

证据：

- [services/updateservice.go](../services/updateservice.go) 第 744-755 行解析 `Content-Range` 时使用 `fmt.Sscanf`，但没有检查返回字段数和错误。
- 若服务端返回异常 206 响应，代码只依赖零值参与比较，存在误判路径。

影响：

- 断点续传遇到异常上游响应时，可能错误追加、错误重下或给出不清晰错误。
- 该路径涉及安装包下载，错误代价较高。

建议：

- 严格检查 `Sscanf` 返回值。
- 对总大小未知、范围不连续、服务端忽略 Range 等场景写表驱动测试。
- 对下载状态文件和临时文件删除错误做显式处理。

## P2：正式测试覆盖不匹配核心复杂度

证据：

- `services/dbqueue.go` 543 行，含单次队列、批量队列、panic 恢复、关闭排空、统计采样等并发逻辑，但正式测试中没有覆盖该组件。
- [services/providerservice_test.go](../services/providerservice_test.go) 第 741-843 行的复制测试没有调用真实 `DuplicateProvider`，而是手写构造副本。
- [main_test.go](../main_test.go) 只有空导入，不验证启动依赖顺序或服务注册。
- `go test -race -timeout 60s ./services` 当前未覆盖，原因是本机 Go 环境未启用 cgo。

影响：

- 最容易出现回归的队列、启动、下载、代理路径，反而缺少自动化保护。
- 看似有测试，但部分测试并没有覆盖真实实现。

建议：

- 为写入队列补充正式并发测试：入队超时、关闭排空、批量提交失败、panic 恢复、重复关闭。
- 将脚本中的压力验证收敛成可运行的正式测试或基准测试。
- 替换“手写副本”测试为真实方法调用。

## P2：前端与服务调用层类型边界弱

证据：

- `frontend/src` 中 `Call.ByName` 命中 110 处。
- `frontend/src` 中 `any` / `Record<string, any>` / `window as any` 等弱类型命中 57 处。
- [frontend/src/components/Main/Index.vue](../frontend/src/components/Main/Index.vue) 3934 行，集成供应商管理、轮询、事件订阅、导入、复制、状态刷新等多类职责。
- [services/providerrelay.go](../services/providerrelay.go) 2083 行，混合路由注册、供应商选择、协议转换、日志写入、错误响应和模型列表转发。

影响：

- 后端方法重命名或签名变更时，字符串调用不容易被类型检查发现。
- 大组件和大服务会抬高修改成本，局部修复更容易引入旁路回归。

建议：

- 前端优先使用生成绑定或集中封装服务 API，不在组件内散落字符串路径。
- 将主页面拆成供应商列表、状态轮询、导入、配置弹窗、动作处理等组合式模块。
- 将代理服务拆出路由、选择策略、转发、日志、协议转换和模型列表模块。

## P2：工作树卫生和忽略规则不足

证据：

- 当前跟踪文件 279 个，未跟踪文件 69 个。
- 根目录存在 10 个约 47 MB 的未跟踪可执行文件，另有一个约 3.6 MB 更新器。
- 根目录存在 48 个 `test-results-*.json` 和 3 个 `multi-provider-test-*.json`。
- `scripts/` 下存在 `app.db`、`test_concurrent.db` 和大量调试脚本。
- 根目录存在 `%s`、`nul`、`currentTextareaRef`、`temp.txt` 等异常文件；其中 `nul` 会让 `rg` 报 `函数不正确`。
- [.gitignore](../.gitignore) 只覆盖了少量前端产物和系统文件，未覆盖上述测试产物、数据库、可执行文件和临时文件。

影响：

- 容易误提交大文件、数据库或测试结果。
- 搜索和审计命令会被异常文件污染，降低排查效率。

建议：

- 清理未跟踪产物前先人工确认用途。
- 扩充 `.gitignore`：`*.exe`、`test-results-*.json`、`multi-provider-test-*.json`、临时数据库、`nul`、`%s`、本地调试文件。
- 将可保留的调试工具移入明确目录，并标注是否允许提交。

## P3：本地配置改动不应进入正式提交

证据：

- 当前工作树中有本地代理设置文件的未提交改动，增加了删除锁文件和 `git add *` 相关权限项。
- 该文件属于本地代理配置，且当前仓库规则把这类文件视为灰名单。

影响：

- 若误提交，会把个人本地权限策略带入团队仓库。
- `git add *` 这类宽泛操作也会增加误暂存风险。

建议：

- 提交前单独复核本地配置文件，不随业务改动一起提交。
- 若团队不要求跟踪该文件，应考虑移出版本控制或在文档中明确处理方式。

## 建议处理顺序

1. 修复或隔离 `requestlog_mock_test.go`，并评估本机真实数据库是否需要恢复。
2. 固化前端 Node 和包管理器，清理双锁文件漂移。
3. 给发布流水线加质量门禁，避免未测试代码直接打包。
4. 修复本地代理启动错误反馈和端口配置。
5. 为自定义工具标识补统一校验和路径边界测试。
6. 为写入队列、更新下载、可用性检测补正式测试。
7. 清理未跟踪产物并扩充忽略规则。

## 本轮未覆盖

- 未启动桌面应用做端到端手测。
- 未跑完整打包命令。
- 未进行网络依赖审计。
- 未删除或修复任何临时产物。
