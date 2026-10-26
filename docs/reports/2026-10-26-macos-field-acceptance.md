# 2026-10-26 macOS 真机验收：全量扫描、导出、历史、中途停止与退出

## 结论

本轮 macOS 云客户端验收的主流程通过：

- 全量扫描完成 1,007 件，六个位置全部 `complete`，覆盖 `complete`，排序验证为“稀有度”。
- JSON 与 CSV 导出均为 1,007 件，物品级失败/未处理为 0。
- 同进程历史重开与退出后冷启动历史重开均恢复到 1,007 件、覆盖完整。
- 中途停止热键生效，保留 34 件，终止为 `cancelled/userCancelled`，覆盖为 `partial`。
- 正常退出完成 `exit wrap-up complete`。

本轮仍有两个未完全收口的问题，不能据此宣称“启动稳定性完全通过”：

1. 云游戏在闲置后断开 `-1022`，一次扫描以 0 件失败；属于外部环境。验收时结果页没有把会话失败原因展示出来，验收后已修复（见下文）。
2. 一次扫描启动后 Youkai Dev 进程直接消失，无检查点、无退出收尾、无 crash report；统一日志显示同一时刻测试自动化触发了 TCC 辅助功能授权请求，受控启动复跑未复现。该次更可能是测试环境/外部终止，待排除。

另有一次受控启动的扫描在 6 秒内以 `navigationMismatch` 失败，立即重试即成功；现已为 `establishList` 增加一次有界重试并补充单元测试，但尚未在真实客户端再次触发验证。

## 环境与构建

| 项目 | 值 |
| --- | --- |
| 系统 | macOS 27.0.1（26A434），Apple M1 Pro |
| 显示 | 3024 × 1964 Retina，主显示器 |
| 游戏窗口 | 《云·绝区零》，1164 × 762，位置 (345, 59) |
| 分支 | `fix/collect-field-acceptance` |
| 提交 | `6233725d6382`（`fix(collect): prepare field acceptance build`） |
| 应用 | Youkai Dev 0.0.1（MyGo dev 构建） |
| 启动命令 | `CGO_ENABLED=1 YOUKAI_GAME_SOURCE=traverse go tool mygo dev` |
| 数据目录 | `~/Library/Application Support/Youkai Dev` |
| 正式数据 | `~/Library/Application Support/Youkai` 未改动 |

扫描期间保持游戏窗口在前台，未移动鼠标或切换应用。中途停止使用全局热键 `⌃⌥⌘S`。

## 结果

### 1. 全量扫描：通过

会话 `2bbfd723-881d-492e-a28d-27058063014b`，本地时间 02:30:56–02:48:02，约 17 分 06 秒。

| 指标 | 结果 |
| --- | --- |
| 采集/产出 | 1,007 / 1,007 |
| 未处理 | 0 |
| 终止 | `completed / verifiedEnd / capturing` |
| 覆盖 | `complete` |
| 排序 | `verified`，标签“稀有度” |
| 检查点 | 128 代，generation 128 含 1,007 件 |
| `positionLost` | 0 |
| 详情归属失败 | 0 |

六位置账目：

| 位置 | 视口 | 访问 | 顶部确认 | 底部确认 | 完整 |
| --- | ---: | ---: | --- | --- | --- |
| 1 | 6 | 96 | 是 | 是 | 是 |
| 2 | 7 | 114 | 是 | 是 | 是 |
| 3 | 7 | 103 | 是 | 是 | 是 |
| 4 | 15 | 229 | 是 | 是 | 是 |
| 5 | 16 | 246 | 是 | 是 | 是 |
| 6 | 14 | 219 | 是 | 是 | 是 |

本轮实际总数为 1,007；此前 2026-10-20 的 1,005 件是旧快照，不作为本轮差异结论。位置 4 的末尾出现 1 行短步（16 个对应关系），位置 6 的末尾出现 2 行短步（12 个对应关系），均符合短步结束证据。

### 2. JSON/CSV 导出：通过

导出文件：

```text
~/Downloads/youkai-2026-10-26-2bbfd723.youkai.json
~/Downloads/youkai-2026-10-26-2bbfd723.youkai-1.json
~/Downloads/youkai-2026-10-26-2bbfd723.youkai.csv
```

