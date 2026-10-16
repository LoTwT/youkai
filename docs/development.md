# 开发指南

更新日期：2026-10-16。本文维护环境要求与本仓库当前实际可用的开发命令；范围与里程碑安排见[实施清单](specs/scanner-implementation-plan.md)，构建与分发决策见 [T7](specs/t7-public-release-acceptance.md)。

## 环境要求

| 工具 | 要求与用途 |
| --- | --- |
| Go | 1.27.2（`go.mod` 固定 `go 1.27.2`；MyGo v0.5.5 要求 1.27.1+）。 |
| MyGo | v0.5.5，作为 Go tool 依赖固定在 `go.mod`（`tool github.com/egoist/mygo/cmd/mygo`），经 `go tool mygo` 使用，不依赖全局安装的 `mygo` 命令。 |
| Xcode 命令行工具 | macOS 打包所需（`codesign`、`hdiutil`、SDK）；开发机 `xcode-select` 指向 `/Applications/Xcode.app`。 |
| mingw-w64 | 本机已安装（`/opt/homebrew/bin/x86_64-w64-mingw32-gcc`，Homebrew mingw-w64 14.0.0_3）。`internal/recognition` 的 OCR 绑定（onnxruntime_go）需要 cgo，Mac 上交叉编译 Windows 目标时用作 `CC`。 |
| makensis | 本机已安装（v3.13）。macOS 上交叉构建 Windows 平台时也会产出 NSIS 安装包；未安装时 MyGo 仅警告并跳过安装包。 |

不使用 Web 前端：`mygo.json` 为纯原生 UI 配置，无需 Bun、Node.js 或 WebView2。

