# 2026-11-14 依赖升级与本地验证

本轮在 `chore/upgrade-mygo-dependencies` 完成本地可评审修改，base / HEAD 均为 `db167c32d53963e2001e9660fb21a54d7a08be00`，没有 stage、commit、push、PR 或 merge。旧 M1 安装、自动更新及 `ef04560` 的 W3 证据保留原版本事实，不作为新组合的实测结果。

当前状态以[第3轮核心收敛记录](#r3-核心范围收敛与最小验证)为准：用户收窄边界要求后，核心适配与 Windows 最小回归27/27通过，建议按新标准复审R1-1，尚无merge/发布许可。此前各轮及第3轮前段的失败、方案和冻结均按当时事实保留，不冒充当前实现保证。

## 版本与来源

2026-11-14 02:37:02 UTC 开始复核上游；Go 下载清单快照时间为 02:38:16.368523 UTC。GitHub 查询使用进程级 LoTwT 凭据并验证 `/user`，远端 main 与本地 base 一致；没有 fetch 或修改源 main。发布说明、tag 引用、Go 模块查询和资产元数据已随会话保留，均使用固定 tag / commit，没有跟随 main/master。

| 项目 | 升级前 | 当前 | 官方依据 |
| --- | --- | --- | --- |
| Go 项目工具链 | 1.27.3 | 1.27.4 | [官方 stable 下载清单](https://go.dev/dl/?mode=json)，由 GOTOOLCHAIN 自动选择；不改全局 Go 安装。 |
| MyGo | v0.6.2 | v0.9.0 | [稳定发布](https://github.com/egoist/mygo/releases/tag/v0.9.0)，2026-11-11，commit `4811a2869b5a66d2b7e21a9eb5993fcff544460b`。 |
| onnxruntime_go | v1.36.0 | v1.38.0 | [固定源码](https://github.com/yalue/onnxruntime_go/tree/75993804ebf75bfc35bd514e3d19cff35146545a)，模块 tag 经 Go proxy / sumdb 验证；上游没有 GitHub Releases。 |
| ORT 原生库 | 1.29.0 | 1.32.0 | [官方稳定发布](https://github.com/microsoft/onnxruntime/releases/tag/v1.32.0)，2026-11-13，commit `fab30a46491a40f5e4c968b80e8a59acbbb91b53`。 |
| PP-OCRv6 small / 字符表 | RapidOCR v3.10.0 固定模型 | 相同字节 | [v3.10.0 清单](https://github.com/RapidAI/RapidOCR/blob/7cfd3d9ef786a2e538ca30df700325e72e85b5f5/python/rapidocr/default_models.yaml)，仍是最新稳定发布，模型 SHA `6f327246b50388f3c176ae304bd95767ea6dc0c9ae92153ef8cbe210b3c14884`。字符表来自同一模型元数据，预处理、样本及阈值不变。 |
| actions/checkout | v7.0.1 | 不变 | [官方 tag](https://github.com/actions/checkout/releases/tag/v7.0.1)，`3d3c42e5aac5ba805825da76410c181273ba90b1`。 |
| actions/setup-go | v7.0.0 | 不变 | [官方 tag](https://github.com/actions/setup-go/releases/tag/v7.0.0)，`b7ad1dad31e06c5925ef5d2fc7ad053ef454303e`。 |
| actions/upload-artifact | v7.0.2 | 不变 | [官方 tag](https://github.com/actions/upload-artifact/releases/tag/v7.0.2)，`cf430e030ddbb5b0abf93d22962f4752f3646cd9`。 |

三个 Actions 的最新稳定发布与 tag commit 都已复核，现有 pin 无需改动。以下列出完整解析模块图；实际使用范围按 Mac / Windows 两平台的应用包与 MyGo CLI 依赖核对。MyGo 新增 RPM 构建链引入的压缩和密码库是 CLI 的传递依赖，不为本产品增加 Linux 发布目标。仅依赖测试或未使用功能的模块保留上游图，不为了追新强行增加 require；typesetting-utils 没有可采用的稳定 tag，不改为新的浮动伪版本。

| Go 模块 | 升级前解析版本 | 升级后解析版本 | 处理依据 |
| --- | --- | --- | --- |
| [github.com/ProtonMail/go-crypto](https://proxy.golang.org/github.com/!proton!mail/go-crypto/@v/v1.5.2.info) | `原模块图无此项` | `v1.5.2` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [github.com/bwesterb/go-ristretto](https://proxy.golang.org/github.com/bwesterb/go-ristretto/@v/v1.2.4.info) | `原模块图无此项` | `v1.2.4` | 当前模块图稳定 tag |
| [github.com/cloudflare/circl](https://proxy.golang.org/github.com/cloudflare/circl/@v/v1.6.5.info) | `原模块图无此项` | `v1.6.5` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [github.com/davecgh/go-spew](https://proxy.golang.org/github.com/davecgh/go-spew/@v/v1.1.1.info) | `原模块图无此项` | `v1.1.1` | 当前模块图稳定 tag |
| [github.com/ebitengine/purego](https://proxy.golang.org/github.com/ebitengine/purego/@v/v0.11.3.info) | `v0.11.1` | `v0.11.3` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [github.com/egoist/mygo](https://proxy.golang.org/github.com/egoist/mygo/@v/v0.9.0.info) | `v0.6.2` | `v0.9.0` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [github.com/go-text/typesetting](https://proxy.golang.org/github.com/go-text/typesetting/@v/v0.3.5.info) | `v0.3.5` | `v0.3.5` | 当前无更新候选；实际应用或 MyGo CLI 依赖 |
| [github.com/go-text/typesetting-utils](https://proxy.golang.org/github.com/go-text/typesetting-utils/@v/v0.0.0-20260419141703-4ffe8874dabc.info) | `v0.0.0-20260419141703-4ffe8874dabc` | `v0.0.0-20260419141703-4ffe8874dabc` | 保留上游模块图，未使用包/依赖测试的新候选 v0.0.0-20261009215314-bb530f4842f3 未加入 require |
| [github.com/hajimehoshi/go-mp3](https://proxy.golang.org/github.com/hajimehoshi/go-mp3/@v/v0.3.4.info) | `v0.3.4` | `v0.3.4` | 当前无更新候选 |
| [github.com/icza/bitio](https://proxy.golang.org/github.com/icza/bitio/@v/v1.1.0.info) | `原模块图无此项` | `v1.1.0` | 当前模块图稳定 tag |
| [github.com/jfreymuth/oggvorbis](https://proxy.golang.org/github.com/jfreymuth/oggvorbis/@v/v1.0.5.info) | `v1.0.5` | `v1.0.5` | 当前无更新候选 |
| [github.com/jfreymuth/vorbis](https://proxy.golang.org/github.com/jfreymuth/vorbis/@v/v1.0.2.info) | `v1.0.2` | `v1.0.2` | 当前无更新候选 |
| [github.com/klauspost/compress](https://proxy.golang.org/github.com/klauspost/compress/@v/v1.20.4.info) | `原模块图无此项` | `v1.20.4` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [github.com/mewkiz/flac](https://proxy.golang.org/github.com/mewkiz/flac/@v/v1.0.14.info) | `原模块图无此项` | `v1.0.14` | 当前模块图稳定 tag |
| [github.com/mewkiz/pkg](https://proxy.golang.org/github.com/mewkiz/pkg/@v/v0.0.0-20250417130911-3f050ff8c56d.info) | `原模块图无此项` | `v0.0.0-20250417130911-3f050ff8c56d` | 保留上游模块图，未使用包/依赖测试的新候选 v0.0.0-20260703220044-4fb89b18cc87 未加入 require |
| [github.com/mewpkg/term](https://proxy.golang.org/github.com/mewpkg/term/@v/v0.0.0-20241026122259-37a80af23985.info) | `原模块图无此项` | `v0.0.0-20241026122259-37a80af23985` | 由上游模块图选择，未手动跟随分支 |
| [github.com/pmezard/go-difflib](https://proxy.golang.org/github.com/pmezard/go-difflib/@v/v1.0.0.info) | `原模块图无此项` | `v1.0.0` | 当前模块图稳定 tag |
| [github.com/stretchr/testify](https://proxy.golang.org/github.com/stretchr/testify/@v/v1.11.1.info) | `原模块图无此项` | `v1.11.1` | 保留上游模块图，未使用包/依赖测试的新候选 v1.12.1 未加入 require |
| [github.com/tphakala/go-aac](https://proxy.golang.org/github.com/tphakala/go-aac/@v/v0.7.1.info) | `原模块图无此项` | `v0.7.1` | 保留上游模块图，未使用包/依赖测试的新候选 v0.7.2 未加入 require |
| [github.com/tphakala/simd](https://proxy.golang.org/github.com/tphakala/simd/@v/v1.11.0.info) | `原模块图无此项` | `v1.11.0` | 保留上游模块图，未使用包/依赖测试的新候选 v1.16.0 未加入 require |
| [github.com/ulikunitz/xz](https://proxy.golang.org/github.com/ulikunitz/xz/@v/v0.5.17.info) | `原模块图无此项` | `v0.5.17` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [github.com/yalue/onnxruntime_go](https://proxy.golang.org/github.com/yalue/onnxruntime_go/@v/v1.38.0.info) | `v1.36.0` | `v1.38.0` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [go.digitalxero.dev/rpm](https://proxy.golang.org/go.digitalxero.dev/rpm/@v/v0.3.4.info) | `原模块图无此项` | `v0.3.4` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [golang.org/x/crypto](https://proxy.golang.org/golang.org/x/crypto/@v/v0.59.0.info) | `原模块图无此项` | `v0.59.0` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [golang.org/x/image](https://proxy.golang.org/golang.org/x/image/@v/v0.48.0.info) | `v0.46.0` | `v0.48.0` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [golang.org/x/mod](https://proxy.golang.org/golang.org/x/mod/@v/v0.42.0.info) | `v0.41.0` | `v0.42.0` | 保留上游模块图，未使用包/依赖测试的新候选 v0.43.0 未加入 require |
| [golang.org/x/net](https://proxy.golang.org/golang.org/x/net/@v/v0.62.0.info) | `v0.59.0` | `v0.62.0` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [golang.org/x/sync](https://proxy.golang.org/golang.org/x/sync/@v/v0.25.0.info) | `v0.24.0` | `v0.25.0` | 当前模块图稳定 tag |
| [golang.org/x/sys](https://proxy.golang.org/golang.org/x/sys/@v/v0.50.0.info) | `v0.49.0` | `v0.50.0` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [golang.org/x/term](https://proxy.golang.org/golang.org/x/term/@v/v0.48.0.info) | `原模块图无此项` | `v0.48.0` | 当前模块图稳定 tag |
| [golang.org/x/text](https://proxy.golang.org/golang.org/x/text/@v/v0.44.0.info) | `v0.43.0` | `v0.44.0` | 当前模块图稳定 tag；实际应用或 MyGo CLI 依赖 |
| [golang.org/x/tools](https://proxy.golang.org/golang.org/x/tools/@v/v0.52.0.info) | `v0.51.0` | `v0.52.0` | 当前模块图稳定 tag |
| [gopkg.in/yaml.v3](https://proxy.golang.org/gopkg.in/yaml.v3/@v/v3.0.1.info) | `原模块图无此项` | `v3.0.1` | 当前模块图稳定 tag |

## 兼容处理与用户可见影响

已读从 v0.6.2 之后到 v0.9.0 的全部已发布稳定说明：v0.6.3、v0.6.4、v0.7.0、v0.7.1、v0.7.2、v0.7.3、v0.7.4、v0.8.0、v0.8.1、v0.8.3、v0.9.0，并对照 v0.9.0 固定源码与本项目调用。

| 范围 | 结论与处理 | 证据边界 |
| --- | --- | --- |
| Relaunch / 自动更新 | v0.9.0 变为 `Relaunch(args ...string)`；入口用无参数闭包适配原 `func()` seam，保留原参数及“保存完成后再发起重启”的顺序。项目直接调用 `mygo.Updater.Check` / `Update.Install`，未接入 `plugins/updater.OnInstalled`，不会新增一条重复重启回调。 | 现有 relaunch handoff、更新及生命周期测试通过；真实签名更新与渠道未重测。 |
| OS 注销/关机 | `SessionEnding` 不可取消。R1 修正后先关闭服务写入接纳并取消输入，再启用 macOS 终止专用 native gate，最后同步排空已接纳的扫描/校对保存；后续 OnClose 不隐藏、不弹提示。普通退出保留异步收尾及原生资源正常注销。未提交草稿不自动提交，OS 仍可强制结束进程。 | 生命周期和服务测试，以及真实 Carbon/主队列依赖的独立子进程红绿测试；没有真实注销/关机或 Mac UI 验收。 |
| 输入与焦点 | v0.8+ 的 modifier / KeyUnknown 也进入 HandleInput。项目唯一该类处理器只把 Space / Enter 视作写入动作，其余键返回 false；新增 Alt、Shift、Caps Lock 断言，保持浏览保护与焦点。 | UI headless 测试通过；IME、Alt 菜单和真实键盘焦点仍需目标机验证。 |
| 原生 UI | 上游包含失焦 accent 灰化、整体 opacity 合成、只响应主按钮的拖动、取消按压不提交、焦点滚动及文本/菜单绘制修复。没有添加覆盖这些默认值的产品重设计。 | 现有界面测试通过，不宣称像素或交互完全等价；真实绘制、缩放和输入法未验收。 |
| 托盘/通知/单实例 | 保留现有托盘创建失败防御、单击恢复、通知 ID 与关闭顺序。v0.9.0 single_instance.go 与 v0.6.2 相同；Windows 托盘菜单定位和通知原生实现有上游变化。 | 生命周期/托盘/通知测试通过。W3 托盘与通知缺口没有由这轮升级补齐。 |
| 资源/原生模块 | 平台资源仍合并到应用资源根，cgo 仍须显式启用。新 CLI 不分发 resources 内 Go 源码；本项目资源只有模型、运行库、许可和图像，没有使用新实验原生模块或 SQLite 插件。 | 两平台实际打包布局核对，Mac 随包识别；不引入 Swift/Rust/Zig 的新构建步骤。 |
| 安装包命名 | v0.7+ 使用 slug：`youkai-setup-0.0.1-amd64.exe`、`youkai-0.0.1-arm64.dmg`。Release 必需资产校验和当前文档已同步；旧发布资产保留原名。MyGo 还生成 install.ps1 / install.sh / SHA256SUMS。 | 实际生成安装包与 DMG；没有发布或运行辅助安装脚本。 |

## OCR 原生制品

两个官方 archive 的实际字节摘要与 GitHub 资产 digest 一致，全部随包成员（主库、provider shared、LICENSE、ThirdPartyNotices）分别复算并固定在 `tools/fetchdeps/deps.go`，没有复用旧库哈希。完整成员大小与摘要随会话保留；当前运行库名由 `internal/ocrlayout` 统一维护，识别集成测试也使用该名称，避免旧 dylib 名导致静默跳过。

| 官方 archive | SHA-256 |
| --- | --- |
| onnxruntime-win-x64-1.32.0.zip | `aaa1d04fcf86bd31e84b90bc0cbde80e632bd0720dcc5d8ba05c4fe45c09d127` |
| onnxruntime-osx-arm64-1.32.0.tgz | `8ddca23f532b9d4fbb279ba9b08dab6c0ac94fdc9aec2eb44a1db7049216c043` |

yalue v1.38.0 请求 C API 31；Mac 实际库报告版本 1.32.0，GetApi(31) 和 GetApi(32) 均非空，真实 OCR 集成通过。Windows 首轮冻结 EXE 后由独立会话完成一次真实加载，具体制品绑定及限制见下文“Windows 首轮冻结制品自检”。

Mac dylib 为 arm64、47,589,736 字节，minos 仍为 14.0，依赖系统 framework / dylib（包括 CoreML、Metal、CFNetwork 等）。Windows 主 DLL 为 x64、20,602,168 字节，provider shared 为 21,816 字节；导入仍包括四个 VC++ v14 DLL、系统 API / UCRT，PE linker 14.44、OS/subsystem 6.0。这些字段不等于产品系统支持承诺。Mac 最低候选仍为 14.0；Windows 的支持范围与 CRT 实际版本/加载继续由 T7 和新的 Windows 构建验证。

ORT LICENSE 与 ThirdPartyNotices 已随新包同步，随包第三方声明已更新。模型独立 LICENSE 材料未归档是原有发布前待办，本轮保留，不将运行库升级转成模型变更。

## 首轮本地验证（冻结快照）

最终 15 个有测试的包通过，2,473 个测试/子测试通过，9 个现场回放测试 Skip，0 个失败。无测试文件的包不计入 Skip 测试数。

| 命令或检查 | 最终结果 |
| --- | --- |
| `go mod verify` | exit 0。 |
| `go mod tidy`，再次比较 go.mod / go.sum SHA | exit 0，字节不变。 |
| `gofmt -l .` | exit 0，无输出。 |
| `go vet ./...` | exit 0。 |
| `go test -json ./...` | 最终 exit 0，包含真实 OCR 集成。初次运行失败另记于下文，未隐藏。 |
| `go tool mygo vet .` | exit 0。 |
| `go build ./...` | exit 0。 |
| `CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 go build ./...` | exit 0，只证明编译。 |
| `go test -json ./internal/recognition ./internal/app ./internal/ui ./cmd/youkai` | exit 0，1,174 个测试/子测试通过、无 Skip。 |
| `go test -v ./cmd/youkai -run 'SessionEnding\|Quit\|Relaunch\|WindowClose\|Notification\|Tray'` | exit 0。 |
| `go run ./tools/fetchdeps`，再次运行 | 两次 exit 0，第二次所有资源摘要一致并跳过。 |
| `MACOSX_DEPLOYMENT_TARGET=14.0 CGO_ENABLED=1 go tool mygo build -platform darwin/arm64 -sign -` | exit 0，生成 app 与 DMG。 |
| `YOUKAI_SELFTEST=1 <app>/Contents/MacOS/Youkai` | exit 0，识别 `等级15/15`，score 1.000；复制至中文/空格路径并从 /private/tmp 启动也通过。 |
| `codesign --verify --deep --strict <app>` | exit 0。 |
| `CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go tool mygo build -platform windows/amd64` | exit 0，生成 EXE 与 NSIS 安装包。 |
| 双平台资源布局、模型 SHA、原生库静态依赖、Info.plist | 通过；app LSMinimumSystemVersion=14.0。 |
| 三个 workflow YAML 解析、`git diff --check` | 通过。没有远端 CI 运行。 |

初次 `go test` 的识别集成因测试硬编码旧 dylib 名而跳过，已改为使用统一布局并在最终套件实际运行；另外 `TestSinglePageUndecidedBaselineStillRoundTrips` 首轮出现一次 detailUnattributed（slot 0 的稳定帧未在测试的 300ms 动作预算内就绪）。采集源码、测试阈值和断言均未修改。新旧依赖各单独 `-count=10` 均 exit 0；最后整套也通过。目前只能确认一次时序失败后未复现，不能证明是旧问题或排除依赖/工具链影响。保留首轮日志与对照证据，交独立评审判断，不靠放松断言掩盖。

最终剩余 9 个 Skip 均为外部现场回放输入未提供：`YOUKAI_SCROLL_REPLAY` 6 项、`YOUKAI_M2_SAMPLES` 3 项。未访问保留的 W3 资料来填充它们：

- `TestScrollReplayFieldSequencesSettleUnderStage2`
- `TestScrollReplayGapLimitedFieldSequencesSettleOnScrollbar`
- `TestScrollReplayGapLimitedFieldSequencesStayUndecidedWithoutScrollbar`
- `TestScrollReplayGapLimitedFieldSequenceCommitsAndRecordsBasis`
- `TestScrollReplayFieldSequencesStillFailUnderLegacy`
- `TestScrollReplayFieldAnchorPairsVerifyDisplacement`
- `TestReplayManagementAndAvatarSamples`
- `TestReplayFieldSamples`
- `TestReplayViewportFromFieldFrames`

首轮实现会话没有运行 Windows 机器、安装版、游戏输入或 W3 扫描，也没有启动 Mac UI。随后独立 Windows 会话只验证了下文绑定 EXE 的 ORT/VC++ 加载；UI/IME、系统注销/关机、托盘/通知真实行为、安装/协议注册及签名自动更新仍需另行安排。以上证据不替代这些验收，不宣布新系统支持范围或 W3/M4 通过。

## Windows 首轮冻结制品自检

以下复用主控转交的独立 Cissia 会话 `7cea2b65-40b8-4f57-9720-ac477dd2c28d` 完成报告。本 Mac 会话没有读取 Windows 原始日志或复制其用户数据。

- Windows 11 build 22631，AMD64；独立 TEMP staging，仅运行一次 `YOUKAI_SELFTEST=1`，842ms、exit 0，识别 `等级15/15`、score 1.000。
- 从 EXE 旁加载 ORT/模型，cwd 不同；使用 System32 四个 x64 CRT 14.44.35211.0，签名 Valid，无复制安装。数据根元数据前后不变，没有 UI、扫描或游戏输入。
- EXE SHA-256：`c3ac79f70d6ac4301efd7cad3e9df21af91febef431ef4590fb01216390be5dc`；ORT DLL：`348c3d29d6995eee37e3451825eaddb7ea7400be3afda6ca2286b12bc775f042`；模型：`6f327246b50388f3c176ae304bd95767ea6dc0c9ae92153ef8cbe210b3c14884`。
- Windows 报告位置为该会话的 `%TEMP%\youkai-dep01-windows-smoke-20261114\7cea2b65-40b8-4f57-9720-ac477dd2c28d\report.md`，另含 `logs/selftest-result-1.json`、`selftest-stderr.txt`、`crt-probe.json`。
- VERSIONINFO 0.0.1 是 `mygo.json` 的应用版本，MyGo0.9.0 是框架依赖版本，两者无需相同。主控直接检查同 SHA EXE 的 `go version -m`，确认 Go1.27.4、MyGo0.9/onnxruntime_go1.38、vcs=db167c3、modified=true。本轮没有改应用版本。

此结果关闭首轮制品的 Windows 依赖加载缺口。R1 重建后 EXE 哈希变化，不能称修正后二进制已在 Windows 运行，也不能推导 GUI、安装器保护或系统终止已验收。

## R1 修正与剩余 Required

本轮为 R1 后第 1 个 fix round，执行会话 `ccff0f22-7fbd-4588-bf80-224f4f6fdc74`。承接冻结的 22 个未暂存 tracked 文件和 1 个新报告，全部 23 文件及 871 全源码 SHA 在开工前匹配；没有提交。新证据单独保存于本会话 `dep01-r1-fix-evidence/`，旧冻结证据只读保留。

| 项目 | 本轮处理 | 状态与验证边界 |
| --- | --- | --- |
| R1-1 安装/卸载绕过退出保护 | 已复核 MyGo 原版真实 NSIS 输入与生成 `install.ps1`。官方 include 初始化点早于最终目录选择，安装/卸载 hook 晚于内置 CloseApp；辅助 PS1 还有独立强杀路径。本轮未替换生成器、未加入不能覆盖真实入口的提前 guard。 | **Required 未关闭，待主控决定构建适配方案。** 当前 NSIS/PS1 仍有强杀，不能安装或发布。受控 Windows fixture 已另行准备但本轮未运行。 |
| R1-2 已接纳校对写入与根锁顺序 | 服务层关闭新写入接纳，排空已接纳修订、偏好及缓存/旧日志清理，连接扫描最终保存，再做 OCR 清理及根锁释放。记录删除和 RetrySave 保持服务锁内串行并在退出中拒绝。普通退出失败且根锁仍持有时重新允许修复保存；并发退出加入同一尝试，已缓存失败的退出也排空后来接纳的写入。不可取消系统终止失败后仍保持接纳关闭。 | 冻结副本真实 red：阻塞 SaveRevision 时 Quit 提前返回且根锁可重获，退出后仍可写；新实现 green。保存失败、修复/重试、重复/并发退出及偏好接纳有回归测试。未提交草稿仍不自动提交。 |
| R1-3 slug 安装包 selector 与上传顺序 | Package 检查、清单、artifact glob 和 Release 检查统一小写 slug；Release 移除构建 `-upload`，所有现有校验及识别自检后才上传，清单最后。 | 实际执行冻结/新 selector，旧为 red，新选择真实小写安装包并生成正确摘要条目；最终上传演练使用本地显式 mock，验证有无 delta、坏摘要/缺安装包拒绝、清单最后。未运行远端 CI，没有上传成功记录；早期隔离问题另记于下文。 |
| R1-4 macOS 主队列退出依赖 | 不可取消系统终止先停止输入及写入接纳，再启用不可恢复的 native gate，唤醒在途主队列等待，拒绝新热键注册/前台查询/激活并禁用回调。Carbon 引用保持存活交 OS 回收，后台注销不再排主队列。普通退出路径不启用 gate。 | 真实 Carbon 注销、在途/后续注册、前台查询和激活在独立原生子进程复现红绿：冻结代码约 1.95s 后失败，新代码立即返回；无游戏输入、用户资料或系统注销。实际 OS 终止及 GUI 仍未运行。 |

R1-1 的方案条件：安全修正至少要接管最终文件操作前的 NSIS CloseApp/un.CloseApp 行为和生成 PS1 的独立退出段，并保证原生成器、静默入口、最终目录与进程重启都无法走回强杀。仅增加 include 检查不能达到这个要求。可选路线是项目内维护版本绑定的生成脚本适配及统一 Windows 构建入口，或者完整自建安装器/辅助脚本；前者改动较少但需要维护真实模板匹配、fail-closed 分支、制品摘要/签名顺序和入口约束，后者长期维护成本更高。建议主控选前者并明确这层维护边界后继续；本轮未擅自改为 fork/vendoring/replace、回退依赖、关闭安装能力或接受数据损失。

一次上传逻辑演练的 PATH mock 被 Lody 注入的 `gh` shell 函数覆盖，向演练目标 `LOCAL/NO-NETWORK` 发起上传调用后返回 `release not found`，没有上传成功记录。该次日志保留为 `r1-3-upload-unisolated-*.log`。之后演练脚本显式定义本地 `gh` 记录函数，正反用例均只落本地参数日志，未再调用外部上传工具。没有对真实仓库执行成功的发布写入。

R1 最终 `go test -json ./...` 为 15 个测试包、2,487 个测试/子测试通过、9 个原有现场回放 Skip、0 失败。`go mod verify`、`go mod tidy -diff`、gofmt、`go vet ./...`、MyGo UI vet、原生及 Windows cgo 编译通过。服务/入口/平台/UI 的 race 检查通过；补充终止接纳不恢复的断言后，服务与入口再次 race 通过。没有改采集源码、时序阈值或既有断言，首轮 detailUnattributed 未归因的不确定性继续保留。

R1 双平台实际打包通过，制品隔离在本会话 `dep01-r1-fix-evidence/artifacts/`。Mac 签名严格验证、DMG 校验、最低系统字段 14.0 及从 `/private/tmp` 启动的随包 OCR 自检通过（`等级15/15`，score 1.000）；两平台随包原生库、模型、LICENSE、ThirdPartyNotices 与固定资源逐字节一致。新 Windows EXE SHA 为 `a43e75cf7353479ae3790e71b5376b1a0e8b941da0f8ea38e572d4c2d082374d`，未在 Windows 运行；安装包 SHA 为 `f8dbc6485c20407a74a4923358a5e06c924a67b2325f21ca85fde342c234a37a`，仍含 R1-1 未修复行为。完整制品清单 `artifacts-manifest.json` 的 SHA 为 `71dfe905df3861160d836ae7c007a88ef0e93080326f14ad8fd402add334b623`。

本机没有 7z，R1 未执行安装包内部列表检查；NSIS 编译与 staging 资源核对通过，不等于已跑 Windows runner 的完整 Package/Release 流程。本轮 Windows 交叉包仍没有 runner 提供的四个 CRT，不能作为正式交付包。未提供更新私钥，没有生成或上传新的签名更新档案，更新源/公钥、更新格式和安装能力未关闭。

## R2 第2修正轮：原生 guard 前置证明待 Windows

执行会话 `df495f0e-40b1-4e7e-af6b-13236e56e63f`，新会话接管同一 DEP-01 隔离树，按本轮明确交接计第 **2/2** 个 fix round；未恢复或派单旧会话、未派子代理。开工独立核验 37/37 变更文件及 878/878 全源码与 R1 冻结摘要、文件集合一致，HEAD/branch/index 均符合交接，主检出保持同 SHA 的干净 main。

**R1-1 未关闭，正式构建适配未实施，当前原版产物仍不能安装或发布。** R2 独立评审已建议关闭 R1-2/3/4，本轮未扩修这些区域或其他 Follow-up。主控已选择“项目内版本绑定最小构建适配”，但要求先证明旧版未协作应用的 OS 排斥机制；本机是 Mac，不能完成必要的 Windows 原语证明，未把编译或源码推断当作该门槛已通过，也没有据此把候选接入正式入口。

本轮完成的独立工作：

- 重新追踪 MyGo0.9 的真实执行链：编译器先找 PATH、Windows 标准安装目录和版本缓存，缺少时自动下载已固定摘要的 NSIS3.13；签名探测执行 `-VERSION`。`nsis.go` 写临时 NSIS 后执行真实编译器，`!uninstfinalize ... = 0` 在编译中签卸载器并传播失败；之后 `build.go` 才签安装器、生成归档/签名/delta/manifest，再生成辅助 PS1，末尾汇总 SHA256SUMS。原版两个关闭函数、PS1 的关闭块及吞错外层 catch 均逐字抽取到探针并记录摘要；没有修改上游模块或共享缓存。
- 排除仅新增命名 mutex 或再次枚举作为证明。MyGo 的既有单实例机制使用 Unix socket；非地址占用错误、耗尽重试仍可返回允许启动，不能当作安装器的强制排斥协议。文件共享规则约束打开句柄，尚不等于已证明覆盖完整 loader/写入/删除周期。
- 为候选“两阶段句柄交接”准备原生受控探针：第一阶段无修改地请求 EXE 写访问，重叠取得禁止读取但允许写入/删除的零访问句柄；实验检查已映射进程拒绝、有效 EXE 重启被阻、实际 NSIS File 原位覆盖、Delete pending期间同名创建被拒，以及释放后恢复启动/删除。**这只是待证候选，不是选定或已通过的生产 guard。** Windows 映射与交接、首次安装、真实最终目录、其他可执行文件、生产持锁生命周期等仍待证/待实现。
- 新包附原始 Win32 fixture 源码及本轮新编译 EXE、原版完整 NSIS 两函数和 PS1 helper/退出块/外层 catch red probes、实际 NSIS 写入/删除探针、README、文件大小和 SHA256。只在自己的随机 TEMP 下操作摘要绑定的 fixture；不含真实 Youkai 安装载荷、网络、用户应用注册表/数据、协议、快捷方式或真实进程注入。原版 red 的预期为运行后安全断言失败，尚未在 Windows 观察；原语 green 同样未运行。

证据全部在本会话 `dep01-r2-fix-evidence/`，包括 `DEP01-R2-HANDOFF.md`、`R1-1-decision.md`、`template-origins.json`、`probe-build.json`、`windows-feasibility/` 和其 ZIP。没有最终转换模板，不能提供“从最终模板抽取”的 green 包；该缺项在交接逐条标明。正式 Package/Release/本地构建接口及签名摘要校验未修改，不能宣称八项安装约束已经满足。

本轮仅更新上述当前状态文档，应用、依赖、模型/字典/采集阈值和已关闭区域均保持 R1 冻结字节。新验证限于探针交叉编译、NSIS 编译、抽取来源/调用位置/包边界核对和文档 diff 检查；首次原生探针因 UNICODE 宏重复定义在 `-Werror` 下编译失败，已加宏保护并通过，失败日志保留。原 R1 的 2487 Pass/9 Skip、服务/mac race、实际包/OCR 证据仅按原范围复用，不覆盖本轮新 EXE 或 OS guard；无完整安装/升级实测授权，也未执行任何真实安装或上传演练。

下一步推荐主控按既定安排另开全新 Windows 会话先执行原语包，返回绑定摘要的真实结果，再决定最小适配能否继续。当前既未证明最小 guard 不可行，也未证明它可行；不能据未知状态改应用启动、权限或已有数据行为。需应用协作的方案对已安装旧版首次升级不能仅靠新 mutex 解决，全面自建 installer 也不会自动消除旧进程重启竞争。第2修正轮仍留 Required，未自行开启第三轮、降低验收或接受数据损失；后续实现轮次按既定上限交主控/用户决定。

## 首轮产物与交接

- `build/darwin-arm64/Youkai.app`、`youkai-0.0.1-arm64.dmg`、`install.sh`。
- `build/windows-amd64/Youkai.exe`、`youkai-setup-0.0.1-amd64.exe`、随包模型/运行库/许可、`install.ps1`。
- `build/SHA256SUMS`；会话另保留全部制品文件的大小与 SHA-256 清单。

本地包使用仓库原更新公钥/源，未提供私钥，MyGo 警告并跳过签名更新档案；自检显示 `updates=enabled`，但在数据根、占用锁与 UI 创建前退出，不启动更新检查。Windows 本地交叉包没有 runner 提供的四个 VC++ DLL，不能交付替代正式 Package/Release 制品。源码内容通过冻结 SHA 清单交给新独立 Sol 会话评审；本轮没有提交、发布或清理旧资料。

## R3 第3轮：正式适配与卸载启动层阻塞

以下是用户追加收窄要求之前的历史阶段；其 writer 实现已撤下，当前源码、验收范围和状态见后文“核心范围收敛与最小验证”。历史冻结、附件和失败记录未覆盖。

负责人 session `5dd744ed-dfa4-40ac-a17d-40acae0fe2e1`，主控仍为 `1b78b7aa-a1cd-491b-8215-01f34b1a2c80`。这是用户明确授权的第3轮，不重置轮数；没有创建实现者或评审子任务，只按窄授权新建两次 Windows TEMP 执行会话。开工 37/37 变更文件及 878/878 全源码与 R2 最新冻结清单匹配，HEAD/branch/index 及主 main 符合交接。未 stage/commit/push/PR/merge/fetch、真实上传或操作旧锁注册、retained/W3/用户资料。

实际实现位于 `tools/mygobuild/`、`packaging/require-safe-installer.nsh` 与 `mygo.json` 的 sentinel 配置。Package、Release、本地交叉命令共用入口。MyGo 保持原 v0.9.0，无 fork/vendor/replace 或共享 module cache 修改；更新公钥、源、渠道、格式与模型/字典/采集阈值未改。

- 绑定 6 个真实上游生成/签名相关源文件及完整规范化 NSIS/PS1 模板。仅允许实际输出/资源列表/版本/渠道等明确变量槽；两个完整 CloseApp、最终 Section 结构与单个准确签名命令均受约束，漂移、重复转换或缺失即失败。直接上游 Windows 构建在 sentinel 编译期拒绝未适配模板。
- NSIS 先在自己的插件 TEMP 提取完整载荷和真实卸载器，再按最终 `$INSTDIR` 取得目录及现有文件句柄；既有文件全部取得成功后才预留新增文件。原生 guard 保留 `GENERIC_READ|GENERIC_WRITE|DELETE`、`FILE_SHARE_READ`，由同一句柄写入/flush 或标记删除，随 NSIS 完整区间存活；不改 ACL，不用应用协作锁，不枚举或强杀进程。普通本地 NTFS 外、reparse/硬链接/歧义别名/未知权限均拒绝。未发现的新增文件不会被通配递归删除。
- PS1 删除完整独立强杀/关窗助手与退出段，严格传播目标发现错误、NSIS 任意非零和外层异常；通过 `/D` 交给实际 NSIS guard，在交接空隙启动的旧进程也必须先被内核占用检查拒绝。
- 编译器先解析真实绝对路径，再放置局部 wrapper；透传 `-VERSION`、签名环境与退出码，保留 Windows 同版本/同摘要 NSIS 自动下载。转换发生在编译/卸载器签名前，随后仍由 MyGo 签安装器、生成原格式归档/delta/manifest；实际 PS1 生成后才转换，最后校验未改制品并重算脚本摘要。无更新的 Package 也生成安装包摘要。当前本地未提供私钥，真实签名更新链未执行。

Windows 原语会话 `2f799c3e-a4a7-4ef8-86f7-1627b0b1886c`，operation `dep01-r3-writer-primitive-5dd744ed-01`：附件 SHA `edcbfe61433d3e02b73340a5b0e9bf719d599cde6c9a8c82a93569b267571c73`；单次 exit 0 / 21.791 s / 253 PASS。有效映像在持续 writer 下不能启动；实际 NSIS 暂存、同句柄写入、首次缺 EXE、删除 pending 禁止重建及释放后恢复启动均通过。环境 Win11 22631 x64 / NTFS / 标准用户。前一轮 access0 交接已在 `593df7e4-4024-4c9b-b3d6-9db1867b6acf` 被实测否定，本轮没有复用该机制。

正式转换流程会话 `c79704c0-189b-4399-98ee-204378101b58`，operation `dep01-r3-final-flow-5dd744ed-02`：附件 SHA `c87864f0198c612b9d670c281a896cfed6d707ce136d140cbc1f20878d518395`；单次 exit 1 / 59.370 s / **152 PASS、3 项原子 FAIL**，另有总失败汇总。完整原生 guard、两个转换后的函数、真实 NSIS File/WriteUninstaller/写删和完整 PS1 控制流参与执行；仅应用载荷和外部注册/快捷方式/网络 provider 换为可丢弃 fixture。两次 15 秒慢保存自行完成；隐藏/草稿/保存失败均未被强杀、拒绝时字节不变。6 个真实 guarded 写删周期与合计 186 次重启竞争通过。三项 FAIL 均是正常 `Uninstall.exe /S` 启动进程 exit 0，期望拒绝码 2；应用仍活着、字节不变、无成功 marker，不能把这些 PASS 当作返回码通过。

根因来源是 NSIS 3.13 [Main.c](https://raw.githubusercontent.com/NSIS-Dev/nsis/v313/Source/exehead/Main.c)：自复制分支在 `un.onInit`/Section 前启动 TEMP 子进程，关闭其进程句柄并返回自身默认 0，根本不等待脚本结果。模板内的失败传播不能让已经退出的进程返回非零。仅给命令补 `_?=` 会让卸载器从被保护树原位运行，与全树可写占用/删除发生冲突，不能未经生命周期验证当作修复。R1-1 **仍 Required**；没有接受错误成功、降低验收或把有限 Windows 通过认作发布许可。

按协作规则，后续实质选项交主控：推荐保留已证的 writer 与 NSIS 核心，评估一个能同步回传结果且保持自删除/签名的窄原生卸载引导；它需要新的生命周期/签名设计与 Windows 证据。另一项是更换/重做分发引导，维护面更大；保留正常入口假成功不满足已批准验收，只有用户能决定改变该要求。本会话未擅自引入这些实现、修改应用启动/权限或开启第4轮，也不自行作 merge 许可。

本轮本地真实交叉打包通过；保留完整转换/模板漂移、PS1 控制流、缺文件/改摘要拒绝和编译器/签名退出传播的 4 个定向回归测试。相关测试/race/vet、双平台适配工具编译、真实 no-updates fixture 打包、sentinel red 及真实 makensis 本地 signer double 的结果以 `dep01-r3-fix-evidence/checks.json` 为准；signer double 只写本地日志，不接触真实签名/上传工具。首次手工 golden fixture 多插空行被完整模板校验拒绝，核对真实生成器后修正 golden；首次摘要路径错误因读取平台目录而失败（真实摘要在输出根目录），均保留失败日志。no-updates 的首份最小项目 fixture 漏写实际 `youkai` URL scheme，也被完整模板校验正确拒绝；只纠正 fixture 配置后打包通过，没有放宽模板绑定。新 C 回归 runner 的首次 `-Werror` 缩进告警已修正，未放宽编译告警。

证据和新临时材料仅在本 session 的 `dep01-r3-fix-evidence/`，包含首尾快照、源清单、相对 37 文件增量、真实制品清单、实现/阻塞交接、全部命令日志及两次附件。Windows 原始证据分别保留于两个新执行会话目录；包/文件前后 hash 不变。第二包 TSV 有 4 处换行丢失、5 条非 ASCII 路径日志截断，37 个已记录直接子 PID 及 runner 已退出；TEMP 自复制孙进程和 PS1 内部 setup 的独立 PID/退出未记录，不能宣称整棵进程树覆盖。fixture 附加的卸载初始化限制函数误名为 `un..onInit`、未运行（不在生产模板中）；driver 仍只运行自身准确目标/marker/hash 绑定的卸载器，但该附加限制不能记为通过，原包保留不修改。交互目录页、真实 HKCU/协议/快捷方式/安装 UI、原生 Windows MSVC 分支、真实签名更新整链未覆盖；本地交叉包仍缺 runner 四 CRT，应用 EXE 实际与 R1 的 `a43e75cf...` 同 SHA、仍未在 Windows 运行。旧 2487 PASS/9 现场 Skip、race/vet/module/Mac OCR 仅按未改区域复用；旧 collect 一次失败仍归因未知。所有新证据/制品保留，旧资料未清理。

## R3 核心范围收敛与最小验证

主控在同一在办第3轮转交用户新要求：

> 后续测试不用做得特别细，有一些边界情况可以等真的遇到了再处理。接下来做什么

主控据此把本轮硬门槛收敛为：完整两个 NSIS CloseApp 和 PS1 禁止强杀/自动提交草稿；普通实际最终安装目录仍有应用进程则停止后续目标文件变更并提示从应用菜单退出、处理未保存内容后重试；基本检测/工具错误、超时和未知码不能误放行；PS1错误须向调用者返回失败。完整版本/模板漂移封锁、统一生产入口及签名/更新/最终摘要/manifest-last顺序仍必须保留。

用户本轮同意延期/未覆盖：安装时重新启动的竞争、完整OS排斥、特殊路径别名/reparse、罕见权限组合、完整交互/IME/托盘/通知/W3矩阵。它们不再是本轮硬门槛，不宣称已通过，不为其引入新启动协议/迁移/ACL或锁体系。标准NSIS卸载直接bootstrap返回0的已知限制仍单列；当前以实际worker是否拒绝并保护文件、以及PS1外层是否失败验收，没有声称bootstrap已修复或用其0推断卸载成功。

当前源码已移除本轮先前新增的native writer插件、暂存写删及额外32位编译依赖；先前47文件已逐项核验并复制到本会话 `dep01-r3-fix-evidence/simplified/prior-source/`，旧冻结和所有实验原件保留。新 `tools/mygobuild/gate.ps1` 只读查询最终目录：仍运行返回2、未知/查询异常返回3，两个完整NSIS函数仅接受明确0，其余情况停止；不发送关窗/强杀，不等一个超时后升级终止方式。正常文件提取/卸载/注册/协议仍由原NSIS完成。PS1保持同一最终 `/D` 交接、严格registry读取、任意非零及外层throw传播，未改公钥/源/渠道、模型/字典/阈值或用户数据行为。

最小 Windows 新执行会话 `269be79b-d390-4880-85b2-04866f742c85`，operation `dep01-r3-core-exit-5dd744ed-04`，绑定包 SHA `192d072fe45451c063c9c4481e27b57274b05ad06632e960460241a754b6c66c`，清单自身 SHA `f0ce2094cee339700ec6fe838af5cf83cfbac1deec4229209f1503b94d441bd6`：Win11 22631 x64 / NTFS / 未提权 / PowerShell5.1 10.0.22621.4745，单次 **exit0 / 19.589s / 27 PASS、0 FAIL**。真实完整转换的install/uninstall/PS1各一组“仍运行拒绝→fixture专用消息退出→继续成功”，只替换真实app payload和外部registry/网络/协议/快捷方式副作用；实际NSIS File/WriteUninstaller/RMDir及完整两个检查函数和PS1控制流参与执行。

| 场景 | direct exit | 实际 worker exit | 后续动作 |
| --- | --- | --- | --- |
| install拒绝 / 成功 | 2 / 0 | 2 / 0 | 拒绝无marker，成功实际提取payload |
| uninstall准备安装 | 0 | 0 | 正常产生真实NSIS卸载器 |
| uninstall拒绝 / 成功 | 0 / 0（原bootstrap） | 2 / 0 | 拒绝时fixture存活/字节不变；退出后实际RMDir删除自有目录 |
| PS1拒绝 / 成功 | **1 / 0** | 2 / 0 | 外层真实非零/零；实际目标动作符合预期 |

7组control全部拿到真实数值退出码（无null），summary.json完整；18个精确记录的driver/fixture/direct/NSIS worker/helper PID均已退出，helper只用PID+路径+marker/hash/class验证后的专用消息退出。ZIP和18个成员前后hash一致。原始报告在该Windows会话 `dep01-r3-core-evidence/20261114-152810-1bb47a6bcdd14e68b64f544c1de7dbb1/`，原生summary在 `C:\Users\70993\AppData\Local\Temp\dep01-r3-core-e12bd006f0954696a8504f617c8f5510\summary.json`；标准NSIS TEMP卸载副本保留，旧TEMP未动。

前一核心执行 `1850e36b-5f12-498a-a116-5d48952114c3` / op03仍保留 **FAIL**：真实worker/文件保护可见，但driver的Start-Process对象directExit为null，非零断言发生空真，GenericList汇总也抛异常；不能算PS1外层传播通过。后续只修一次性driver为原生Process.Start保有handle、显式ToArray汇总，并加强null拒绝，生产适配与核心断言没有放宽；用新包、新会话完成上述27/27。无独立review介入，不是第4修正轮。新执行会话的独立校验脚本末尾打印格式错误不影响已写证据；其后只读JSON复核，未重跑driver。早期stdout大小快照4096B在流关闭前取得，最终5462B，以最终核对为准。

本轮必要工具/模板4个定向测试、vet、Windows工具编译与真实应用crosspackage通过；最终摘要重新核对。旧完整套件2487PASS/9现场Skip、race/OCR等只按不变区域复用，未为了文档或延期边界重跑。真实Windows应用EXE仍与R1同SHA且未运行，交叉包仍缺runner四CRT，没有真实签名/联网更新整链、目录页/HKCU/协议/快捷方式或用户数据安装实测。此处完成的是用户收窄后的核心修正，建议独立复审据新标准关闭R1-1；merge、发布与后续轮次仍由主控/用户决定，本会话不自行开启第4轮。