- JSON `format: youkai.scan`、`formatVersion: 2`、`items: 1007`、`excludedCount: 0`。
- 1,007 件全部 `quality: autoAccepted`、`validation.status: passed`。
- 会话终止与覆盖字段分别与检查点一致：`completed`、`complete`。
- CSV 1,008 行（1 行表头 + 1,007 件），字段包含序列、会话、质量、套装、稀有度、槽位、等级、主属性、四个副词条、管理与装备状态。
- 两次 JSON 导出均成功；第二个文件是重复点击产生的 `-1` 后缀文件，内容时间戳不同，未覆盖既有文件。

### 3. 历史重开：通过

- 同进程重开 `2bbfd723`：1,007 条、覆盖完整。
- 退出后冷启动重开：历史列表 6 条记录，打开同一条记录后先短暂显示 0 条，随异步加载恢复为 1,007 条、覆盖完整。
- 0 件失败记录在验收时只显示“无法确认”，与“失败 0 件”的组合容易误导；验收后历史与结果页已改为显示会话终止状态与故障码，见“验收后修复”。

### 4. 中途停止：通过

会话 `dc9b0f9e-4232-45b7-aa74-29dc3e521bd7`，启动后约 42 秒发送全局停止热键。

| 指标 | 结果 |
| --- | --- |
| 采集/产出 | 34 / 34 |
| 未处理 | 0 |
| 终止 | `cancelled / userCancelled / capturing` |
| 覆盖 | `partial` |
| 位置 1 | 2 视口、34 访问、顶部确认、底部未确认、`complete: false` |
| 检查点 | generation 7（34 件） |
| UI | 显示 34 条、部分覆盖、全部可导出 |

平台会话收尾日志为 `captureStopped=true drained=true released=true hotkeyErr=<nil>`，说明停止时采集、输入释放和热键清理均完成。

### 5. 正常退出：通过

10:51:51 日志出现：

```text
youkai: exit wrap-up complete
Youkai Dev quit
```

进程退出码 0；退出前没有活动扫描。随后冷启动历史数据仍可读取。

## 问题与观察

### 外部环境：云游戏断开

会话 `50a63c20-818d-42e2-adef-99e4d35d1451` 在启动后约 1 秒失败，0 件。游戏窗口显示：

```text
连接中断
因为您长时间未操作游戏，已中断连接
错误码：-1022
```

这是云客户端长时间无操作后的断连，不是扫描器逻辑错误。重新连接后需要重新准备页面，见下一条。

### 启动稳定性：一次进程消失

会话 `4566d045-4abb-4727-a2f0-216ad644238e` 于 10:48:14 被接收，随后 Youkai Dev 进程消失；无检查点、无 `exit wrap-up complete`、无 crash report，日志停在 `accepted`。统一日志提供了同刻线索：10:48:14.760 测试自动化的 `swift-frontend`（responsible `Lody`）对 Youkai Dev 发起 TCC 辅助功能授权请求，10:48:14.779 进程退出（`proc_exit`，无崩溃报告）。时间重合说明更可能是测试工具/TCC 交互或外部终止，而非扫描器自身崩溃；该次应用也不是受控 `dev3` 进程启动的实例。受控启动后的扫描没有复现同类退出。

结论：**测试环境侧高度可疑，仍待排除**。在没有可复现的应用侧证据前，不把该次消失计入扫描器故障，但也不宣称启动路径完全稳定。

### 受控启动后的首次扫描失败，重试通过

会话 `3d4e796a-e9d9-4cfb-af44-f41d677acb80` 在受控启动后约 6 秒失败：

```text
termination: failed / navigationMismatch / capturing
captured: 0
coverage: partial
observedScope.positions: 位置 1，1 视口，0 访问
```

失败发生在 `establishList` 等待仓库列表稳定的阶段，早于任何位置准备日志。立即重试的 `dc9b0f9e` 扫描正常进入位置 1、完成筛选与顶部确认并采到 34 件。因此本轮把该次失败记录为页面/码流就绪时序问题，具体触发条件待复现；不能据此确认 `navigationMismatch` 处理已完全覆盖重连后的页面状态。

### 0 件失败会话的结果页展示（验收后已修复）

验收时，`50a63c20`、`3d4e796a` 等 0 件失败会话的结果页显示：

