# M3 规模测试与正式验收清单

更新日期：2026-10-20。状态：合成数据与自动化规模检查已完成；真实六位置字段验收、Windows 实测轮 ② 与 macOS 原生体验项均**尚未执行**。本文把“已自动化覆盖”“已用合成数据覆盖”和“必须真机执行”分开记录，未执行的项目不是通过。

- 被测基线：`main` `6e8e8f0`（窗口生命周期已由 PR #48 合并）。
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
| macOS 六位置完整字段验收 | **待用户现场执行** | 见 §3.1。独立 Clear、顶部滑块、Lost、相邻同详情、管理/装备、覆盖均未执行。 |
| Windows 实测轮 ② | **待用户现场执行** | DPI、GDI、SendInput、托盘、安装包与真实客户端均未验证。 |
| macOS 托盘/深链接/输入法/文件对话框 | **待用户现场执行** | 自动化只覆盖替身；系统原生行为未验证。 |

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

以下项目全部标记为**待用户现场执行**。只有实际执行并留下本节要求的证据，才能在交付记录中改为通过。

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
| 管理状态与装备 | 逐位置抽查锁定、弃置、无标记；已装备、未装备、他人装备。列表盘格右下角角标与顶部“拆解”左侧指示一致；详情等级右侧头像与列表格右上角头像一致。冲突或不可读必须为 `null`，不能落成 `none`/未装备。 | 任一冲突或不可读：记录 `null` 与实际画面；不得改写成合法状态。 |
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

## 4. 正式验收执行记录模板

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

- 不能推出真实六位置字段验收通过：本文未执行任何真实游戏位置。
- 不能推出独立 Clear、顶部滑块、Lost、相邻同详情、管理/装备或覆盖结论通过。
- 不能推出 Windows M3 通过：实测轮 ② 未执行。
- 不能推出扫描质量、OCR 正确率或库存无漏扫：合成数据不包含真实截图与识别结果。
- 不能推出 10,000 行 UI 压力通过：10,000 行只覆盖数据层。
- 不能推出真实文件对话框、输入法、托盘或深链接体验通过：自动化只覆盖替身与接线。