cgo 说明：`internal/recognition` 依赖 cgo 编译 onnxruntime_go 的 C 桥接。直接调用的 `go build`/`go vet`/`go test` 与 `go tool mygo vet .` 在原生构建下默认 `CGO_ENABLED=1`，无需处理。`go tool mygo dev` 与 `go tool mygo build` 则遵循 MyGo v0.5.5 的行为：环境未显式设置 `CGO_ENABLED` 时，MyGo 给子进程设置 `CGO_ENABLED=0`（[T1](specs/t1-mygo-ui-distribution.md) 已记录，显式设置时尊重用户值）。当前 `cmd/youkai` 尚未引用识别包，应用构建不受影响；识别包接入应用后，必须以 `CGO_ENABLED=1 go tool mygo dev`、`CGO_ENABLED=1 go tool mygo build` 的方式调用，否则 cgo 包编译失败。识别包在包初始化时不加载任何原生库与模型（见[架构第 4 节](specs/scanner-architecture.md#4-ocr原生资源与构建)）。

## 常用命令

在仓库根目录执行。

| 命令 | 用途 |
| --- | --- |
| `gofmt -l .` | 格式检查，无输出即通过。 |
| `go vet ./...` | Go 静态检查。 |
| `go build ./...` | 编译全部包。 |
| `go test ./...` | 运行测试；`internal/recognition` 的集成测试在 OCR 资源缺失时自动跳过，本地需先运行 fetchdeps。 |
| `go test -race ./internal/recognition` | 识别包的竞态检查（含取消与生命周期并发路径）。 |
| `go tool mygo vet .` | 先运行 `go vet ./...`，再检查原生 UI 生命周期问题；参数是应用目录（项目根），不能写成 `./...`。 |
| `go tool mygo dev` | 开发运行，编辑 Go 代码、配置或资源后自动重建重启；开发应用名为 `Youkai Dev`（标识 `com.lotwt.youkai.dev`，位于 `.mygo/dev`），与正式安装的数据和权限隔离。退出应用或 Ctrl+C 结束。 |
| `go tool mygo build` | 打包本机平台制品到 `build/<os>-<arch>/`：macOS 生成 ad-hoc 签名的 `Youkai.app` 与 DMG。 |
| `CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 go build ./...` | Windows 交叉编译检查（OCR 绑定需要 cgo 与 mingw-w64）；只证明能编译，不代表能在 Windows 上运行。需要交叉编译识别测试二进制时把 `build` 换成 `test -c -o <path> ./internal/recognition`。 |

`go tool mygo build` 亦接受 `-platform` 指定目标，如 `-platform windows/amd64`（在 macOS 上产出 EXE，装有 makensis 时同时产出 NSIS 安装包）。按已确认的测试方式，交给用户测试的 Windows 安装包仍以 GitHub Actions 产物为准，见下节。

## GitHub Actions 检查

`.github/workflows/ci.yml` 在每个 pull request 与推送到 `main` 时，于 `macos-latest`（Apple Silicon）与 `windows-latest` 两个 runner 上运行轻量检查：`gofmt -l .`（要求无输出）、`go vet ./...`、`go test ./...`，macOS 上额外运行 `go tool mygo vet .`。检查不打安装包、不跑长时间测试、不下载 OCR 资源；供 Windows 测试的安装包按需另行由 GitHub Actions 构建，见[实施清单](specs/scanner-implementation-plan.md#开发与-windows-测试方式)。仓库根 `.gitattributes` 将文本文件统一为 LF 换行，Windows 检出不会因此产生 `gofmt` 误报。

OCR cgo 绑定接入后，两个 runner 都要编译 `internal/recognition` 的 C 桥接：macOS runner 自带 Xcode 的 `clang`，`windows-latest` 镜像自带 MinGW gcc（写入镜像工具清单，Windows Server 2025 为 gcc 15.2.0），且原生构建 `CGO_ENABLED` 默认为 1，CI 无需额外装工具链；资源缺失时识别集成测试用 `t.Skip` 跳过，因此 PR 检查仍不下载 OCR 资源。以上 runner 镜像结论来自 runner-images 的镜像清单，首次在本仓库 PR 上实际运行 CI 时确认；若镜像变动导致 gcc 缺失，在 CI 中补装 MinGW。CI 编译通过不代表 cgo 绑定能在 Windows 实机加载运行库，该验证归 Windows 测试（见[实施清单的开发与 Windows 测试方式](specs/scanner-implementation-plan.md#开发与-windows-测试方式)）。

## 第三方资源下载

OCR 运行库与模型不进 Git，由 `tools/fetchdeps` 按固定 URL 与 SHA-256 下载（固定组合与摘要在 [T3](specs/t3-ocr-runtime-model-samples.md#固定组合与制品静态核查)，VC++ 运行库不在此工具范围，见[实施清单的工程约定](specs/scanner-implementation-plan.md#已定的工程约定)）。在仓库根目录运行，不需要 API key：

| 命令 | 用途 |
| --- | --- |
| `go run ./tools/fetchdeps` | 下载全部平台的资源（双平台打包所需）。 |
| `go run ./tools/fetchdeps -platform current` | 只下载当前平台所需的资源（公共模型仍会下载）。 |
| `go run ./tools/fetchdeps -platform windows/amd64` | 只下载指定平台的资源，交叉打包前使用。 |

产物与缓存位置（均被 `.gitignore` 忽略，不要手工编辑）：

| 路径 | 内容 |
| --- | --- |
| `resources/windows-amd64/onnxruntime/` | Windows 的 `onnxruntime.dll`、`onnxruntime_providers_shared.dll` 与 ORT 的 `LICENSE`、`ThirdPartyNotices.txt`。 |
| `resources/darwin-arm64/onnxruntime/` | macOS 的 `libonnxruntime.1.29.0.dylib` 与 ORT 的 `LICENSE`、`ThirdPartyNotices.txt`。 |
| `resources/models/` | 两平台共用的识别模型 `PP-OCRv6_rec_small.onnx`。 |
| `.fetchdeps-cache/` | 下载缓存（保存压缩包原文件；摘要一致时直接复用，不访问网络）。 |

行为约定：已存在且摘要一致的文件跳过；压缩包与每个解出文件都按固定 SHA-256 校验，不符立即失败；所有文件先写临时文件、校验通过后改名，失败不会留下半成品。目录名对应 MyGo 的 `resources/` 约定：平台子目录的内容只随对应平台的应用发布，其余随所有平台发布。

## 与 Windows 测试方式的关系

用户不在 Windows 上安装开发工具，Windows 上的功能与安装测试使用 GitHub Actions 构建的安装包（正式发布与开发期测试的 Windows 制品均来自 GitHub Actions，约定见[实施清单](specs/scanner-implementation-plan.md#开发与-windows-测试方式)）。

Mac 上的 Windows 交叉编译检查只用于尽早发现编译与平台隔离问题：交叉编译通过不代表能在 Windows 上运行，VC++ 运行库（DLL 来源与实际加载）也不在本地交叉编译的覆盖范围内，需要快速排查非 VC++ 问题时可另行交叉编译免安装包供测试。只能在 Windows 实机验证的事项见实施清单第 1 节。