- `结果：本次扫描（已保存 0 条）`
- `覆盖：部分覆盖`
- `排除：失败 0 件 · 未处理 0 件 · 其他 0 件`

“失败 0 件”是物品级统计，但页面没有把会话级 `termination`（例如 `navigationMismatch` 或 `-1022` 引发的失败）显示为失败原因。验收后已修复并验证：

- 历史覆盖率列显示 `失败：navigationMismatch`、`失败：pageUnverified`；取消记录显示 `已停止`。
- 结果页显示 `扫描失败：navigationMismatch（采集阶段），未保留任何条目`，或 `扫描已停止：结果只覆盖已采集的条目`。
- 0 件记录的状态与标题改为 `没有可导出的条目` / `无可导出条目`，不再显示“全部可导出”。
- 失败记录的空状态显示 `本次扫描失败，没有已保存的条目。`，不再沿用“还没有已保存的条目”。

以上已在 dev6 构建上打开真实历史记录验证：`3d4e796a` 显示失败原因，`dc9b0f9e` 显示已停止，`2bbfd723` 仍为 1,007 条、覆盖完整且无错误提示。

### 冷启动历史打开的加载竞态

一次冷启动后立即打开失败记录，结果页停留在 `正在加载最新检查点`；重启并等待初始化完成后再打开同一记录，正常显示。该现象疑似初始化快照与历史槽加载的竞态，尚未稳定复现；当前未修改该路径，列入待定位项。它不影响已打开记录的数据，但会让用户误以为记录为空。dev8 冷启动后立即打开同一失败记录则正常加载，本轮未复现。

### 停止方式提示

扫描页仅提示“点击本窗口的‘停止扫描’按钮”，未提示全局停止热键 `⌃⌥⌘S`。本轮热键工作正常；如果热键是正式支持的停止方式，应在 UI 文案中同步说明。

### 验收后修复（未提交）

在本报告对应的验收提交 `6233725` 之后，针对上述发现完成了两项代码修改：

- **失败/停止状态可见**：`HistoryRecord` 携带会话 `termination`；历史覆盖率列显示故障码，结果页显示会话终止说明；0 件记录的状态改为“没有可导出的条目”/“无可导出条目”。新增 `session_status_test.go`、历史与结果页渲染测试，并在 dev6 构建上用真实记录验证。
- **列表建立的一次有界重试**：`establishList` 在第一次等待结束时无条件再给一个 action budget（不再要求第一次已看到仓库判决）；仍未稳定才以 `navigationMismatch` 停止。新增 `TestEstablishListRetriesStalledStart` 与 `TestEstablishListRetriesSilentStart`，分别覆盖“首帧后码流停顿”和“首次等待完全没有可判定帧”。补测中 dev7 的首扫仍以 `navigationMismatch` 失败（当时还是“需先看到仓库判决”的旧条件），放宽后 dev8 连续三次短扫描（`1392881f` 17 件、`f4783805` 4 件、`d5eb4891` 13 件）均进入位置 1 并正常停止，没有再次失败；但三次都没有触发重试日志，说明成功来自首次等待本身恢复，重试分支仍只有单测覆盖。

修复后 `go test ./...` 与 `go vet ./...` 全部通过（含 `internal/game/zzz/collect`、`internal/ui`、`internal/app`）。这些改动尚未提交，报告保留验收时与验收后的区分。

### 未覆盖项

- Windows 真机本轮未执行，不宣称 Windows 通过。
- 本轮未重新执行逐件校对、待校对导航、截图清理、设置页与更新流程；这些沿用既有报告，不在本轮结论内。
- 长闲置断开后的自动重连不在应用职责内；本轮记录的是断连对扫描启动的影响。
- 本轮的 1,200 ms 滚动等待与顶部确认在当前客户端上未再出现 `positionLost`，但这不能证明所有客户端都不会因云惯性多滚一行；结构性 3 行步长与 `ScrollStepRows` 校准仍属于后续改进项。

## 修复后真机回归（2026-10-26 14:08）

本轮修复（活动记录编辑保护、滚动位移唯一性、选中/转盘连续性守卫、崩溃输出）在 `4dbe84f` 工作区上完成后，重新做了一次真实全量扫描。应用用独立进程启动，避免 `go tool mygo dev` 的后台终端被回收：

