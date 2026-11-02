# 2026-11-02 Windows 本地客户端 1920×1080 全量真机扫描

> 本报告是这次 Windows 本地客户端扫描数据的唯一权威来源；[架构 §6](../specs/scanner-architecture.md#windows-本地客户端遍历状态1920x1080-首次全量真机扫描完成时序未标定)、T2/T5、决策与索引只引用结论。验收对象是 Windows 本地完整客户端在 1920×1080 窗口模式下的首次全量真机扫描，不是 [T5 §6](../specs/t5-zzz-inventory-traversal.md#6-保存什么证据怎样验收)的三客户端三次人工基准验收，也不等于 Windows 已通过验收。

## 结论

- 在 dev 构建 `890e573` 上，Windows 本地完整客户端完成一次 1920×1080 全量扫描：1,017 件，六个位置全部 `complete`，`coverage=complete`，`sort=verified`、`sortLabel=稀有度`，`capturedCount=1017`，0 件未处理。
- 59 次滚动全部 `displaced`、`attempts=1`；0 次 `undecided`、0 次 `lost`、0 次滚动重试；1,017 件物品的 `retries` 全部为 0。没有 stop trigger、fault、位置级降级或不完整位置。
- 1,016 件通过字段校验，1 件进入校对（`needsReview`）：seq 273 的名称行被识别为 `山大王[3]1`，报 `nameLineUnparsed`，导致 `setId`/`slot` 为 null；它的等级、主属性和全部副词条与 mac 基线 seq 273 一致。
- 与 [2026-11-01 macOS 云客户端全量基线](2026-11-01-macos-stage2-field-acceptance.md)按多重集合逐件比较：1,016/1,017 相同，唯一差异就是 seq 273 的 `setId`/`slot`；按序号直接比较为 1,014/1,017 相同，因为 seq 390/391（同为 32900、4 号位、暴击率主属性）在两次扫描中顺序相反。管理状态逐件相同；装备状态净增 7 件未判定，但逐件有 21 处状态变化，按内容重新配对后仍是这 21 处，不能概括为“只差 7 件”。
- 108 件 `equipped` 的 `agentId` 全部为 null；run3 共 108 次头像匹配，分数 0.220–0.857（中位 0.663），全部低于 0.90 阈值。#111 当时记录的是 run2 的 4 件（0.484–0.702），那时的记录是对的；run3 与 run2 不是同一批数据。
- 时序仍是 macOS 云客户端的借用值（`timing=macOS cloud profile (uncalibrated)`），没有因时序触发停止；`drag_ms` 为 1,326–1,437 ms，均在 #111 放宽后的 1,500 ms 上限内。时序标定未完成，不能据此判断性能或其他硬件上的稳定性。
- 这仍只是单次完整扫描。T5 §6 的三次人工基准库存验收、其他 16:9 档位、导出与截图、位置级降级、Windows 云客户端均未覆盖；不能据此宣称 Windows 已通过验收或开放其他档位。

## 环境与构建

| 项目 | 值 |
| --- | --- |
| 机器 | `DESKTOP-6B7KG8L`（操作者记录；证据目录未保存主机名） |
| 系统 | Windows（操作者记录与 2026-11-01/02 探针同一台机器；本次证据未保存 OS 版本读数） |
| 显示 | 单块 3840×2160，125% 缩放（操作者记录；日志只记录游戏客户区 1920×1080） |
| 游戏 | 《绝区零》本地完整客户端，`C:\lo\mihoyo\miHoYo Launcher\games\ZenlessZoneZero Game\ZenlessZoneZero.exe`，窗口模式，客户区 1920×1080；游戏版本 3.3.0 来自 [W3a 同机记录](2026-11-02-windows-local-sample-round.md)，本次证据未保存版本读数 |
| 应用 | Youkai Dev 0.0.1，commit `890e5736c6bb`，windows/amd64，dev 构建，数据根 `C:\Users\70993\AppData\Local\Youkai Dev` |
| 权限 | `platform windows: integrity: target=high self=high`；按 D-1 方式在提权 PowerShell 中设置环境变量后启动 |
| 启动 | `YOUKAI_GAME_SOURCE=traverse`、`YOUKAI_GAME_CLIENT=local`、`YOUKAI_GAME_EXE=<游戏 exe>`、`YOUKAI_DEV_AUTOSTART=1` |
| 布局与排序 | `profile=windowsLocal-1920x1080 tier=1920x1080 client=1920x1080 scale=1.0000 layout=winlocal-1920x1080 sort=template`；排序模板来自 #109 的 Windows 原生样本 |
| 时序 | `timing=macOS cloud profile (uncalibrated; W3 finalizes)`，仍是 mac 借用值 |
| 会话 | `ece1b49f-7a78-4b1c-bc0b-9ad414b09958` |
| 时间 | 进程 23:10:30.785 启动；23:10:31.277 接受；23:24:06.167 结束；接受至结束 814.9 s（13 分 34.9 秒）；mac 基线为 15 分 08 秒，仅作对照 |

扫描开始时 `observedScope.startPosition=3`、`filterAtStart=active`；这两个字段只是准备前的观测元数据，遍历仍按位置 1→6 执行。游戏窗口在扫描期间保持前台；本次没有移动鼠标或切换应用。

## 三次运行与修复

| 运行 | 构建 | 开始方式 | 接受时间 | 结束 | 结果 | captured / produced / unprocessed |
| --- | --- | --- | --- | --- | --- | --- |
| run1 | `64bef82` | 用户点开始（`request "ui-1"`） | 20:37:27.291 | 20:37:57.497 | `failed / prepareTimeout / preparing`，原因 `focusLost` | 0 / 0 / 0 |
| run2 | `3d7e08a` | `YOUKAI_DEV_AUTOSTART=1` | 21:26:10.919 | 21:26:31.385 | `failed / invalidOperation / capturing` | 20 / 19 / 1 |
| run3 | `890e573` | `YOUKAI_DEV_AUTOSTART=1` | 23:10:31.277 | 23:24:06.167 | `completed / verifiedEnd / capturing` | 1,017 / 1,017 / 0 |

- **run1（20:37）：** 旧构建没有实现 Windows `Activator`，用户点开始后准备阶段等满 30 秒后以 `prepareTimeout`（`focusLost`）结束。日志只记录 `another window is the actual foreground window`，没有保存当时前台窗口的身份；操作者记录当时前台是 Youkai。日志切片没有 `activate:` 行，说明当时没有发出 `SetForegroundWindow`；checkpoint 1 为 0 件。run1 的日志切片从接受请求开始，未包含版本行，`64bef82` 来自本轮运行序列记录；#110 随后实现 `platform.Activator`，代码差异见 `3d7e08a`。
- **run2（21:26）：** 自动启动；激活后立即复查未见游戏在前台（拒绝与异步切换无法区分），约 0.24 s 后游戏已在前台并通过就绪检查，准备成功。位置 1 采集 20 件，其中 19 件产出且校验通过；第 20 件因扫描中断为 `unprocessed / interrupted`，没有识别行。第一次滚动在发送前被输入层拒绝：`invalidOperation: drag duration 1.2s outside 0..1s`；滚动汇总为 `outcome=error attempts=1 drag_ms=0 rows=0`，扫描以 `invalidOperation` 停止。#111 把 Windows `MaxDragDuration` 从 1 s 放宽到 1,500 ms，并保持与 mac 的 1,200 ms 滚动拖拽一致。
- **run3（23:10）：** 自动启动；激活后立即复查仍未见游戏在前台（拒绝与异步切换无法区分），日志记录 `waiting for the user to focus the game`；约 0.24 s 后游戏已在前台并通过就绪检查。完整扫描在 23:24:06 结束，checkpoint 130 为 1,017 件；checkpoint 129 也是 1,017 件，目录保留 129/130。

> 结论中的“三次运行”不表示三次人工基准验收。run1 是手动启动但构建缺少 Activator；run2 是中断的短运行；只有 run3 是完整扫描。用户点开始且 Youkai 为前台进程时的激活成功路径，在 #110 之后仍未实测。

## run3 结果

### 1. 全量扫描

| 指标 | 结果 |
| --- | --- |
| 采集 / 产出 / 未处理 | 1,017 / 1,017 / 0 |
| 终止 | `completed / verifiedEnd / capturing` |
| 覆盖 | `complete`（`session.coverage.status=complete`、`observedScope.coverage=complete`） |
| 排序 | `verified`，`sortLabel=稀有度`；方向未读取 |
| 位置 | 6/6 `complete`，`topVerified`/`bottomVerified` 均 true |
| 检查点 | generation 130（1,017 件）；129 也是 1,017 件 |
| 会话文件清单 | 10,939 个文件（含 2 个检查点），合计 56,771,653 字节（约 54.1 MiB） |
| 物品状态 | 1,017 `produced`；1,016 `autoAccepted`、1 `needsReview` |
| 详情归属 | 1,002 `transitionVerified`、9 `referenceVerified`、6 `initialVerified`；0 `detailUnattributed` / `detailUnresolved` |
| 位置级降级 | 0 个不完整位置；无 `positionFailureLimit` |
| 后续故障 | 0 |
| 平台收尾 | `captureStopped=true drained=true released=true hotkeyErr=<nil>` |

### 2. 位置账目

| 位置 | 视口 | 访问 = 接纳 | 滚动 | 末次滚动 | 顶部 | 底部 | 完整 |
| --- | ---: | ---: | ---: | ---: | --- | --- | --- |
| 1 | 6 | 99 | 5 | 4 行 | 是 | 是 | 是 |
| 2 | 7 | 112 | 6 | 3 行 | 是 | 是 | 是 |
| 3 | 7 | 106 | 6 | 2 行 | 是 | 是 | 是 |
| 4 | 15 | 230 | 14 | 1 行 | 是 | 是 | 是 |
| 5 | 16 | 249 | 15 | 2 行 | 是 | 是 | 是 |
| 6 | 14 | 221 | 13 | 3 行 | 是 | 是 | 是 |
| 合计 | 65 | 1,017 | 59 | — | 6/6 | 6/6 | 6/6 |

六个位置都记录 `S filter active, top confirmed`。顶部确认每个位置首次尝试即接受（`the list stayed still`，`top attempts=1`），`top_ms` 为 5,218 / 5,207 / 5,221 / 5,230 / 5,222 / 5,221 ms。位置 1 的末次滚动仍为 4 行，另行做了 1 次底部确认（`bottom_ms=6337`）；其余位置末次滚动为 1–3 行短步，没有另做底部确认（`bottom_ms=0`）；六位置 `bottomVerified` 均为 true。

本次总数 1,017 件；与 2026-11-01 的 1,017 件在多重集合层面几乎一致，但不是同一时点库存，不能据此推断库存从未变化。

### 3. 滚动判定

59 次成功滚动在 run3 日志中产生 118 行 `collect: scroll position=`（每次尝试一行、汇总一行）；按汇总行统计：

- 59 次全部 `displaced`，0 `undecided`，0 `lost`。
- `attempts=1` 共 59 次，滚动重试 0；物品 `retries=0` 共 1,017 次。
- basis：`selection+feature` 57 次，`selection+feature+scrollbar` 2 次。
- 每次前进的行数：54 次 4 行、2 次 3 行、2 次 2 行、1 次 1 行；与 mac 基线的行数分布相同。
- 日志没有 `outcome=error`、`stop trigger`、`incomplete` 或位置级降级行。

| 位置 | 滚动 | `selection+feature` | `selection+feature+scrollbar` |
| --- | ---: | ---: | ---: |
| 1 | 5 | 5 | 0 |
| 2 | 6 | 6 | 0 |
| 3 | 6 | 6 | 0 |
| 4 | 14 | 14 | 0 |
| 5 | 15 | 14 | 1 |
| 6 | 13 | 12 | 1 |

证据包只保存了诊断索引和 3 张抽样 PNG，没有保存 59 份滚动 manifest 本体，因此本报告不能像 macOS 阶段 2 报告那样核对每份 manifest 的 `settle_observations`、前后 frame id 或停稳帧。滚动结论以 run3 日志的汇总行为准。

### 4. 诊断

- `diagnostics-index.tsv` 共 59 个滚动诊断目录，全部为 `scroll-ok`，没有失败目录；索引合计 91 个文件、10,781,167 字节。
- 其中 8 个目录各 5 个文件（4 张关键帧裁剪 + 1 份 manifest），51 个目录各 1 个文件（仅 manifest）。
- 证据包另保存 3 张抽样 PNG：位置 1 vp1 的 2 张（`0002-f623`、`0004-f625`）和位置 1 vp2 的 1 张（`0000-f935`）；它们用于证明诊断输出存在，不代表完整关键帧集。
- 完整诊断目录位于 Windows 数据根的 `logs/diagnostics`，未随本证据包保存。

### 5. 时序

| 指标 | 最小 | 中位数 | p90 | 最大 |
| --- | ---: | ---: | ---: | ---: |
| `motion_ms` | 29 | 77 | 109 | 199 |
| `settle_ms` | 254 | 300 | 349 | 438 |
| `drag_ms` | 1,326 | 1,330 | 1,332 | 1,437 |
| `total_ms` | 1,587 | 1,635 | 1,685 | 1,772 |

> p90 沿用证据分析脚本口径：59 个样本按升序取第 ⌊0.90 × 59⌋ = 53 个值；按 nearest-rank 会取第 54 个，`motion` 为 125、`settle` 为 354、`drag` 为 1,333、`total` 为 1,709。正文采用前者。

`drag_ms` 最大 1,437 ms，低于 #111 放宽后的 1,500 ms 输入上限；没有因拖拽时长触发 `invalidOperation`。但时序仍是 mac 的未标定值，本次数值与 mac 的差异不能解释为 Windows 更快或更慢，也不能据此定稿时序 profile。

### 6. 识别与校验

- 1,017 件全部有识别行；1,016 件 `validation.status=passed`，1 件 `failed`（`quality=needsReview`）。
- 失败件为 seq 273：名称行识别为 `山大王[3]1`（分数 0.902），报 `nameLineUnparsed`；`setId`/`slot` 为 null；等级 15、主属性防御力 184 和四条副词条与 mac 基线 seq 273 一致；装备状态不同（mac equipped、Windows null）。
- 管理状态：1,005 `locked`、12 `discarded`；与 mac 基线逐件相同。
- 装备状态：887 件 `unequipped`、108 件 `equipped`、22 件 `null`；mac 基线为 887 / 115 / 15。净增 7 件未判定，但逐件有 21 处变化：101 件 equipped→equipped、12 件 equipped→null、5 件 null→equipped、2 件 unequipped→equipped、2 件 equipped→unequipped、10 件 null→null、885 件 unequipped→unequipped。按内容重新配对（含 seq 273 的 equipped→null）后仍是这 21 处。
- 装备者身份：108 件 `equipped` 的 `agentId` 全部为 null；108 次头像匹配的 best score 为 0.220–0.857，中位 0.663，全部低于 0.90 阈值。另有 21 件 `null` 状态也带 `equippedStateUnavailable` 警告，但没有头像匹配分数（`agent avatar match not evaluated`）。装备警告不阻止自动接受；本轮只有 seq 273 因名称行错误进入校对。

## 与 mac 基线的逐件对比

### 方法

以 2026-11-01 macOS 云客户端 `attempt2/checkpoint-final.json` 为基线。每件物品取以下键：

```text
setId | slot | level | mainStat.attribute | mainStat.value |
substats（attribute=value+unit@enhancementRolls，按集合比较，顺序不计）
```

对两次扫描各 1,017 件做多重集合比较（同一键可重复计数），另按序号直接比较。管理状态、装备状态与详情归属另行逐序号比较；装备状态再按内容重新配对核对。

### 结果

- **主键：** 1,016/1,017 相同。Windows 侧多出的唯一键是 seq 273 的 `null | null | 15 | defense | 184 | ...`，mac 侧对应键是 `33200 | 3 | 15 | defense | 184 | ...`；其余字段完全相同。也就是说，唯一差异就是 seq 273 的套装/槽位未解析。
- **按序号：** 1,014/1,017 相同；除 seq 273 外，seq 390/391（同为 32900、4 号位、暴击率主属性）在两次扫描中顺序相反。
- **管理状态：** 1,005 locked→locked、12 discarded→discarded，逐件相同。
- **装备状态：** 净计数为 equipped 115→108、null 15→22；逐件变化见上节 21 处，按内容重新配对后仍是这 21 处，不能只按净差 7 件理解。
- **详情归属：** 两次扫描都是 1,002 transition / 9 reference / 6 initial；9 次参照往返均成功。
- **位置账目：** 视口、访问数、滚动数逐项相同；末次滚动行数与顶底证据也相同。
- **滚动：** 59 次均为 `displaced`、`attempts=1`；basis 分布为 57/2，mac 为 56/3；行数分布相同。

两次扫描相隔一天，且不是同一时点库存。上述结果只说明这次 Windows 观测与 mac 基线高度一致，不证明库存稳定，也不证明 OCR 在所有画面下都正确。

## 未解决问题

1. **用户点开始的激活路径未实测。** run2/run3 的日志只证明激活后立即复查未见游戏在前台（拒绝与异步切换无法区分），约 0.24 s 后就绪检查通过；run1 在 #110 之前根本没有发出激活请求。因此“用户点开始、Youkai 为前台进程”时的成功激活路径仍没有真机证据。**待跟进：** #110 在 `SetForegroundWindow` 后立即复查，可能把尚未完成的异步切换误报为失败；本 PR 不改代码。
2. **时序未标定。** 滚动停稳与拖拽时长仍来自 macOS 云客户端；本次没有因时序出错，`drag_ms` 也在 #111 的 1,500 ms 上限内，但时序 profile 仍需 Windows 原生标定。
3. **装备者身份未识别。** 108 件 equipped 的 `agentId` 全部为 null；头像匹配分数 0.220–0.857，全部低于 0.90。当前头像模板来自 mac，需要 Windows 侧模板或阈值标定；不能把 108/22 的装备状态计数当作可靠结论。
4. **托盘图标缺失，无法从托盘退出。** 操作者报告：关闭主窗口后进程仍在、托盘没有图标，日志没有 `tray` 行，用户无法退出；根因未查。证据目录没有保存托盘相关日志或截图。
5. **dev 链接警告未定位。** 操作者记录链接时出现 `.rsrc merge failure: multiple non-default manifests`；本轮自检与扫描均正常，但没有进一步确认该警告对资源或清单的影响。
6. **名称行误读。** seq 273 的名称行 `山大王[3]1` 无法按 `套装名[N]` 解析，导致 `nameLineUnparsed`、`setId`/`slot` 为 null；需要在校对中修正并补充该形态的识别样本。

## 其他观察

- **反作弊：** 在 High 完整性下持续自动输入约 13.5 分钟，没有观察到拦截或游戏报错。这只是一次观察，不是长期安全保证，也不是对反作弊策略的结论。
- **证据包边界：** 证据包没有保存完整诊断 manifest、游戏版本、OS 版本或主机名读数；相关值若来自操作者记录，已在环境表中标注。
- **构建警告：** `.rsrc merge failure: multiple non-default manifests` 是构建期观察，不在 run3 扫描日志中；本报告不推断其根因。

## 不能推出的结论

- **不是 Windows 验收通过。** 本次只有一次完整扫描，不能替代 T5 §6 的三客户端、每客户端至少三次人工基准库存验收；也不能替代 Windows 云客户端、其他档位或安装制品的验收。
- **其他 16:9 档位未开放。** 本次只验证 1920×1080；1280×720、1600×900、2276×1280、2560×1440、3840×2160 仍需各自的实机字段验收。
- **导出、截图、历史重开与界面复核未测。** 本次用 `YOUKAI_DEV_AUTOSTART=1` 自动开始并在扫描结束后停止；没有点击导出、截图或逐件校对。
- **位置级降级未触发。** 本次 0 个不完整位置、无 `positionFailureLimit`，因此位置准备/证明失败的降级路径没有 Windows 真机证据。
- **Windows 云客户端未测。** 本次只覆盖本地完整客户端；Windows 云客户端的窗口身份、视频矩形、断流恢复与输入仍需单独验证。
- **时序与性能未定稿。** 时序仍是 mac 未标定值；本次的 814.9 s、滚动时长与停稳时间只适用于这次运行，不能作为其他硬件、其他档位或长期性能结论。
- **装备状态与头像匹配不足以外推。** 108 件 equipped 的 `agentId` 全部为 null，头像分数低于阈值；不能把 108/22 当作可靠的装备者统计，也不能据此推断其他账号或库存的装备状态。
- **覆盖口径有限。** `coverage=complete` 是当前实现的六位置账目、顶部/底部证据和排序标签口径，不等同于阶段 3 的蓝色填充、面板底部控件范围证明，也不持续检查扫描中的排序/筛选变化。
- **排序方向与计数未核对。** `sort=verified` 只证明面板标签模板匹配，不读取升降序；界面不显示数量，本次未做计数对账。
- **单一显示配置。** 本次是 3840×2160 / 125% / 窗口模式 / 1920×1080 客户区；SDR/HDR、100% 缩放、无边框或独占全屏、其他显示器布局未覆盖。
- **OCR 不是普适结论。** 1,016/1,017 与 mac 基线一致，说明这次观测高度一致；不能证明所有套装名、词条行或异常画面都能正确解析。
- **反作弊安全未证明。** 一次 13.5 分钟无拦截的观察不能作为长期或大规模使用的安全保证。

## 证据

```text
验收根目录:
~/Library/Logs/Youkai Dev/probes/20261102-windows-field-runs/

SHA256SUMS
run1/
  session-85124b4b-checkpoint-1.json
  youkai-game-current.png
  youkai-log-since-952.txt
run2/
  checkpoint-4.json
  checkpoint-5.json
  session-files.tsv
  youkai-log-run2-since-2063.txt
run3/
  checkpoint-129.json
  checkpoint-130.json
  diagnostics-index.tsv
  session-files.tsv
  youkai-log-run3-since-18074.txt
  diag-20261102-231052722-pos1-vp1-scroll-ok-attempt1-0002-f623.png
  diag-20261102-231052722-pos1-vp1-scroll-ok-attempt1-0004-f625.png
  diag-20261102-231104379-pos1-vp2-scroll-ok-attempt1-0000-f935.png

对比基线:
~/Library/Logs/Youkai Dev/probes/20261101-full-acceptance/attempt2/checkpoint-final.json

Windows 会话数据根（日志中的原始路径）:
C:\Users\70993\AppData\Local\Youkai Dev\sessions\ece1b49f-7a78-4b1c-bc0b-9ad414b09958\
  checkpoints/129.json
  checkpoints/130.json
  evidence/（会话文件清单 10,939 个文件，56,771,653 字节）
```

`SHA256SUMS` 共 15 条，全部校验通过：

```text
985c87ad63ff3c7ca228b5c99afbf08e012d7f4f9e2636ba73d7283f1f784067  run1/session-85124b4b-checkpoint-1.json
14947912965c115120bb58dfb67832a4c9301dfb599f769b758d263216585d4b  run1/youkai-game-current.png
ccd31e94efb7cb072aeeeeb4ffcacb6fff3468bf424ab1d6b84cbde978e87ffb  run1/youkai-log-since-952.txt
12556fa9bb7094f3e0029b9629209ad2c426c1fa11c723a36b5812299e8ce2e0  run2/checkpoint-4.json
1bcff0d3dcd4dabf04a27f95058d847b79af08227cf4d6324717a6a43f8650ef  run2/checkpoint-5.json
fc1578e8a52874e8ec239cf242ad17b61c43bff096c671e59a7ab60a52bfc6c0  run2/session-files.tsv
54fdd2b714db23e55aa9676a41531a366dec955dc4a3caa9618fec1aa80e493b  run2/youkai-log-run2-since-2063.txt
59808d2ac48f1e08e6f6b11706eddc49c8edde0cdc0e140fc3680ce6d2dfd33e  run3/checkpoint-129.json
be9502a1291b826f20978797173964fa7afdf4e1c086339c211ba789042dca80  run3/checkpoint-130.json
a4362716a9143e1036bcffb19f3552e35c435674966398ae7c405583fdd65bac  run3/diag-20261102-231052722-pos1-vp1-scroll-ok-attempt1-0002-f623.png
7e301714b212b514f79d7ec1fc6387be1ed8c3f3bc1456072204b7f4536a9efd  run3/diag-20261102-231052722-pos1-vp1-scroll-ok-attempt1-0004-f625.png
9cda755725ece6420afdc427c84638f0f999332d382736df5adf8bafa36e9889  run3/diag-20261102-231104379-pos1-vp2-scroll-ok-attempt1-0000-f935.png
91a96c3ab482f1d15ae7903162f13a81a7fdf50c7a5d2494185583211ca31061  run3/diagnostics-index.tsv
c9e5eeeb97a5ea8512754fbd63f8cf4d833277d5f481713de353223483e7f807  run3/session-files.tsv
089226192c819b230a767e17effb7ac422cda005fe52d7f3f7985be642c41fd0  run3/youkai-log-run3-since-18074.txt
```

## 复算方式

以下命令只读证据，输出文件写在 `/tmp`。`R` 指向本次验收目录，`L` 指向 run3 日志，`M` 指向 mac 基线。

```text
R="$HOME/Library/Logs/Youkai Dev/probes/20261102-windows-field-runs"
L="$R/run3/youkai-log-run3-since-18074.txt"
M="$HOME/Library/Logs/Youkai Dev/probes/20261101-full-acceptance/attempt2/checkpoint-final.json"
```

**校验和与证据大小**

```text
(cd "$R" && shasum -a 256 -c SHA256SUMS | grep -c ': OK$')

awk -F'\t' 'NR>1 {n++; s+=$2} END {printf "files=%d bytes=%d\n", n, s}' "$R/run3/session-files.tsv"
awk -F'\t' 'NR>1 {n++; f+=$2; s+=$3} END {printf "dirs=%d files=%d bytes=%d\n", n, f, s}' "$R/run3/diagnostics-index.tsv"
```

**run1 / run2 / run3 结论**

```text
jq -r '[.session.sessionId, .session.termination.kind, .session.termination.code, .session.termination.phase, .session.capturedCount, (.items|length)] | @tsv' \
  "$R/run1/session-85124b4b-checkpoint-1.json" \
  "$R/run2/checkpoint-5.json" \
  "$R/run3/checkpoint-130.json"

jq -r '.items[] | [.sequence,.quality,(.data.structured.validation.status // "")] | @tsv' "$R/run2/checkpoint-5.json"
grep -c 'collect: item ' "$R/run2/youkai-log-run2-since-2063.txt"
grep -c ' recognized:' "$R/run2/youkai-log-run2-since-2063.txt"
grep 'stop trigger' "$R/run2/youkai-log-run2-since-2063.txt"
```

**run3 汇总与时间**

```text
jq -r '[.session.termination.kind, .session.termination.code, .session.termination.phase,
        .session.coverage.status, .session.capturedCount, (.items|length),
        .session.observedScope.sort, .session.observedScope.sortLabel,
        .session.observedScope.coverage, .session.observedScope.startPosition,
        .session.observedScope.filterAtStart] | @tsv' "$R/run3/checkpoint-130.json"

jq -r '[.session.capturedCount,(.items|length)] | @tsv' "$R/run3/checkpoint-129.json" "$R/run3/checkpoint-130.json"
grep -c '^checkpoints/' "$R/run3/session-files.tsv"

python3 -c 'from datetime import datetime; a=datetime.fromisoformat("2026-11-02T23:10:31.277117"); b=datetime.fromisoformat("2026-11-02T23:24:06.167105"); print(round((b-a).total_seconds(),1))'
```

**位置账目与滚动**

```text
jq -r '.session.observedScope.positions[] | [.position,.viewports,.visits,.complete,.topVerified,.bottomVerified] | @tsv' "$R/run3/checkpoint-130.json"

grep -c 'collect: scroll position=' "$L"
grep 'collect: scroll position=.*attempts=' "$L" | wc -l
grep 'collect: scroll position=.*attempts=' "$L" | sed -E 's/.*outcome=([a-z]+).*/\1/' | sort | uniq -c
grep 'collect: scroll position=.*attempts=' "$L" | grep -o 'attempts=[0-9]*' | sort | uniq -c
grep 'collect: scroll position=.*attempts=' "$L" | sed -E 's/.*basis=([a-z+]+).*/\1/' | sort | uniq -c
grep 'collect: scroll position=.*attempts=' "$L" | sed -E 's/.*position=([0-9]+) viewport_from=.*/\1/' | sort -n | uniq -c
grep 'collect: scroll position=.*attempts=' "$L" | grep -o 'rows=[0-9]*' | cut -d= -f2 | sort -n | uniq -c
grep 'collect: item ' "$L" | grep -o 'retries=[0-9]*' | sort | uniq -c

grep 'collect: position=.*summary' "$L" | sed -E 's/.*position=([0-9]+) summary.*top_ms=([0-9]+).*bottom_ms=([0-9]+).*/pos=\1 top_ms=\2 bottom_ms=\3/'
for p in 1 2 3 4 5 6; do
  grep "collect: scroll position=$p viewport_from=.*attempts=" "$L" | tail -1 | grep -o 'rows=[0-9]*'
done

grep -c 'stop trigger' "$L" || true
grep -c 'incomplete' "$L" || true
grep -c -E 'detailUnattributed|detailUnresolved' "$L" || true
```

**校验与逐件对比**

```text
jq -r '.items[].data.structured.validation.status' "$R/run3/checkpoint-130.json" | sort | uniq -c
jq -r '.items[] | select(.data.structured.validation.status=="failed") | [.sequence,.data.lines[0].text,.data.structured.issues[0].code] | @tsv' "$R/run3/checkpoint-130.json"
jq -r '.items[].observed.capture.association' "$R/run3/checkpoint-130.json" | sort | uniq -c

jq -r '.items[] | [.data.structured.data.setId, .data.structured.data.slot, .data.structured.data.level,
  .data.structured.data.mainStat.attribute, .data.structured.data.mainStat.value,
  ([.data.structured.data.substats[] | (.attribute+"="+.value+.unit+"@"+(.enhancementRolls|tostring))] | sort | join(","))] | @tsv' "$M" | sort > /tmp/mac-keys.tsv
jq -r '.items[] | [.data.structured.data.setId, .data.structured.data.slot, .data.structured.data.level,
  .data.structured.data.mainStat.attribute, .data.structured.data.mainStat.value,
  ([.data.structured.data.substats[] | (.attribute+"="+.value+.unit+"@"+(.enhancementRolls|tostring))] | sort | join(","))] | @tsv' "$R/run3/checkpoint-130.json" | sort > /tmp/win-keys.tsv

comm -12 /tmp/mac-keys.tsv /tmp/win-keys.tsv | wc -l
comm -23 /tmp/mac-keys.tsv /tmp/win-keys.tsv
comm -13 /tmp/mac-keys.tsv /tmp/win-keys.tsv

jq -r '.items[].data.structured.data.managementState' "$M" | sort | uniq -c
jq -r '.items[].data.structured.data.managementState' "$R/run3/checkpoint-130.json" | sort | uniq -c

jq -r '.items[] | [.sequence, (.data.structured.data.equipped.state // "null")] | @tsv' "$M" | sort -n > /tmp/mac-equipped.tsv
jq -r '.items[] | [.sequence, (.data.structured.data.equipped.state // "null")] | @tsv' "$R/run3/checkpoint-130.json" | sort -n > /tmp/win-equipped.tsv
join -t $'\t' -j 1 /tmp/mac-equipped.tsv /tmp/win-equipped.tsv | awk -F'\t' '{c[$2" -> "$3]++} END {for (k in c) print c[k], k}' | sort -nr
```

**头像匹配分数**

```text
jq -r '.items[].observed.equippedAgentEvidence' "$R/run3/checkpoint-130.json" | grep 'scores ' > /tmp/win3-agent-evidence.txt
wc -l < /tmp/win3-agent-evidence.txt
sed -E 's/.*scores ([0-9.]+) <.*/\1/' /tmp/win3-agent-evidence.txt | sort -n | sed -n '1p;54p;108p'
jq -r '.items[] | select(.data.structured.data.equipped.state=="equipped") | .data.structured.data.equipped.agentId' "$R/run3/checkpoint-130.json" | sort | uniq -c
```

**run3 时序**

```text
for k in motion_ms settle_ms drag_ms total_ms; do
  printf '%s ' "$k"
  grep 'collect: scroll position=.*attempts=' "$L" | grep -o "$k=[0-9]*" | cut -d= -f2 | sort -n | sed -n '1p;30p;53p;59p' | tr '\n' ' '
  echo
done
```

**run3 诊断索引**

```text
awk -F'\t' 'NR>1 {c[$2]++} END {for (k in c) print k, c[k]}' "$R/run3/diagnostics-index.tsv" | sort -n
```
