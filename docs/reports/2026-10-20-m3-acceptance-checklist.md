# M3 规模测试与正式验收清单

更新日期：2026-10-20。状态：合成数据与自动化规模检查已完成；macOS 六位置现场轮已执行两次。较早的 `5d3200e3` 轮覆盖 `unknown`、排序 `unverified`；随后的 `5387f071` 干净复跑完成六位置，`coverage:"complete"`，排序标签“稀有度”已验证（仅标签，不读取升降序方向），1005 件、0 失败。2026-10-20 本机 macOS 原生体验（托盘、关窗隐藏、⌘Q/托盘退出、第二实例、`youkai://open`、保存对话框中文输入、文件对话框取消/覆盖/无权限）已执行并通过空闲态验收；扫描中停止/转交与磁盘不足未测。**正式验收仍未通过**：装备者身份、14 件装备状态不一致、相邻同详情保护、滚动 Lost、管理 `none`、Windows 轮 ② 与扫描中托盘/深链接行为仍缺证据；v2 已把装备字段改为可选 warning，不再阻止导出。本文把“已自动化覆盖”“已用合成数据覆盖”“现场执行结果”和“必须真机执行”分开记录，未执行或证据不足的项目不是通过。

- 被测基线：`main` `b4fa6b2`（规模与验收清单已由 PR #49 合并）；较早现场轮运行于 `main` `4c5f014`；干净覆盖复跑运行于 `main` `7609a32` 加排序/覆盖改动的本地构建。
- 本文对应分支：`test/m3-scale-acceptance`。
- 合成数据只验证界面与数据层在规模下的行为，不证明扫描质量、OCR 正确率、真实库存覆盖或游戏兼容性。
- 正式验收要求见 [T5 第 6 节](../specs/t5-zzz-inventory-traversal.md#6-保存什么证据怎样验收)、[T6 第 5 节](../specs/t6-review-export-workflow.md#5-正式功能验收与后续)与 [T7 交付门槛](../specs/t7-public-release-acceptance.md#2-首版交付门槛)。

## 1. 结论摘要

| 验收面 | 状态 | 证据与边界 |
| --- | --- | --- |
| 1,000 行结果数据与浏览 | 已自动化 | `BuildRecordView` 构建 1,000 行；真实结果视图打开并渲染 1,000 行记录。仅证明数据/表格接线，不是产品性能基准。 |
| 10,000 行结果数据 | 已自动化（数据层） | `BuildRecordView`、`pendingCount`、`nextPendingAfter` 在 10,000 行下执行；**没有渲染 10,000 行 UI**。 |
| 问题导航与待校对计数 | 已自动化 | 混合已结算/待校对行，验证计数、向前查找、跳过已结算行与不回头。 |
| 连续校对 | 已自动化 | 在 1,000 行模型上连续执行 100 轮“确认并下一项”，每轮走假服务保存；未写 1,000 次真实检查点。 |
| JSON/CSV 导出（1,000 件可用） | 已自动化 | 1,100 条记录中 1,000 件可用、100 件排除；核对纳入/排除数、顺序、目录子集与两种格式的同一批物品。 |
| JSON/CSV 导出（10,000 条数据层） | 已自动化（数据层） | 10,000 条中 9,000 件可用；两种格式只在内存生成，不落盘。 |
| 图片缓存上限与迟到回包 | 已自动化 | 1,000 行记录中连续访问 100 行、每行 2 张图，缓存不超过配置上限；受控迟到回包仍挂在原证据键上。 |
| 终止、恢复与保存失败 | 已自动化 | 复用现有服务/存储测试；见 §2.2。 |
| 截图与记录清理 | 已自动化 | 复用现有服务、存储与界面测试；使用临时目录。 |
| 编辑确认与旧修订冲突 | 已自动化 | 复用现有界面与服务测试；见 §2.2。 |
| 窗口生命周期、第二实例与深链接 | 已自动化（假窗口/托盘） | PR #48 的 `cmd/youkai/lifecycle_test.go`；真实托盘、系统深链接与窗口行为仍待真机。 |
| macOS 六位置完整字段验收 | **已现场执行，未通过完整验收** | 2026-10-20，`macosCloud`。`5d3200e3` 轮覆盖 `unknown`、排序 `unverified`，v2 可用导出 1005 件、0 排除；`5387f071` 干净复跑六位置，覆盖 `complete`、排序标签“稀有度”已验证（仅标签），1005 件、0 失败。装备者身份、同详情保护、Lost 与 `none` 样本仍缺证据。见 §4.1、§4.2。 |
| Windows 实测轮 ② | **待用户现场执行** | DPI、GDI、SendInput、托盘、安装包与真实客户端均未验证。 |
| macOS 托盘/深链接/输入法/文件对话框 | **部分通过（2026-10-20）** | 托盘状态/打开/退出、关窗隐藏、⌘Q/托盘退出、第二实例、`youkai://open`、保存对话框中文输入、取消/覆盖/无权限已验证；扫描中停止/转交与磁盘不足未测。见 §4.3。 |

## 2. 已自动化覆盖

### 2.1 本轮新增的规模测试

| 测试 | 位置 | 覆盖内容 |
| --- | --- | --- |
| `TestScaleBuildRecordViewOneAndTenThousandRows` | `internal/ui/scale_test.go` | 1,000/10,000 行行数、序号、待校对计数、向前查找、跳过已结算行、不回头。 |
| `TestScaleResultsBrowserRendersThousandRows` | `internal/ui/scale_test.go` | 打开 1,000 行历史记录，真实视图渲染标题、待校对计数与首屏结构化行。 |
| `TestScaleContinuousReviewAdvancesThroughPendingRows` | `internal/ui/scale_test.go` | 1,000 行上连续 100 轮确认/保存/定位下一项，行质量、选中位置、待校对计数与提示逐轮核对。 |
| `TestScalePendingNavigationHandlesTenThousandRows` | `internal/ui/scale_test.go` | 10,000 行下的 `pendingCount` 与 `nextPendingAfter`，含末行不回头。 |
| `TestScaleEvidenceCacheStaysBoundedAcrossItems` | `internal/ui/scale_test.go` | 1,000 行记录中访问 100 行、每行 2 张图；缓存不超过上限，键与位图一一对应。 |
| `TestScaleLateEvidenceCompletionStaysKeyed` | `internal/ui/scale_test.go` | 第一行图片在选中切到第二行后才返回；迟到位图留在第一行证据键上，第二行显示不变。 |
| `TestScaleExportThousandUsableItems` | `internal/app/export_scale_test.go` | 1,100 条记录、1,000 件可用；JSON/CSV 纳入与排除数、顺序、目录子集、同一冻结快照。 |
| `TestScaleExportTenThousandItemsDataLayer` | `internal/app/export_scale_test.go` | 10,000 条记录、9,000 件可用；两种格式的数据层生成、顺序与末行。 |

本地执行记录（2026-10-20，Apple Silicon Mac）：

```text
go test -count=1 ./internal/ui/... ./internal/app/... ./internal/record/...
ok  internal/ui      0.656s
ok  internal/app     3.072s
ok  internal/record  1.575s
real 3.71s

go test -count=1 ./...
ok  ./...            real 190.87s
```

`internal/game/zzz/collect` 的完整回放测试约占 189s，是整仓耗时的主要来源；本轮新增测试本身在秒级以内。单测日志中的规模耗时（同机观察值，不是性能预算）：

| 场景 | 观察值 |
| --- | --- |
| `BuildRecordView` 1,000 行 | 约 3–6ms |
| `BuildRecordView` 10,000 行 | 约 35–41ms |
| 1,000 行结果视图打开并渲染 | 约 57–59ms |
| 100 轮连续确认（1,000 行模型） | 约 3ms |
| 1,000 件可用 JSON / CSV | 约 8ms / 1ms；约 1.2MB / 217KB |
| 10,000 条记录 JSON / CSV | 约 43ms / 7ms；约 10.8MB / 2.0MB |

这些数字只说明测试数据规模下没有明显退化，不是端到端 UI 帧率、内存或导出磁盘性能的验收结论。

### 2.2 复用的现有自动化覆盖

以下检查不是本轮新写，但在本分支基线上重新执行并通过：

| 范围 | 代表测试 |
| --- | --- |
| 终止、收尾与恢复 | `internal/app/termination_test.go`：`TestWatcherFaultKeepsAdmittedResultsAndFinalizesOnce`、`TestQuitRefusesToFinishWhileTheFinalSaveIsMissing`、`TestWrapUpDoesNotReviveAStaleSaveFailureAfterRetry`、`TestLoadRecordRecoversWithoutResumingTheScan`。 |
| 检查点回退与中断标记 | `internal/record/load_test.go`：`TestLoadFallsBackOverCorruption`、`TestLoadMarksInterruptions`、`TestLoadDoesNotUpholdCompleteCoverage`。 |
| 保存失败与重试 | `internal/record/store_test.go`、`internal/app/service_test.go`、`internal/ui/view_test.go` 的保存失败/重试用例。 |
| 截图与记录清理 | `internal/app/cleanup_test.go`、`internal/record/cleanup_test.go`、`internal/ui/cleanup_test.go`。 |
| 编辑确认、未知字段与旧修订 | `internal/ui/edit_test.go`：`TestEditSavePersistsRevision`、`TestConfirmAndNextThroughToEmpty`、`TestRevisionConflictReloadsCurrentRevision`、`TestLateSaveCannotCrossItems`、`TestSaveKeepsExplicitUnknownsPending`；`internal/app/review_test.go`。 |
| 导出契约 | `internal/app/export_test.go`：字段、单位、目录子集、覆盖结论、空导出与 JSON/CSV 同源。 |
| 窗口生命周期与第二实例 | PR #48；`cmd/youkai/lifecycle_test.go` 覆盖默认隐藏、关闭即退出、托盘失败回退、显式退出收尾、第二实例恢复/停止、深链接过滤与扫描中深链接。 |
| 独立 Clear 与相邻同详情的规则层 | `internal/game/zzz/collect/inventory_test.go`：`TestTraverseIndependentClear...`、`TestSinglePageSameDetail...`。这些是脚本化页面序列，不是真实客户端证据。 |

### 2.3 合成数据的明确边界

- 规模项使用共享的完整结构化载荷，不生成 10,000 张真实截图，也不运行 OCR。
- 1,000 行结果视图只证明真实表格/详情接线；10,000 行只到数据层，**不能**写成“10,000 行 UI 压力通过”。
- 缓存项只验证解码位图的条目上限与身份键，不测量 GPU 保留、原生位图内存或整机内存峰值。
- 导出项只验证数据层与文件内容，不经过系统文件对话框，也不写用户目录。
- 连续校对使用假服务保存 100 轮，不验证真实检查点轮转、磁盘延迟或断电恢复；后者由现有存储/终止测试与真机清理验收分别覆盖。
- 合成数据不能证明真实库存没有漏扫、重扫、详情错配或提前完整。

## 3. 待用户现场执行的正式验收

以下项目原本均待用户现场执行。macOS 六位置已于 2026-10-20 执行一次，原始证据见 §4.1；其余项目仍待执行。只有实际执行并留下本节要求的证据，且满足全部通过条件，才能在交付记录中改为通过。

### 3.1 macOS 六位置完整字段验收

#### 环境与启动

- Apple Silicon Mac、已确认的系统版本与显示配置；《云·绝区零》云客户端已登录，手动进入驱动盘仓库页并保持前台。
- 先准备 OCR 资源：

```sh
go run ./tools/fetchdeps -platform current
```

- 正式验收使用独立 Clear 校验（只在验收流程启用，不是日常产品行为）：

```sh
CGO_ENABLED=1 \
YOUKAI_GAME_SOURCE=traverse \
YOUKAI_INDEPENDENT_CLEAR=1 \
go tool mygo dev
```

- 开发版数据根与日志：`~/Library/Application Support/Youkai Dev/`、`~/Library/Logs/Youkai Dev/youkai.log`。不要把真实用户数据或完整截图提交进仓库。
- 扫描期间不要切换应用、不要手动操作游戏；停止用界面“停止”或全局热键 ⌃⌥⌘S。

#### 每个位置 1–6 的检查

对转盘位置 1 至 6 分别记录以下结果。扫描器会自动按 1→6 推进，但验收记录必须逐位置填写，不能用总件数替代。

| 检查项 | 执行与预期证据 | 失败处理 |
| --- | --- | --- |
| 转盘位置 | 日志出现 `position N: wheel selected`，`capture.position` 为 1–6，且每位置 `viewportIndex` 从 0 重新计数。 | 位置未选中或跳位：记录 `navigationMismatch`/实际日志，停止。 |
| 独立 Clear | 日志出现 `position N: filter reset and S selected`；独立 Clear 路径在重置后先关闭面板并要求漏斗为 clear，再重新打开勾选 S。若重置未清除旧筛选，必须报 `navigationMismatch` 并停止。 | 出现非 clear 漏斗、面板未关闭或 S 未生效：记录失败码，停止，不继续录入。 |
| 顶部滑块 | 日志出现 `position N prepared: S filter active, top confirmed` 与 `top confirmation accepted: the thumb stayed at the top`；顶部证据是滑块起点不变（±2 像素）且仍为 Top。 | 连续无进展、Lost 预算耗尽或顶部未确认：停止并保留已接纳结果。 |
| Lost | 滚动期间若出现 `lost=true`，确认实现没有立即提交未确认位移：在 `ScrollBudget` 内继续观察，预算耗尽才停止；不得把单个 Lost 动画帧当成结束。自然未触发 Lost 时记“未触发”，不能据此判 Lost 行为通过。 | 预算耗尽仍不停稳：记录 `Lost` 与停止位置；不降低阈值、不猜测跳过的行数。 |
| 相邻同详情 | 找到至少一组相邻且详情完全相同的 S 盘（或明确的真实重复盘样本），确认两件各自出现、各计一件；日志可出现参照往返，但不得按属性/哈希合并。 | 无法归属：`detailUnattributed` 停止并保留未解决位置；该位置记未通过。 |
| 管理状态与装备 | 逐位置抽查锁定、弃置、无标记；已装备、未装备、他人装备。列表盘格右下角角标与顶部“拆解”左侧指示在锁定维度一致；顶部不能区分弃置与无标记，弃置/无标记以列表角标为准；详情等级右侧头像与列表格右上角头像一致。冲突或不可读必须为 `null`，不能落成 `none`/未装备。 | 任一冲突或不可读：记录 `null` 与实际画面；不得改写成合法状态。 |
| 覆盖结论 | 当前排序标签未验证时，UI 与 JSON 只能显示 `partial`/`unknown`，不能显示完整覆盖。若六位置均走完但排序未验证，记录“六位置走完、覆盖为部分”，这不是失败，也不能写成 complete。只有同时满足 T5 §5 全部条件才允许 complete。 | 出现未经排序验证的 complete：按缺陷停止验收并保留 JSON/UI 证据。 |
| 末页与计数 | 每位置保留末端证据（滑块 Bottom + 最后一步不足整步，或 Bottom + Stationary）；若有人工基准总数，逐件核对。 | 只有“数量相等”不能证明没有漏一件又重一件。 |
| 字段与导出 | 扫描收尾后核对待校对计数；抽查 JSON/CSV 的 `position`、`viewportIndex`、`managementState`、`equipped`、单位、序号与排除数；JSON 与 CSV 来自同一修订。 | 字段不一致、同一修订不同批次或排除数不符：保留导出文件与记录，停止验收。 |

#### 预期日志锚点

```text
youkai: collect: position N: wheel selected
youkai: collect: position N: filter reset and S selected
youkai: collect: position N prepared: S filter active, top confirmed
youkai: collect: position N: top confirmation accepted: the thumb stayed at the top
youkai: collect: position N scroll <before> -> <after>: R rows, M correspondences
```

独立 Clear 路径不额外打印一条“clear”日志；它的证据是重置后关闭面板时漏斗为 clear、未出现 `navigationMismatch`，以及随后 S 生效的 “filter reset and S selected”/“prepared” 行。如果日志无法证明这两个检查点，按未验证处理。

#### 记录字段

每个位置至少记录：位置号、扫描开始/结束时间、会话 ID、日志路径、是否触发 Independent Clear、顶部确认结果、滚动 Lost 次数、相邻同详情是否出现、管理/装备抽查件数、覆盖结论、待校对数量、导出文件路径与 SHA-256、失败码（若有）。

### 3.2 Windows 实测轮 ②

| 项目 | 执行与预期证据 | 失败处理 |
| --- | --- | --- |
| 安装包与识别自检 | 用 GitHub Actions 构建的安装包在 Windows 11 实机安装；`YOUKAI_SELFTEST` 退出码 0 并识别固定样本。 | 安装或自检失败：记录版本、日志、DLL/VC++ 状态，停止后续验收。 |
| 窗口身份与 DPI | 完整客户端与云客户端各记录窗口身份、DPI、客户区几何；DPI 不一致或几何变化应拒绝输入/停止。 | 错窗口、坐标错位：立即停止，不猜测坐标。 |
| GDI 采集与 SendInput | 真实前台、遮挡、失焦、拖拽、点击、停止热键；记录实际输入效果与停止时延。 | 输入未生效或失焦未停止：记录平台差异，暂停该客户端验收。 |
| 托盘/菜单栏与关闭 | 默认关闭隐藏、显式退出收尾、托盘恢复、托盘失败回退。 | 托盘不可用或退出未收尾：保留窗口与日志，按失败记录。 |
| 深链接与第二实例 | 浏览器/命令行唤起 `youkai://open`；第二实例恢复首实例并停止活动扫描。 | 未恢复或误停止：记录 URL、进程与日志。 |
| 输入法与文件对话框 | 在编辑字段输入中文、小数、百分比；导出 JSON/CSV 的系统保存对话框覆盖取消、同名覆盖、无权限与磁盘不足。 | 输入法提交错误、对话框取消仍写文件或覆盖失败：保留输入与文件，按失败记录。 |

Windows 实测轮 ② 完成前，不宣称 Windows M3 通过。

### 3.3 macOS 原生体验项

| 项目 | 执行与预期证据 |
| --- | --- |
| 托盘/菜单栏 | 关闭窗口默认隐藏、菜单栏“打开/停止/退出”可用；托盘创建失败时关闭改为退出并显示说明。 |
| 深链接 | `open 'youkai://open'` 恢复窗口；空闲时不误停扫描，扫描中先停止再显示。 |
| 第二实例 | 已运行时再次启动不建立第二窗口，由首实例恢复。 |
| 输入法 | 编辑套装、主词条、数值等字段时中文输入、候选提交、小数与百分号不丢失；保存后重开仍一致。 |
| 文件对话框 | 导出 JSON/CSV 的取消、同名覆盖、无权限、磁盘不足；取消不改变记录，失败保留内部会话。 |

自动化只覆盖了生命周期与导出接线的替身；上述系统对话框、输入法与真实托盘必须实机确认。

## 4. 正式验收执行记录

### 4.1 2026-10-20 macOS 现场轮（macosCloud）

- 构建：`main` `4c5f014`，开发版；启动参数 `YOUKAI_GAME_SOURCE=traverse`、`YOUKAI_INDEPENDENT_CLEAR=1`。
- 环境：Apple Silicon Mac；macOS 云客户端 `com.miHoYo.cloudgames.Nap`；布局 `maccloud-1920x1080`，几何版本 1，20 格；游戏窗口 1164×762。
- 会话：`5d3200e3-9548-4873-863d-a6694b4c7161`；`startedAt` 2026-10-20T03:33:11Z，`endedAt` 03:49:36Z（本地 11:33:11–11:49:36）；起始转盘位置 2，起始筛选已开启，排序 `unverified`。
- 检查点：`~/Library/Application Support/Youkai Dev/sessions/5d3200e3-9548-4873-863d-a6694b4c7161/checkpoints/127.json`（generation 127）。
- 日志：`/tmp/youkai-field-run.log`；应用日志 `~/Library/Logs/Youkai Dev/youkai.log`。
- 结果：1005 captured / 1005 produced / 0 unprocessed / 0 failed；终止 `completed` / `verifiedEnd`；覆盖 `unknown`。
- 自动接受：875/1005（87.1%）`autoAccepted`，130/1005（12.9%）`needsReview`；130 件全部因装备字段不可用（`equippedStateUnavailable`），没有 OCR 解析问题。该待校对比例高于 T3 的 5% 候选门槛，但本轮是全库存现场轮，不是受控验收集。
- 导出策略：130 件被排除不是 OCR 解析失败，而是 v1 `fieldPolicy.equipped=required` 使装备字段缺失/未识别时整件 `validation=unavailable`。v2 已把 `equipped` 降为可选字段，导出保留 `null` 或 `{state:"equipped",agentId:null}`，并保留字段级 warning。
- v2 重新构建：同一 checkpoint 127 按 v2 策略导出 1005 件、排除 0 件；116 件 `equipped/agentId=null` 与 14 件 `equipped=null` 均以 warning 保留。上面 JSON/CSV 的 875 件/130 排除是 v1 现场导出文件的历史结果，不覆盖原文件。
  - v2 验证文件：`/tmp/youkai-2026-10-20-5d3200e3-v2.youkai.json`，1,196,421 B，SHA-256 `61f628b2271ee28751085ee626f05231cd92f452a88260dfb5128fad1af09e3f`；`/tmp/youkai-2026-10-20-5d3200e3-v2.youkai.csv`，1,006 行，SHA-256 `366ae4cc7d46c9dffa83c52c4465d96e28f34f38948242e542e59b7e3dd322b3`。
- 范围决定：装备者身份按用户决定延后处理；本轮其余功能不以 `agentId` 为阻塞项。
- 截图：会话目录约 93 MB，已保留，未执行删除。
- 副词条数据核对：1005 件共 3771 条副词条，原始 OCR 行到 `attribute`/`value`/`unit`/`enhancementRolls` 的比对为 0 不一致；所有数值都符合对应词条的基础档位倍数（如 `暴击率+412%` → `critRate/12/percent/rolls=4`，`生命值+3448` → `health/448/flat/rolls=3`）。UI 上“挤在一起”是展示排版问题，不是数据解析问题。
- 导出（v1 现场文件）：JSON 875 件、排除 130 件；CSV 875 行 + 表头；两者 `revision=0`、序号集合一致；`fieldPolicy={management:"required",equipped:"required"}`。
  - JSON：`~/Downloads/youkai-2026-10-20-5d3200e3.youkai.json`，1,011,680 B，SHA-256 `2264791c3b3bb504b0f905a865aa6bf8566211a316f137d14141885da95fb664`。
  - CSV：`~/Downloads/youkai-2026-10-20-5d3200e3.youkai.csv`，185,965 B，SHA-256 `43a77ad67a8acffb92de4e0b4b451dbc55684909fc3a9c9eb3935a18cf98cbf4`。
- 数量：本轮 1005 件；库存跨轮可变，不以旧轮 995 件作基线，也不做跨轮数量一致性判断。

#### 逐位置记录

| 位置 | 开始（本地） | 结束（本地） | 件数 | viewport | 序号 | 待校对 | 滚动 Lost | 独立 Clear | 顶部确认 | 末端证据 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 11:33:26 | 11:35:06 | 97 | 6（0–5） | 1–97 | 23 | 0 | 通过 | attempt 1 接受 | seq 97，`bottom=true` |
| 2 | 11:35:06 | 11:37:01 | 114 | 7（0–6） | 98–211 | 22 | 0 | 通过 | attempt 1 接受 | seq 211，`bottom=true` |
| 3 | 11:37:01 | 11:38:43 | 104 | 7（0–6） | 212–315 | 21 | 0 | 通过 | attempt 1 接受 | seq 315，`bottom=true` |
| 4 | 11:38:43 | 11:42:18 | 225 | 14（0–13） | 316–540 | 21 | 0 | 通过 | attempt 1 接受 | seq 540，`bottom=true` |
| 5 | 11:42:18 | 11:46:13 | 249 | 16（0–15） | 541–789 | 24 | 0 | 通过 | attempt 1 接受 | seq 789，`bottom=true` |
| 6 | 11:46:13 | 11:49:36 | 216 | 14（0–13） | 790–1005 | 19 | 0 | 通过 | attempt 1 接受 | seq 1005，`bottom=true` |

逐位置日志锚点均为 `position N: wheel selected`、`filter reset and S selected`、`top confirmation accepted: the thumb stayed at the top`、`prepared: S filter active, top confirmed`；未出现 `navigationMismatch`、`detailUnattributed`、`positionLost` 或 `evidenceFailed`。

#### 检查项结论

| 项目 | 结果 | 证据或说明 |
| --- | --- | --- |
| 六位置 1–6 转盘与准备 | 通过（日志级） | 6/6 出现选中、独立 Clear、顶部确认与 prepared；每个位置 `viewportIndex` 从 0 重新计数。 |
| 独立 Clear（`YOUKAI_INDEPENDENT_CLEAR=1`） | 通过（日志级） | 6/6 出现 `filter reset and S selected`，未出现 `navigationMismatch`。 |
| 顶部滑块确认 | 通过（日志级） | 6/6 在 attempt 1 内接受；每次先出现一次零位移 `lost=true`（best shift 0），随后 `top confirmation accepted`，thumb 位置 226/227，±2 像素内。 |
| Lost 预算与安全停止 | 未触发 | 6 次 `lost=true` 都是顶部确认尝试中的零位移误判，随后接受；列表滚动期间未触发 Lost。不能据此判定 Lost 行为通过。 |
| 相邻同详情双盘 | 部分证据，疑似绕过保护 | seq 806/807：位置 6、viewport 0、row 4 col 0/1，文本与结构化字段完全一致，检查点和导出中各计一件（CSV 第 693/694 行）。但两件均为 `transitionVerified`，整轮没有 `referenceVerified`；保存的行裁剪 MAD 仅 0.12–0.57，远低于 `DetailSameMax=2.0`。可能是预点击详情基线未落在紧邻前一件，或云视频压缩噪声导致“详情已变”误判；需受控复现，当前不能判定同详情保护已生效。 |
| 管理状态三态 | 部分通过 | 全量：`locked=998`、`discarded=7`、`none=0`、`null=0`。7 件弃置均为列表角标 `discarded` + 顶部 `unlocked`；未取得无标记样本，`none` 路径未验证。 |
| 装备与装备者 | 字段未完成（v2 可选） | 全量：`unequipped=875`、`equipped=116`、`null=14`；116 件已装备的 `agentId` 全部为 `null`。14 件 `null` 都是详情/列表头像存在性不一致，按契约保留为 `null`。v2 将这 130 件作为装备字段 warning 保留，不再阻止导出。 |
| 覆盖结论 | 未通过（如实为未知） | `coverage.status=unknown`，`sort=unverified`；未错误显示 `complete`，但也没有完成覆盖验收。 |
| 待校对计数与连续校对 | 部分 | v1 现场为 875 件 `autoAccepted`（87.1%）、130 件 `needsReview`（12.9%），全部因装备字段不可用；v2 将这 130 件重新归类为装备字段 warning，待校对为 0。本轮未执行“确认并下一项”连续校对。 |
| JSON/CSV 导出与同修订 | 通过（导出本身） | v1 现场文件为 875 件可用、130 件排除；v2 从同一 checkpoint 重新构建为 1005 件、0 排除，JSON/CSV 序号集合一致、`revision=0`。116 件 `equipped` 与 14 件 `null` 以 warning 保留，CSV 对应字段为空。 |
| Windows 轮 ② | 待执行 | 未执行。 |
| macOS 托盘/深链接/第二实例/输入法 | 通过（空闲态） | 托盘状态“就绪”、打开主窗口、退出；关窗隐藏后托盘恢复；第二实例只保留一个进程并恢复窗口；`youkai://open` 恢复、未知 URL 保持隐藏且不记录 query；保存对话框输入法可提交“测试”。扫描中停止/转交未测。 |
| macOS 文件对话框 | 通过（取消/覆盖/无权限） | 取消不改变文件；同名覆盖出现替换确认并成功；只读目录写入失败并显示 permission denied，未留下临时文件。磁盘不足未测。 |

### 4.2 2026-10-20 六位置覆盖复跑（macosCloud）

- 构建：`main` `7609a32` 加排序标签探针与覆盖账目改动的本地开发版；启动参数 `YOUKAI_GAME_SOURCE=traverse`。
- 环境：Apple Silicon Mac；macOS 云客户端 `com.miHoYo.cloudgames.Nap`；布局 `maccloud-1920x1080`，几何版本 1，20 格；游戏窗口 1164×762。
- 会话：`5387f071-d2b6-4b75-827a-25927e7b0148`；`startedAt` 2026-10-20T07:27:04Z，`endedAt` 07:43:04Z（本地 15:27:04–15:43:04）；起始转盘位置 3，起始筛选 `active`。
- 检查点：`~/Library/Application Support/Youkai Dev/sessions/5387f071-d2b6-4b75-827a-25927e7b0148/checkpoints/128.json`（generation 128）。
- 结果：1005 captured / 1005 produced / 0 unprocessed / 0 failed；终止 `completed` / `verifiedEnd`；`coverage.status="complete"`。
- 排序：每次打开筛选面板都匹配到内置“稀有度”标签，`observedScope.sort="verified"`、`sortLabel="稀有度"`。该探针只验证标签，不读取升降序方向；`complete` 不声称方向已验证。
- 覆盖账目：六位置全部 `complete:true`、`topVerified:true`、`bottomVerified:true`。

| 位置 | 视口 | 访问 | 顶部 | 底部 | 完整 |
| --- | ---: | ---: | --- | --- | --- |
| 1 | 6 | 97 | ✓ | ✓ | ✓ |
| 2 | 7 | 114 | ✓ | ✓ | ✓ |
| 3 | 7 | 104 | ✓ | ✓ | ✓ |
| 4 | 14 | 225 | ✓ | ✓ | ✓ |
| 5 | 16 | 249 | ✓ | ✓ | ✓ |
| 6 | 14 | 216 | ✓ | ✓ | ✓ |

同日更早一次运行在位置 1 第 3 视口、48 次访问后因 Lucia 抢占前台报 `focusLost`；`observedScope` 保持 `partial`，位置 1 为 `complete:false`、`bottomVerified:false`，未误报完整覆盖。

本次复跑只验证排序与覆盖账目，不替代 §4.1 的字段、装备者、同详情、Lost、管理与导出验收。

### 4.3 2026-10-20 macOS 原生体验验收（空闲态）

- 构建：`main` `cc3aed6` 的开发版 `Youkai Dev`，`CGO_ENABLED=1 go tool mygo dev`；数据根 `~/Library/Application Support/Youkai Dev/`。
- 环境：Apple Silicon Mac；窗口 1020×700；系统处于空闲态，没有活动扫描，游戏未运行。
- 托盘：菜单栏状态项的辅助描述为 `Youkai · 就绪`；菜单项为状态（禁用）、打开主窗口（可用）、停止扫描（禁用）、退出（可用）。
- 关窗隐藏：`⌘W` 后进程仍存活，主窗口 `onScreen=false`；点击托盘“打开主窗口”后窗口恢复、同一进程。
- 第二实例：窗口隐藏时 `open -n "Youkai Dev.app"`；仍只有一个 `Youkai Dev` 进程，主窗口恢复，日志为 `second instance handed over 0 argument(s); restoring the main window`。
- 深链接：窗口隐藏时 `open youkai://open` 恢复同一进程的主窗口；`open 'youkai://unknown?secret=should_not_log'` 保持窗口隐藏，日志只记录 `youkai://unknown`，未记录 query。
- 明确退出：`⌘Q` 与托盘“退出”均结束进程，日志出现 `exit wrap-up complete`。
- 关闭即退出：勾选“关闭窗口时停止扫描并退出应用”后，`settings.json` 写入 `{"closeBehavior":"exit"}`；`⌘W` 触发收尾并结束进程。测试后已恢复 `{"closeBehavior":"hide"}` 并重启，启动日志为 `close behavior=hide`。
- 导出：从历史记录 `5387f071` 打开，导出 v2 JSON/CSV；JSON 1005 件、`excludedCount=0`、`coverage=complete`、revision 128；CSV 1005 行 + 表头，序号集合一致，UTF-8 BOM。文件：
  - `~/Downloads/youkai-2026-10-20-5387f071.youkai.json`，1,196,422 B，SHA-256 `4fa08e574906b9d7772cffa85044816eb0134b3626088c7d4a27a295ab44726c`。
  - `~/Downloads/youkai-2026-10-20-5387f071.youkai.csv`，213,622 B，SHA-256 `918e4e8750419370e723c586e4f0445ece81c9ae4c9e01f1282b51a8175a6285`。
- 文件对话框取消：取消后 UI 显示“已取消导出”，目标文件 mtime/size 不变。
- 同名覆盖：再次保存同一文件名出现 `already exists. Do you want to replace it?`，选择 Replace 后写入成功；JSON 因 `exportedAt` 更新产生新摘要。
- 无权限：将 `/tmp/youkai-readonly.*` 设为只读并在保存面板中定位到该目录；保存后 UI 显示 `导出失败：写入 … permission denied`，目录未留下 `.tmp` 文件，记录未被破坏。
- 中文输入法：在保存面板文件名输入框用当前拼音输入法输入 `ceshi` 并提交，字段值变为“测试”，随后取消。该证据覆盖保存对话框文本输入；记录字段当前只有数字输入，未单独做中文编辑。

未测：扫描中的托盘“停止扫描”、扫描中第二实例/深链接的“先停止输入”行为、磁盘不足、Windows 原生行为。这些不能由本轮空闲态结果推出通过。

### 4.4 空白记录模板

每次现场执行复制下表填写；没有证据的行保持“待执行”。

| 项目 | 结果（通过/失败/待执行/未触发） | 证据路径或日志锚点 | 备注 |
| --- | --- | --- | --- |
| 六位置 1–6 转盘与准备 | 待执行 |  | 逐位置一行 |
| 独立 Clear（`YOUKAI_INDEPENDENT_CLEAR=1`） | 待执行 |  | 记录漏斗 clear 检查点 |
| 顶部滑块确认 | 待执行 |  | 记录 `top confirmation accepted` |
| Lost 预算与安全停止 | 待执行 |  | 未触发时写“未触发” |
| 相邻同详情双盘 | 待执行 |  | 无样本时写“未取得样本” |
| 管理状态三态 | 待执行 |  | 抽查件数与 `null` 情况 |
| 装备与装备者 | 待执行 |  | 当前/他人/未装备 |
| 覆盖结论 | 待执行 |  | 排序未验证时只能是 partial/unknown |
| 待校对计数与连续校对 | 待执行 |  | 记录首末序号 |
| JSON/CSV 导出与同修订 | 待执行 |  | 文件 SHA-256 |
| Windows 轮 ② | 待执行 |  | 安装包与实机信息 |
| macOS 托盘/深链接/输入法/对话框 | 待执行 |  | 系统版本与操作步骤 |

## 5. 失败与证据处理

- 任何检查未执行、证据不足或无法复现，都记“待执行/未验证”，不能写成通过。
- 失败时先停止扫描或验收操作，保留当前记录、日志、导出文件与画面截图；不要删除记录或截图，不要手工改检查点。
- 独立 Clear 或 S 生效失败（`navigationMismatch`）、详情归属失败（`detailUnattributed`）、位置丢失（`positionLost`）、证据失败（`evidenceFailed`）都按安全停止处理；已接纳结果保留为部分结果。
- Lost 到预算仍不停稳时停止，不降低阈值、不反向乱滚寻找“看起来相同”的物品。
- 覆盖结论只能按 T5 §5 的条件升级；排序标签未验证、末端证据不足或计数冲突时保留 `partial`/`unknown`，字段修正不能改变覆盖结论。
- 导出与界面不一致时，以同一已保存修订的 JSON/CSV 和会话记录为证据，记录版本、会话 ID、导出时间与文件摘要；不要用新扫描覆盖旧证据。
- 需要复现时使用同一构建、系统、客户端、显示配置与仓库起点；换条件后的结果单独记录。

## 6. 明确不能由本文推出的结论

- 不能推出真实六位置字段验收通过：`5387f071` 轮覆盖 `complete` 且排序标签已验证（仅标签，不含升降序方向），但 116 件已装备未识别装备者、14 件装备状态不一致、同详情保护、Lost 与 `none` 样本仍缺证据；库存跨轮可变，不能用旧轮数量作基线。
- 不能推出独立 Clear、顶部滑块、Lost、相邻同详情或管理/装备字段通过：`5d3200e3` 轮只在日志层确认了独立 Clear 与顶部确认；滚动 Lost 未触发，相邻同详情只有数据层样本，管理 `none` 未取得样本，装备身份与 14 件装备状态未通过。
- 不能推出 Windows M3 通过：实测轮 ② 未执行。
- 不能推出可用导出代表完整库存：875 件可用数据全部为未装备，所有已装备项因 `agentId=null` 被排除。
- 不能推出扫描质量、OCR 正确率或库存无漏扫：现场轮只证明流程终止与字段落盘，没有逐件人工真值对照；1005 与 995 的差异也未归因。
- 不能推出 10,000 行 UI 压力通过：10,000 行只覆盖数据层。
- 不能推出扫描中托盘“停止扫描”、扫描中第二实例/深链接的停止行为或磁盘不足通过：本轮原生验收只在空闲态执行；这些路径仍只有自动化或代码证据。