```text
YOUKAI_GAME_SOURCE=traverse \
  .mygo/dev/darwin-arm64/Youkai Dev.app/Contents/MacOS/Youkai Dev
```

| 项目 | 结果 |
|---|---|
| 会话 | `006cfef2-0c35-42c0-bb6a-a299396a307f` |
| 时间 | 13:50:41–14:08:58，约 18 分 17 秒 |
| 采集 / 产出 / 未处理 | 1,007 / 1,007 / 0 |
| 自动接受 | 1,007 件 |
| 终止 | `completed / verifiedEnd / capturing` |
| 覆盖 | `complete` |
| 检查点 | 128 代，最终约 10.39 MB |
| 证据 | 10,831 个文件，约 55.5 MB |
| 守卫故障 | `positionLost`、`selectionMoved`、转盘不一致、`navigationMismatch` 均为 0 |

六位置账目与修复前的 1,007 件全量扫描一致：

| 位置 | 视口 | 访问 | 顶部 | 底部 | 完整 |
|---|---:|---:|---|---|---|
| 1 | 6 | 96 | 是 | 是 | 是 |
| 2 | 7 | 114 | 是 | 是 | 是 |
| 3 | 7 | 103 | 是 | 是 | 是 |
| 4 | 15 | 229 | 是 | 是 | 是 |
| 5 | 16 | 246 | 是 | 是 | 是 |
| 6 | 14 | 219 | 是 | 是 | 是 |

共 59 次已提交滚动：54 次 4 行、3 次 1 行、1 次 3 行、1 次 2 行。`record.Load` 复核结果：0 件 interrupted、覆盖未降级、1,007 件证据文件大小与 SHA-256 全部通过。CSV 为界面实际导出，JSON 由同一 `BuildUsableExport` 数据层生成；两者均为 1,007 件、`excludedCount: 0`、全部 `autoAccepted` / `passed`。

同一次回归中还有一段被外部终止的会话 `f3ce9fdd-a6ec-4512-a593-694e425b8b6e`：完成位置 1（96 件）并进入位置 2 后，应用进程在 13:45:13 消失，检查点 26 保存了 197 件（196 produced、1 unprocessed），无终止记录、无崩溃输出。该次由代理后台终端回收造成，不是扫描逻辑故障；其守卫同样没有误报。

第三项复核使用该 197 件会话的 `/tmp` 副本，未触碰全量证据：`SaveRevision` 将 item 1 保存为 revision 1 / `humanConfirmed`；`DeleteEvidence` 移除 2,167 个文件、约 11.3 MB，记录仍可 `Load` 为 197 件、覆盖 `partial`、证据索引为空。`go test ./internal/ui` 与 `./internal/app` 全部通过。真实界面的历史点击与破坏性清理按钮仍未由自动化点击。

**本轮未关闭：** 顶部确认仍有 3 个位置首次尝试 `lost`，每次约多等 5 秒；筛选重置是否生效仍是已接受的残余风险（用户现场观察认为生效）；历史页面的可视化重开未在本次自动化检查中点击复核。顶部确认的首次拖拽已追加“先等待静止基准帧”（`awaitTopBase`）修复，单元测试与全量 Go 测试通过，但下列真机全量运行早于该修复，仍需一次短扫复测。

### 顶部确认修复后的短扫复测

会话 `685f16b7-ed49-4172-be75-9c8782561ed5` 在修复构建上运行到位置 4，用户手动停止。位置 1–3 的顶部确认第一次尝试直接接受；位置 4 第一次尝试仍为 `lost`（零位移误差 5.51），但滚动条稳定在顶部，日志记录 `top confirmation accepted: the thumb stayed at the top`，没有再执行第二次尝试。该结果验证了“未定 + 滑块稳定”的补充证据；修复在位置 1–4 上消除了第二次 5 秒等待。

这次运行最终记录为 `ended (failed): positionLost`，而不是 `cancelled`：383 采集、382 produced、1 unprocessed，位置 1–3 完整、位置 4 部分，平台会话正常关闭。检查点中第 383 件的 `unprocessedReason` 是 `interrupted`，说明第一终止触发是故障而不是用户停止；停止发生在故障之后，或至少没有先取得终止权。`positionLost` 的所有滚动/位移分支都会先写日志，日志中没有对应行，因此剩余可能是 `visitSlot` 的选中格位移守卫（该分支此前不写日志）。已在该分支补充前后选中 slot、最后帧和 pre-click 帧的日志；下次复现可直接区分是否是云客户端在点击后自动移动了选中格。

### 选中格守卫的现场复现

会话 `e3bf895b-73d8-4dde-8350-22d1b4f67215` 从位置 1 开始跑完位置 1–5，在位置 6 的视口 5、目标 slot 18（row 4, column 2）停止：886 captured、885 produced、1 unprocessed，终止 `failed / positionLost / capturing`，覆盖 `partial`。位置 6 尚未完成。

新日志给出了具体原因：

```text
selection moved from slot 17 to -1 between visits
(last frame 58591 reused=false, pre-click frame 58601 reused=false)
```

上一件（seq 886，row 4, column 1）的详情帧仍是选中状态，随后 10 帧内的 pre-click 帧选中标记变为 `-1`；两帧都是 fresh，因此不是停表/复用帧造成的旧画面，也不是手动停止。守卫在点击 slot 18 前停止，seq 886 因中断未产出；885 件已产出记录保持完整。这个结果说明守卫本身按设计工作，但单帧选中标记消失是否是云客户端动画，需要先复核再决定是否可继续。

另外，本轮在游戏里先切到位置 4 再开始，记录的 `observedScope.startPosition` 是 4，但外层遍历代码固定按 `for pos := 1; pos <= 6; pos++` 执行，所以实际仍从位置 1 开始。`startPosition` 只是准备前的观测元数据，不是扫描起点；之前让用户先切到位置 4 的说明是错误的。

针对这次单帧消失，`visitSlot` 已改为：发现选中格变化后，在一个 action budget 内继续观察，只有选中格回到上一件或稳定在另一格才产生结论；回到上一件则继续，持续消失/移动才按 `positionLost` 停止。新增 `awaitSelectionReturned` 及瞬时消失/持续消失两个单元测试；完整 `go test ./internal/game/zzz/collect` 通过。该修复仍需一次现场复测确认能否跨过 seq 886。

顶部确认在本次六个位置均无第二次尝试：位置 1/2/4/5 首次 Stationary 接受，位置 3/6 首次 Lost 但滑块稳定在顶部，日志记录 `the thumb stayed at the top`。因此顶部优化在 1–6 全范围成立。

## 证据

```text
检查点（全量）:
~/Library/Application Support/Youkai Dev/sessions/2bbfd723-881d-492e-a28d-27058063014b/checkpoints/128.json

检查点（修复后全量）:
~/Library/Application Support/Youkai Dev/sessions/006cfef2-0c35-42c0-bb6a-a299396a307f/checkpoints/128.json

检查点（修复后中途终止）:
~/Library/Application Support/Youkai Dev/sessions/f3ce9fdd-a6ec-4512-a593-694e425b8b6e/checkpoints/26.json

检查点（中途停止）:
~/Library/Application Support/Youkai Dev/sessions/dc9b0f9e-4232-45b7-aa74-29dc3e521bd7/checkpoints/7.json

应用日志:
~/Library/Logs/Youkai Dev/youkai.log

受控启动日志:
/tmp/youkai-macos-test-2026-10-25/dev3.log
/tmp/youkai-macos-test-2026-10-25/dev4.log
/tmp/youkai-macos-test-2026-10-25/dev6.log
/tmp/youkai-macos-test-2026-10-25/dev7.log
/tmp/youkai-macos-test-2026-10-25/dev8.log

导出:
~/Downloads/youkai-2026-10-26-2bbfd723.youkai.json
~/Downloads/youkai-2026-10-26-2bbfd723.youkai.csv
~/Downloads/youkai-2026-10-26-006cfef2.youkai.json
~/Downloads/youkai-2026-10-26-006cfef2.youkai.csv
```

## 复现方式

```text
CGO_ENABLED=1 YOUKAI_GAME_SOURCE=traverse go tool mygo dev
```

1. 打开《云·绝区零》并进入驱动盘仓库。
2. 应用内点击“开始扫描”，保持游戏窗口在前台。
3. 中途停止使用 `⌃⌥⌘S`，或点击应用内“停止扫描”。
4. 在结果页导出 JSON/CSV，退出后重新打开应用进入历史，确认记录仍可读。
