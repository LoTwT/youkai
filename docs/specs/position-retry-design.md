# 按位置重试与续扫（草案）

更新日期：2026-10-27。

> 状态：草案，未经用户确认，不代表已采纳。任何实现、合同升版或行为变更都需要用户另行决定。
>
> 基线：`main@fd005f9b15861579b6be5243d16305272baec4fe`（本工作树）。本文只讨论 ZZZ 驱动盘仓库六位置遍历的故障恢复；推荐方案不改变字段识别或结果校对，导出格式的版本影响在第 5 节单独讨论。

阅读标记：

- **[事实]**：在基线的代码或文档中直接读到；给出文件与行号或文档链接。
- **[用户输入]**：本次任务提供的真机实例；仓库内没有找到对应的原始日志或报告，未独立复核。
- **[推断]**：基于事实的设计判断或估算；需要实现、脚本化测试或实机验收才能成立。

相关契约只在本文引用，不复制正文：[架构第 5 节](scanner-architecture.md#5-扫描会话与结果语义)、[T5 §4](t5-zzz-inventory-traversal.md#4-一件物品何时可以进入识别队列)、[T5 §5](t5-zzz-inventory-traversal.md#5-滚动衔接重复物品与末尾)、[T5 §6](t5-zzz-inventory-traversal.md#6-保存什么证据怎样验收)、[JSON/CSV v2](scan-json-format.md)、[T6](t6-review-export-workflow.md)。

## 1. 现状与问题

### 1.1 当前遍历是整次会话的单条控制流

**[事实]** `Inventory` 一个实例服务一次会话，`Collect` 在生产者 goroutine 上调用 `run`；`run` 固定按 `for pos := 1; pos <= 6; pos++` 推进，每个位置先 `preparePosition`，再 `collectPosition`。任一准备或遍历错误都会记录当前位置的不完整账目并直接返回，后续位置不再尝试。[inventory.go](../../internal/game/zzz/collect/inventory.go#L594) [inventory.go](../../internal/game/zzz/collect/inventory.go#L678) [inventory.go](../../internal/game/zzz/collect/inventory.go#L730)

**[事实]** 每个位置开始时，`preparePosition` 重置 `viewport`、`visited`、`selectionTracked`、`allowInitial`、`positionVisits`、`listEstablished`、`topVerified`、`lastScrollRows`、`lastScrollEvidence`、`lastDetailFrame` 和耗时累计；位置之间没有继承导航状态。[inventory.go](../../internal/game/zzz/collect/inventory.go#L681)

**[事实]** 位置级证据已经存在：`positionScope` 记录 `position`、`viewports`、`visits`、`topVerified`、`bottomVerified`、`complete`；`recordPosition` 追加一条位置账目；`updateCoverage` 只有六个位置全部 `complete`、顺序为 1–6 且排序已验证时才给出 `complete`，否则为 `partial` 或 `unknown`。[inventory.go](../../internal/game/zzz/collect/inventory.go#L277) [inventory.go](../../internal/game/zzz/collect/inventory.go#L295) [inventory.go](../../internal/game/zzz/collect/inventory.go#L432)

**[事实]** `positionLost` 有两个来源：滚动无法建立唯一位置对应（含未停稳、无对应、`applyTransition` 校验失败），以及点击前的选中格守卫发现选中框没有回到预期位置。前者见 [inventory.go](../../internal/game/zzz/collect/inventory.go#L2506)，后者见 [inventory.go](../../internal/game/zzz/collect/inventory.go#L1615)；两者共用 `CodePositionLost`。[traversal.go](../../internal/game/zzz/collect/traversal.go#L63)

**[事实]** `navigationMismatch` 覆盖的是另一类问题：页面或列表不再可验证、转盘未切到目标位置、筛选未 `Active`、滚动未产生位移或结果不可判定等。它和 `positionLost` 共用停止路径，但语义不同。[inventory.go](../../internal/game/zzz/collect/inventory.go#L743) [inventory.go](../../internal/game/zzz/collect/inventory.go#L780) [inventory.go](../../internal/game/zzz/collect/inventory.go#L2499)

**[事实]** 接纳序号由内核在 `Slot.Admit` 时按会话单调分配；未接纳的尝试不消耗序号。`visitIndex` 只对 S 级接纳递增，非 S 跳过不消耗。`capturedCount` 等于已接纳观测数（含后来失败或未处理项）。[handle.go](../../internal/scan/handle.go#L78) [types.go](../../internal/scan/types.go#L16) [inventory.go](../../internal/game/zzz/collect/inventory.go#L2183) [checkpoint.go](../../internal/record/checkpoint.go#L243)

**[事实]** `observedScope` 在预检成功后先上报一次；`Controller.Collect` 只在 `trav.Collect` 返回后再次上报，遍历过程中不会把每个位置的账目持续送到记录层。[controller.go](../../internal/game/zzz/collect/controller.go#L245) [controller.go](../../internal/game/zzz/collect/controller.go#L262)

**[事实]** 保存时 `capturedCount` 必须等于条目记录数；恢复时排队/运行条目转为中断未处理，且存储的 `complete` 覆盖会被降为 `partial`。应用首次启动扫描总是新建记录；历史记录只能重开校对，不能继续扫描，不存在“从上次位置继续”的入口。[deliverer.go](../../internal/app/deliverer.go#L391) [checkpoint.go](../../internal/record/checkpoint.go#L330) [load.go](../../internal/record/load.go#L21) [service.go](../../internal/app/service.go#L239)

### 1.2 两个真机实例

| 实例 | 现场结果 | 来源 |
| --- | --- | --- |
| 会话 `e3bf895b-73d8-4dde-8350-22d1b4f67215` | 位置 1–5 完成，位置 6 的视口 5、目标 slot 18（row 4, column 2）停止；886 captured、885 produced、1 unprocessed，终止 `failed / positionLost / capturing`，覆盖 `partial`。日志显示上一件（seq 886）的选中框在 10 帧内从 slot 17 变为 `-1`；守卫在点击 slot 18 前停止。 | [事实] [2026-10-26 macOS 真机验收报告](../reports/2026-10-26-macos-field-acceptance.md#选中格守卫的现场复现) |
| 会话 `16ef968e` | 位置 1 的最后 3 行短步因误差 11.20 略超 10.0 上限而 `positionLost` 停止。 | [用户输入] 本次任务提供；仓库内未找到该会话的报告或日志，未独立复核 |

**版本说明（`e3bf895b`）：** 该次停止早于 `awaitSelectionReturned` 的有界等待修复；报告同节记录修复是在这次现场之后加入的，修复后的真机复测尚未执行。[2026-10-26 报告](../reports/2026-10-26-macos-field-acceptance.md#选中格守卫的现场复现) 因此这个实例既不能证明当前代码仍会在同一处停止，也不能证明位置重试一定能跨过它；见 4.6。

**[推断]** 这两个实例的共同点是：故障发生在某个位置内部，前面的位置或前面的视口已经取得独立证据；它们不是范围、几何或权限层面的失效。因此“按位置重试”比“整轮重扫”更接近故障的实际单位。全量重扫约 18 分钟是用户提供的量级；T5 中“每位置约 1 分钟、全程约 6 分钟”是早期推算值，不能直接当作真机耗时。[T5 §5](t5-zzz-inventory-traversal.md#5-滚动衔接重复物品与末尾)

**[推断]** 位置重试的代价主要取决于该位置已走了多远。若故障在最后几个视口，重走整个位置仍可能需要约 1–3 分钟；若采用“回到顶部但跳过已解决格位”，代价主要落在滚动而非点击和识别。两种做法的取舍见第 3 节。

### 1.3 现有契约对恢复的限制

**[事实]** 架构第 5 节规定：停止是单向的，重新开始必须完成上次收尾并重新预检，首版不承诺暂停续扫；正常结束或故障后每个采集序号恰好得到一个结局；封存后不再接纳迟到写入；检查点只恢复最近可验证代次，异常后标记中断且不能升级覆盖。[架构第 5 节](scanner-architecture.md#5-扫描会话与结果语义) [检查点与恢复](scanner-architecture.md#保存校对与导出)

**[事实]** T5 规定：只有选中、详情、稳定和归属证据同时成立才接纳；滚动只按唯一、完整、一一对应的位置对应推进；顶部与底部必须各自取得独立证据；六位置账目是覆盖结论的依据；排序标签未验证时不能声明 `complete`。[T5 §4](t5-zzz-inventory-traversal.md#4-一件物品何时可以进入识别队列) [T5 §5](t5-zzz-inventory-traversal.md#5-滚动衔接重复物品与末尾) [T5 §6](t5-zzz-inventory-traversal.md#6-保存什么证据怎样验收)

**[事实]** scan JSON v2 用 `session.id + items[].sequence` 标识一次观测；`capturedCount` 是已接纳的 S 级观测数（含后来失败或未处理项）；`excludedCount = session.capturedCount - items.length`；`coverage` 只是 `complete / partial / unknown` 加可选 `expectedCount`；v2 拒绝未定义字段，新增会改变读取契约的字段必须升版。[JSON v2 §2](scan-json-format.md#2-公共外壳) [JSON v2 标识](scan-json-format.md#标识与目录子集) [JSON v2 校验](scan-json-format.md#6-示例与校验边界)

**[事实]** T6 规定新扫描建立新会话，历史重开只读取已保存结果，导出取单次已保存修订；不提供 archive 视图、ZIP 或档案导入导出。[T6 §1](t6-review-export-workflow.md#1-最小界面与完整使用流程) [T6 §3](t6-review-export-workflow.md#3-保存重新查看与导出建议)

**结论：** 本设计不能把“重试”实现成一次新的停止再启动，也不能在重试中放宽任何接纳、滚动或覆盖证据；它只能在同一会话的 `Collect` 内，把“某个位置的失败”与“整次会话的失败”分开处理。是否需要跨会话续扫，是第 3 节的另一个方案。

## 2. 目标与非目标

**目标**

1. 位置级一次性故障不再导致整次扫描结束，尤其不再需要重扫已经完成的位置。
2. 重试不产生重复的可用物品，不静默漏掉格位，不重编号，不改变 `capturedCount` 的定义。
3. 覆盖结论仍只由已验证的顶部、底部、位置账目和排序证据推出；失败或无法确认时保持 `partial`/`unknown`。
4. 已接纳的结果、证据和原始处理结局不因重试被删除或改写。
5. 用户能看见发生过重试，而不是把失败静默吞掉。

**非目标**

- 不实现跨进程暂停/恢复，也不改变架构第 5 节“停止后重新预检”的约束。
- 不合并任意两次独立扫描，不引入 archive 或跨会话导入导出。
- 不放宽滚动阈值、顶部/底部判据或详情归属阈值。
- 不自动重试焦点、几何、权限、筛选/排序、存储、识别运行库等会话级故障。
- 不把 OCR 失败、字段待校对或未处理项当作覆盖失败。

## 3. 候选方案

### 3.1 方案 A：会话内对失败位置重试

A1 与 A2 的共同点：重试仍发生在同一次 `Collect` 内，`session.id`、序号分配、检查点和导出都还是同一次会话；只把“当前位置”重新准备一次，不重启平台会话。A3 是现有动作级重试的层次，不是位置级变体。

#### A1：整位置重走，旧尝试标记为被取代

- 失败后重新执行该位置的完整准备（转盘、筛选、回顶），从顶部重新访问所有格位。
- 第一次尝试已接纳的条目保留在记录中，但标记为“被后续尝试取代”；可用导出只取最新一次尝试。
- 新尝试的接纳继续使用新的会话序号；`capturedCount` 仍按“所有已接纳观测”计数，因此旧尝试会进入 `excludedCount`。
- 覆盖只认最新尝试；最新尝试完成时位置可 `complete`。

#### A2：回到顶部续走，继承已解决的绝对格位（推荐）

- 失败后重新执行该位置的完整准备，从顶部重新走列表。
- 第一次尝试中已经接纳 S 或确认非 S 跳过的**绝对格位**（位置 + 绝对行 + 列）被继承为“已解决”；重走时这些格位只参与滚动与位置对应，不再点击、不再接纳。
- 新尝试只处理此前未解决的格位；`seq`、`visitIndex`、`capturedCount`、`excludedCount` 都不增加重复项。
- 覆盖取两次尝试的并集：最终尝试提供顶部/底部证据，已解决的绝对格位由第一次尝试提供覆盖依据。

#### A3：只重试失败的动作

现有代码已经在动作层做了有界重试：无效点击在预算内重发，滚动未位移时重试拖拽，选中框瞬时消失时在 action budget 内等待恢复。[inventory.go](../../internal/game/zzz/collect/inventory.go#L1559) [inventory.go](../../internal/game/zzz/collect/inventory.go#L2444) [inventory.go](../../internal/game/zzz/collect/inventory.go#L1615) 这一层应当保留，但它解决不了“已经无法建立位置对应”或“选中框持续不回”的情况；位置级重试是它的外层，不应在位置层再重复同一动作的重发。

### 3.2 方案 B：新会话从指定位置开始，导出时合并

- 开始扫描时允许选择起始位置（1–6），新会话只扫描该位置及之后的位置。**注意：** 现有 `observedScope.startPosition` 只是准备前观测到的转盘位置，不是扫描起点；外层循环固定从 1 开始，因此 B 需要新的起点参数和循环范围。[inventory.go](../../internal/game/zzz/collect/inventory.go#L594) [2026-10-26 报告](../reports/2026-10-26-macos-field-acceptance.md#选中格守卫的现场复现)
- 每次扫描仍是独立记录；用户需要在新会话结束后把多次扫描的结果合并导出。
- B1：不合并，分别导出多个文件。每个文件的 `coverage` 只覆盖自己的位置，不能单独声称整仓 `complete`。
- B2：导出时合并为一份文件。需要新的组合标识、跨文件序号或来源引用、合并后的 `capturedCount`/`excludedCount` 语义、证据归属和清理规则。

### 3.3 方案 C：同一记录内多段采集

介于 A 和 B 之间：不新建记录，而是在一次记录内允许第二次 `Collect`（新的一段），序号继续递增，覆盖账目合并。它避免公共格式合并，但需要重新打开已封存记录、重新获取平台会话、重新预检，并让保存/校对/清理支持“多段记录”。这实际上是把“暂停续扫”做成记录能力，超出本次最小变更。

### 3.4 比较

| 方案 | 重试/续扫单位 | 已接纳条目 | 序号与计数 | 覆盖与合同 | 主要代价 | 结论 |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | 当前位置整段重走 | 保留，标记被取代；导出只取最新尝试 | 新序号继续分配；`capturedCount` 含两次尝试，`excludedCount` 增大 | 最新尝试完成才 `complete`；内部记录需加尝试/取代字段 | 重复点击、OCR、证据和磁盘；旧尝试的部分成果不进入可用导出 | 可作为 A2 的退路；不作为首选 |
| A2 | 当前位置回到顶部续走 | 保留且不重复接纳；绝对格位继承 | 序号、`capturedCount`、`excludedCount` 均不因重试增加 | 两次尝试的并集；内部 `observedScope` 增加尝试账目；公开 v2 形状不变；跳过前必须逐行像素核验 | 需要保留行图并逐行核对；依赖排序标签一致与列表顺序不变，方向不可见 | **推荐，但以逐行核验和排序一致为前提；否则退到 A1** |
| B1 | 新会话，从位置 P 开始 | 各记录独立保留 | 各记录独立编号与计数 | 每个文件只能 `partial`；无法跨文件声称 `complete` | 用户要管理多个文件；整仓结论缺失 | 只适合临时救急，不解决目标 |
| B2 | 新会话 + 导出合并 | 各记录独立保留 | 需要全局序号或来源引用；合并会改变计数语义 | 需要 scan JSON v3、记录组、证据与清理规则 | 合同、界面、历史、清理和身份全面改动 | 不作为首版；会话级故障再评估 |
| C | 同一记录多段采集 | 保留在一条记录 | 序号继续递增 | 覆盖账目合并；内部记录多段 | 重新打开已封存记录、多段生命周期和保存语义 | 可作后续方向，成本高于 A2 |

### 3.5 推荐 A2 的理由

**[推断]**

1. 它直接利用现有“位置 × 列表”状态机已经在维护的格位账目：`visited`、`applyTransition` 和 `positionScope` 本来就把格位当作遍历单位。[inventory.go](../../internal/game/zzz/collect/inventory.go#L500) [inventory.go](../../internal/game/zzz/collect/inventory.go#L2799)
2. 它不制造重复观测。A1 需要用“被取代”概念解释两份同一件盘的记录；A2 在重走时直接跳过已解决格位，`capturedCount` 与可用导出的语义完全不变。
3. 它不需要公共合同升版。重试账目只进入不透明的 `observedScope` 和捕获证据；JSON v2 的 `session`、`items`、`coverage`、`capturedCount`、`excludedCount` 都不改形状。
4. 它的代价与故障位置相关：只需要滚动经过已解决的行，不必重新点击和重新识别。对 `e3bf895b` 这类后段失败，前提是选中格消失只是瞬时动画且修复后的复测能跨过；对 `16ef968e` 这类短步失败，前提是能通过重新标定或独立证据解决，而不是单纯延长预算。否则重试仍会安全失败（见 4.5、4.6）。
5. 它保留了“位置是独立证据单位”的现有设计，不把六个位置变成跨文件、跨会话的合并问题。

**A2 的明确前提（评审后收紧）：**

- **排序：** 扫描期间用户不改变排序、筛选或库存状态，这是 [T5 §3](t5-zzz-inventory-traversal.md#3-开始前必须核对的页面条件) 已经要求的既有前提。重试时面板排序标签必须仍为已验证状态，且与第一次尝试的同一标签一致；否则不进入跳过路径：标签未验证时停止或退 A1（覆盖只能 partial）；读到与第一次不同的标签时，按 T5 §3 视为可观察变化，必须停止，A1 也不能继续把该位置当作同一范围。T5 §3 还要求运行中观察到筛选、排序、库存数量或布局变化时停止并撤销整仓结论。T5 的探针不读取升降序方向，方向翻转仍是不可见的残余风险（见第 8 节）。
- **列表：** 不能只用 `ListEvidence.Occupied` 证明“同一列表、同一顺序”。S 筛选后列表紧密排列，已覆盖行几乎全是占用；换位、排序方向翻转和置顶盘变化都可能不改变占用模式。A2 必须为每个已覆盖绝对行保留遮罩后的行图，重走时逐行做灰度核验；任何一行不匹配就停止，不跳过（见 4.2）。如果第一次尝试中存在重复行图，行图不能证明实例身份，这些行也不得跳过。
- **去重边界：** 行图核验只证明“同一绝对格位现在是否仍显示第一次尝试的同一视觉内容”，用于决定能否继承已有接纳；它不是按内容判断两件盘相同，也不用于合并、删除或跨格位匹配观测。
- **退路：** 如果上述排序前提或逐行核验不被接受，A2 不成立；应退到 A1（全量重走、旧尝试标记取代）或直接停止，而不是执行无核验的跳过。

**评审后的推荐边界：** A2 仍是首选，但初稿把它描述成“只靠占用位和排序标签”的轻量方案是不成立的。逐行像素核验、排序标签一致和方向不可见风险接受，是 A2 的三个前提；任一项不能满足时，推荐改为 A1，而不是降低证据标准。若目标库存中存在大量行图相同的行，或无法在重走时证明行图能区分这些行，A2 的跳过条件经常不成立，首版应直接采用 A1，把 A2 留作后续优化。

## 4. 推荐方案 A2 的详细设计

### 4.1 哪些故障可以触发位置级重试

**[推断]** 下表的故障码来自基线代码，但“是否重试”和条件是本设计判断；实现时必须用结构化分类而不是错误文本。

| 故障码/来源 | 含义 | 位置级自动重试 | 条件与动作 |
| --- | --- | --- | --- |
| `positionLost`（滚动） | 列表移动但无法建立唯一对应、未停稳、无对应，或 `applyTransition` 校验失败 | 是，最多一次 | 页面仍是仓库、同一转盘位置、漏斗 `Active`、几何未变、输入许可仍开；排序标签必须已验证且与第一次尝试一致，否则不跳过（未验证：停止或 A1，覆盖 partial；不同：停止）。重走时逐行做遮罩行图核验 |
| `positionLost`（选中格守卫） | 点击前选中框没有回到上一件，且 action budget 内持续缺失或移到别格 | 是，最多一次 | 先沿用现有的有界等待；仍失败后重新准备该位置并从顶部续走；A2 的排序标签一致与逐行核验前提同样适用；若消失是点击末行触发的客户端自动滚动，重试可能再次触发（见 4.6） |
| `navigationMismatch` | 页面/列表不可验证、转盘未切换、筛选非 `Active`、滚动无位移/不可判定、列表已不在目标位置 | 否 | 现有动作级重试已经覆盖“点击/拖拽没生效”；剩下的属于范围、页面或筛选证据失效，原地重试只会重复同一失败。停止并保留已接纳结果 |
| `focusLost` | 目标窗口失焦 | 否 | 输入许可已关闭；按架构第 5 节收尾，用户重新开始新会话 |
| `targetHidden`、`targetAmbiguous`、`targetLost` | 遮挡、身份歧义或目标丢失 | 否 | 页面前提失效；停止并保留已接纳结果 |
| `geometryChanged`、`geometryInvalid` | 几何或缩放变化 | 否 | 坐标证据失效；停止并重新预检，不重试当前位置 |
| `permissionRevoked` | 采集或输入权限撤回 | 否 | 平台前提失效；停止 |
| `captureStopped`、`inputBlocked`、`inputFailed`、`observationFailed` | 采集流、输入或观测失败 | 否 | 会话级故障；停止并保留已接纳结果 |
| `detailUnattributed` | 详情无法归属到目标；已有参照往返也未能解决 | 否 | 位置级重走会重复同一条归属证据；保留未解决位置，停止。若将来要重试，应先单独设计“同格重读”而不是整位置重走 |
| `detailUnresolved` | 详情可归属但品质或副词条行数不可判定 | 否 | 同上；属于识别/布局证据缺口，不是导航一次性故障 |
| `evidenceFailed`、`storageFailure` | 投影、入队、证据写入或检查点保存失败 | 否 | 证据/存储链路已经不可信；重试可能制造不一致或重复写入。停止并保留已接纳结果 |
| `prepareTimeout`、`pageUnverified`、`anchorsMissing` | 准备阶段或预检不成立 | 否 | 还没有位置账目；用户修正页面、窗口或几何后重新开始 |
| `controllerFailed` | 控制器返回了没有诊断码的错误 | 否 | 无法判断是位置级瞬态；按会话故障停止 |
| `userCancelled`、`interrupted`、`runtimeFailure`、`releaseFailed` | 用户停止、崩溃恢复、识别运行库或释放失败 | 否 | 不是位置级瞬态故障 |

**实现注意：** 不要按错误消息文本判断是否可重试。建议在 `collect` 内引入一个明确的“位置级可重试”分类（例如包装 `scan.FaultError` 的结构化标记），由产生故障的分支自己标注；`positionLost` 的两个来源都标为可重试，`navigationMismatch` 默认不可重试。这样不会因为后续文案变化误重试。[推断] `navigationMismatch` 目前是一个较宽的代码；如果后续实测表明某个瞬态分支（例如 `currentList` 的短时不可判定）是主要的一次性故障，应先拆分诊断码并取得证据，再决定是否纳入重试，而不是直接按字符串放行。

### 4.2 重试流程

1. **在位置边界接住错误。** `run` 在 `collectPosition` 返回错误后判断分类；不可重试或预算用尽时，记录不完整位置并返回。可重试时先记录第一次尝试的失败，不把它当作会话终止原因。
2. **先区分滚动失败是“未停稳”还是“停稳但无法对应”。** **[事实]** `awaitScroll` 已经会在 Lost 后继续观察到 `ScrollBudget` 结束，并对每个新帧重新与拖动前帧比较；`unsettled=true` 只在已经看到 candidate、但相对 candidate 未停稳时出现，`unsettled=false` 表示到预算结束仍没有 candidate（可能是停稳但无法建立对应）。[inventory.go](../../internal/game/zzz/collect/inventory.go#L2628) 对 `unsettled=false` 再等一个预算只是把等待时间加倍，不能据此假设会恢复；只有 `unsettled=true` 或新证据表明列表仍在移动时，才值得延长等待。底部短步还需要独立证据（例如滑块位移交叉验证）或重新标定；不能只因为滑块在底部就接受一个 `Lost`。
3. **重新准备同一个位置。** 保留该位置已解决的绝对格位集合，重置本尝试的 `viewport`、`visited`、`selectionTracked`、`allowInitial`、`lastScrollRows`、`lastScrollEvidence`、`lastDetailFrame` 和耗时累计；如果第一次尝试已有已解决格位，重试开始时将 `selectionTracked` 设为 true，避免重试后的首次点击绕过选中格守卫。然后依次 `establishList`、`switchWheel(pos)`、`resetFilters`（一次面板访问：打开→重置→勾选 S→关闭→要求漏斗 `Active`，并记录本次排序标签）、`scrollToTop` 并取得稳定的顶部确认。转盘、漏斗、几何或输入许可不成立时中止重试，按该步的原始故障码停止；排序标签必须已验证且与第一次尝试同一标签，否则不进入跳过路径（未验证：停止或 A1，覆盖 partial；不同：停止）。
4. **从顶部续走。** 每个新视口进入时，把已解决绝对格位标成 `visited`；`nextSlot` 只返回未解决且占用的格位。已解决格位不发点击，只允许滚动和列表证据观察。
5. **逐行核对列表没有改变（不是只核对 Occupied）。** **[事实]** `nextSlot` 只根据 `visited` 与 `ListEvidence.Occupied` 选择下一个格位；S 筛选后列表紧密排列，已覆盖行几乎全为“占用”，占用模式对换位、排序方向翻转和当前装备置顶变化几乎没有区分力。[inventory.go](../../internal/game/zzz/collect/inventory.go#L1019) 第一次尝试必须为每个已覆盖绝对行保留一份遮罩后的行图（或视口帧加偏移）；重走时对每个要跳过的行做与 `MatchScroll` 同类的灰度比较，遮掉选中框（`Border`/`Bar`）和锁定/装备角标（`Management`/`Avatar`）区域；任一行超过标定阈值即停止，不跳过。占用位只作前置快速检查。行图核验只证明同一绝对格位的内容未变，不是按内容去重（见 4.3）；如果第一次尝试中有两行遮罩行图相同，行图无法在重排后区分它们，这些行不得按绝对行继承，必须停止或退到 A1。
6. **继续接纳。** 新格位的点击、详情归属、S 判定、`seq`、`visitIndex`、证据与第一次尝试完全相同；不允许因为“这是重试”而降低稳定帧、详情归属或 S 徽章的要求，也不使用 `initialVerified`（见 4.3 的旧详情风险）。
7. **结束该位置。** 取得底部独立证据后，把该位置记为完成；覆盖取两次尝试的并集。若重试再次失败，记录第二次尝试的不完整账目，停止会话（默认，见开放问题 3）。
8. **记录与上报。** 每次尝试结束都更新 `observedScope` 的位置账目；至少在本位置第一次失败后、开始重试前，把账目送到记录层，避免重试过程中崩溃丢失“曾经失败”的事实。

**绝对行状态（实现说明）：** 当前 `inventoryRun` 只有 `viewport` 计数，没有绝对行偏移。[inventory.go](../../internal/game/zzz/collect/inventory.go#L500) A2 需要新增 `rowBase`：每次该位置的顶部确认后置 0；只有 `applyTransition` 完成全部校验并提交 `Rows` 后，才执行 `rowBase += Rows`。也就是说，`rowBase` 是从顶部起所有已提交滚动的 `Rows` 累计值，绝对行 = `rowBase + row`（`row` 为当前视口内行）。一次 `Lost`、`unsettled`、ambiguous 或被 `applyTransition` 拒绝的滚动都不更新 `rowBase`，也不把 after 帧映射成新的绝对行；因此失败前最后一个已映射视口仍是 `rowBase..rowBase+Rows-1`，重试从顶部重新开始，只继承这些已映射行。不能把 `viewport` 计数当作绝对行。

### 4.3 已接纳条目如何处理

**A2 的选择是“保留、继承、不重复接纳”。**

- 第一次尝试已接纳的 S 级观测保留其 `sequence`、`visitIndex`、证据、原始处理结局和字段修订；不删除、不重编号、不改成 `superseded`。
- 第一次尝试中确认的非 S 跳过也视为已解决格位；重走时跳过，不重复点击。
- **重试不复用首件的 `initialVerified`。** 失败尝试可能已经在页面上留下选中框和旧详情；如果重试的第一个未解决格位已经带选中框，不能仅凭“详情稳定”就接纳，必须先选一个当前视口内可见且可区分的参照格位，再回到目标，按 T5 §4 的参照往返取得关联证据。否则重试可能把上一件的旧详情记给目标。实现上建议重试开始时禁用 `allowInitial`，并为“目标已选中”增加参照路径而不是直接报 `detailUnattributed`。
- 第一次尝试中已接纳但后来成为 `unprocessed` 的条目（例如 `e3bf895b` 的 seq 886）同样保留并继承格位；重试不会为它再发一次点击。它会以未处理项出现在结果中，`capturedCount` 仍包含它，可用导出会把它计入 `excludedCount`。这是 A2 的已知取舍：不制造重复，但也不自动补采这一件。
- 如果用户希望“只补采未处理或失败的那一件”，那会产生同一物理盘的第二条观测，必须进入 A1 的“被取代/排除”语义；本方案不把这件事混进自动重试。见开放问题 4。
- 禁止用属性哈希、图像哈希或“内容相同”来跨尝试去重；跨尝试的身份来自**已验证的绝对格位**，不是内容。[T5 §5](t5-zzz-inventory-traversal.md#5-滚动衔接重复物品与末尾)
- 行图核验（4.2 第 5 步）只回答“同一绝对格位现在是否仍显示第一次尝试的同一视觉内容”，用于决定能否继承已有接纳；它不回答“两件盘是否相同”，不得用像素相等来合并、删除或跨格位匹配物品。两行行图相同但绝对格位不同时，仍是两件不同的观测；如果第一次尝试里存在重复行图，不能靠行图把它们对应到正确实例，这些行必须停止或退 A1。

### 4.4 序号、`capturedCount` 与位置账目

- `seq` 仍由内核按会话单调分配；重试只对**新解决**的格位调用 `Admit`，已解决格位不消耗序号，不产生重复序号。[handle.go](../../internal/scan/handle.go#L91)
- `visitIndex` 仍只在 S 级接纳时递增；重试跳过的已接纳件保留原 `visitIndex`，新接纳继续递增。[inventory.go](../../internal/game/zzz/collect/inventory.go#L2216)
- `capturedCount` 仍等于条目记录数；A2 不改变这个定义，也不把跳过的格位算成新捕获。[deliverer.go](../../internal/app/deliverer.go#L391)
- `excludedCount` 仍等于 `capturedCount - items.length`；A2 不会因为重试新增被排除项，也不会为同一格位新增第二条条目。此前已接纳的条目仍在 `items` 中，除非它们自身不可用。[export.go](../../internal/app/export.go#L251)
- 位置账目建议改为**每个位置一条**，内部带尝试数组；不能用“每次尝试追加一条 position 记录”的方式，因为现有 `updateCoverage` 要求六条记录顺序为 1–6。[inventory.go](../../internal/game/zzz/collect/inventory.go#L446)

示意（数字仅说明形状，不是实测值；下面只展示位置 6，`coverage: complete` 以其余五个位置也完成为前提）：

```json
{
  "kind": "zzzInventory",
  "page": "warehouse",
  "sort": "verified",
  "sortLabel": "稀有度",
  "coverage": "complete",
  "positions": [
    {
      "position": 6,
      "viewports": 6,
      "visits": 101,
      "topVerified": true,
      "bottomVerified": true,
      "complete": true,
      "recovered": true,
      "attempts": [
        {
          "attempt": 1,
          "sortLabel": "稀有度",
          "viewports": 6,
          "visits": 96,
          "topVerified": true,
          "bottomVerified": false,
          "complete": false,
          "failureCode": "positionLost"
        },
        {
          "attempt": 2,
          "sortLabel": "稀有度",
          "viewports": 6,
          "visits": 5,
          "topVerified": true,
          "bottomVerified": true,
          "complete": true
        }
      ]
    }
  ]
}
```

每次尝试的 `visits` 只计该尝试新解决的格位（S 接纳 + 非 S 跳过）；顶层 `visits` 是各尝试的并集，所以 96 + 5 = 101。每次尝试的 `viewports` 沿用现有 `positionScope` 的 `viewport+1` 口径；示例假设第一次停在视口索引 5，因此是 6，而不是 5。顶层 `viewports` 取最终尝试的值。`attempts` 保留每次尝试的视口、访问、顶部/底部、排序标签和失败码；`recovered` 只表示“曾经失败但最终完成”，不是覆盖结论的替代品。

### 4.5 底部短步：重试不一定能解决 `16ef968e`

**[事实]** 当前 `MatchScroll` 只有在最佳整行位移误差不超过 10、且比零位移至少低 3 时才判 `Displaced`；否则可能判 `Lost`。`Lost` 结果不携带 `Rows`/`Matches`，`applyTransition` 无法据此推进。[pixelmatch.go](../../internal/game/zzz/collect/pixelmatch.go#L480) [maccloud.go](../../internal/game/zzz/collect/maccloud.go#L321) [inventory.go](../../internal/game/zzz/collect/inventory.go#L2799)

**[事实]** `awaitScroll` 不是“只观察一次就判 Lost”：它在 Lost 后继续观察到 `ScrollBudget` 结束，并对每个新帧重新与拖动前帧比较；`unsettled=true` 只在已经出现 candidate、但相对 candidate 未停稳时置位，`unsettled=false` 表示到预算结束仍没有 candidate（可能是停稳但无法建立对应）。[inventory.go](../../internal/game/zzz/collect/inventory.go#L2628) 因此“再观察一个 ScrollBudget”只是把预算加倍，不是新增一种证据。

**[用户输入/评审提供]** 会话 `16ef968e` 的失败日志为：

```text
collect: position 1 scroll 5959 -> 6324 lost: unsettled=false after=6324 rows=0 matches=0
evidence=frames 5959 → 6324: undecided (zero-displacement error 18.58, best shift 441 at error 11.20)
drag_ms=1425 settle_ms=5079
```

**[推断]** 这次滚动已经用满了 5 秒的默认 `ScrollBudget`（`settle_ms=5079`），仍然没有 candidate、`rows=0`、`matches=0`；`unsettled=false`。**在这次证据下，延长预算无效**：没有理由假设同一个 after 帧会在再等一个预算后变成可对应，它只会延长等待。3 行短步的误差 11.20 只比 10.0 略高，可能是列表尚未完全停稳或短步吸附造成的测量偏差，也可能在同一位置的第二次遍历中重复出现。若重复出现，A2 的位置重试会在同一底部再次失败，不能解决该实例。

可选做法（按证据强度排序）：

1. **重新标定或增加独立证据（真正可能改变结论）。** 例如把滑块位移作为独立的交叉验证，或重新标定 `ScrollDisplacedMax` / 短步阈值；任何新规则都必须用新样本单独验收，不能把 10.0 静默改成 12.0，也不能仅凭“滑块在底部”接受 `Lost`。
2. **只在 `unsettled=true` 时考虑延长等待。** 如果列表确实还在移动，现有预算可能不足；这需要新的 `unsettled` 样本和更长的预算标定，而不是默认加倍。
3. **保持现状，重试失败就停止。** 这样不会削弱既有证据，但 `16ef968e` 这类确定性短步仍会留下 `partial`。

本草案不推荐把“再观察一个 ScrollBudget”作为 `16ef968e` 的修复；第 1 项作为开放问题，不纳入 A2 的最小实现。

### 4.6 选中格守卫：重试也未必能解决 `e3bf895b`

**[事实]** `e3bf895b` 停止时还没有 `awaitSelectionReturned` 的有界等待；该修复是在这次现场之后加入的，修复后的真机复测尚未执行。[2026-10-26 报告](../reports/2026-10-26-macos-field-acceptance.md#选中格守卫的现场复现)

**[推断]** 如果选中框消失是点击末行触发客户端自动滚动或重排，重试从顶部再走到同一行、再点同一格，可能再次触发同样的反应；即使 `awaitSelectionReturned` 能吸收瞬时消失，也不保证能吸收一个由点击本身稳定触发的位移。位置重试在这种情况下会在同一处再次失败。

因此：

- 先做 `awaitSelectionReturned` 修复后的真机复测，再判断 A2 对 `e3bf895b` 是否还有价值（见开放问题 11）。
- 若消失是确定性的末行行为，A2 不是修复手段；应单独研究“避开末行点击/改用其他推进动作”。A1 只改变重复条目的处理与导出口径，同样不解决确定性行为，也不能替代位置证据。
- 若消失只是单帧动画，现有有界等待或位置重试可能足够；这需要现场日志证明，而不能用本实例直接推断。

### 4.7 重试预算与终止

- 每个位置最多一次位置级重试；重试内部仍保留现有的动作级重试（无效点击、滚动未位移等），但不对同一个位置做第二次位置级重试。
- 建议整个会话最多两次位置级重试（例如两个不同位置各一次）。连续多个位置失败通常意味着页面、筛选或客户端行为已经变化，继续重试只会延长错误状态。
- 用户在重试期间按停止快捷键、焦点丢失、几何变化或权限撤回时，立即中止重试；不因为“重试还没结束”而忽略停止。
- 重试失败后默认停止整个会话，记录第二次失败为该位置的最终失败；覆盖保持 `partial`。是否继续后续位置见开放问题 3。

### 4.8 不重试的边界

- 任何会话级故障（焦点、几何、权限、采集流、平台会话）都不重试。
- 任何证据/存储故障（`evidenceFailed`、`storageFailure`、保存失败）都不重试。
- 任何详情归属或识别证据缺口（`detailUnattributed`、`detailUnresolved`）都不自动重试。
- 任何范围/页面/筛选证据失效（`navigationMismatch`、`pageUnverified`、`anchorsMissing`）都不重试。
- 用户取消、崩溃恢复、识别运行库故障、无诊断码的 `controllerFailed` 不重试。

## 5. 对记录、检查点、恢复、导出和界面的影响

### 5.1 记录与检查点

- **内部 `observedScope` 扩展。[推断，需实现]** 增加每位置的 `attempts[]`、`recovered`，并保持每个位置一条记录；`recordPosition` 从“追加”改为“按 position 更新”，`updateCoverage` 仍要求位置 1–6 各一条。[inventory.go](../../internal/game/zzz/collect/inventory.go#L436)
- **逐行核验状态。[推断，需实现]** 为每个位置保留 `rowBase`、已解决绝对行集合和遮罩后的行图。第一次尝试进入每个稳定视口时，为其中每个已覆盖/占用的绝对行保存行图，滚动后仍可比较。行图只在本次会话内存中保留，按位置有界（一个位置最多几十行），不写入检查点或公共 JSON；崩溃后不续扫，因此不要求跨进程恢复。行图用于 4.2 的逐行灰度核验，任何不匹配即停止，不跳过；如果行图因内存上限被淘汰，对应行也不得跳过，必须停止或退 A1。
- **捕获证据增加尝试号。[推断，可选]** 在 `collect.Observation` 中增加 `attempt`（1 起），让同一位置两次遍历的 `viewportIndex` 不混淆。该字段位于不透明的 `capture` JSON 中，记录层不解析。[inventory.go](../../internal/game/zzz/collect/inventory.go#L174) [gameobservation.go](../../internal/app/gameobservation.go#L42)
- **检查点信封版本。[推断]** 如果新增字段只放在不透明的 `observedScope` 和 `capture` 中，`record.SessionShell` 的结构不变，`record.FormatVersion` 不必升级；`validate` 只检查 `observedScope` 是 JSON 文档。[checkpoint.go](../../internal/record/checkpoint.go#L15) [checkpoint.go](../../internal/record/checkpoint.go#L305) 如果改为在 `ItemRecord` 或 `SessionShell` 上增加 `attempt`/`superseded` 字段，则必须把内部检查点格式升到 v3，并让新读取端继续能读 v2 代次。
- **遍历中持久化。[事实与推断]** 当前 `observedScope` 只在预检后和 `Collect` 返回后上报，重试中的位置账目不会在中途落盘。[controller.go](../../internal/game/zzz/collect/controller.go#L262) 建议在“第一次尝试失败、决定重试”时推送一次 scope；否则进程在重试中崩溃时，记录只能看到预检范围或最终不完整状态，虽然不会错误升级覆盖，但会丢失“刚才失败过”的可审计事实。
- **恢复。[事实]** 崩溃恢复仍只加载最近有效检查点，排队/运行条目转为中断未处理，已存储的 `complete` 降为 `partial`，不自动续扫。[load.go](../../internal/record/load.go#L21) A2 是会话内重试；崩溃后仍按现有恢复语义处理，不新增断点续扫。
- **证据。** 第一次尝试的证据不删除、不改写；重试的新证据按现有 `evidence/` 规则保存。清理截图时，两类证据都按记录整体清理，不做跨尝试引用计数。

### 5.2 导出（scan JSON v2）

**A2 不改变 v2 的形状。[推断，基于现有合同]**

- `session.id`、`items[].sequence` 仍是会话内标识；重试不重编号。
- `capturedCount` 仍等于条目记录数；A2 没有第二次接纳同一格位，所以不会因为重试增加计数。
- `excludedCount = capturedCount - items.length` 不变；未处理、失败或不可用的条目本来就计入排除。
- `coverage.status` 仍只由六位置账目和排序证据推出；成功重试的位置只有在逐行核验和排序一致前提下才可以 `complete`，重试失败则为 `partial`/`unknown`。
- v2 的 `coverage` 只有 `status` 和 `expectedCount`；位置尝试账目留在内部 `observedScope`，不进入公共文件。如果产品要求导出文件披露“曾重试”，必须在 v3 增加字段，不能塞进 v2。

**如果选择 A1：** 需要把“被取代的尝试”排除出可用导出；`excludedCount` 会包含这些条目。v2 的字段形状仍可容纳，因为 `excludedCount` 的语义是“本文件排除的已采集记录数”，但界面和文档必须明确说明重试前的条目不计入可用数据；如果还要逐件标注“被取代”，则需要内部字段或 v3。[JSON v2 §2](scan-json-format.md#2-公共外壳)

**如果选择 B2：** 合并多次扫描会改变 `session`、`sequence`、`capturedCount`、`excludedCount` 和证据归属，属于新的公共合同；应按 v3 处理，不能复用 v2 的单会话语义。[JSON v2 §2](scan-json-format.md#2-公共外壳) [JSON v2 校验](scan-json-format.md#6-示例与校验边界)

**结论：** 推荐 A2 时不需要 scan JSON v2 升版；需要升版的是 A1 的逐件取代标注（如果要做）和 B2 的跨会话合并。

| 变更 | 版本影响 | 说明 |
| --- | --- | --- |
| `observedScope.positions[].attempts` / `recovered` | 不升内部检查点信封，不升 scan JSON | `observedScope` 对记录层不透明；公共 JSON 不包含它 |
| `capture.attempt`（可选） | 不升内部检查点信封，不升 scan JSON | 位于不透明的 `observed` 载荷；导出不读取 |
| 行图 / `rowBase` / 已解决绝对行集合 | 不升内部检查点信封，不升 scan JSON | 仅本次会话内存；不持久化，不进入公共 JSON |
| `ItemRecord.attempt` / `superseded` | 内部检查点 v3 | 记录结构新增字段，旧代次仍由新读取端兼容 |
| 公共 JSON 披露重试 | scan JSON v3 | v2 拒绝未定义字段；不能偷偷塞入 |
| 多次扫描合并导出 | scan JSON v3 | `session`、序号、计数和证据归属都改变 |

### 5.3 覆盖结论

- `coverage: complete` 仍要求六个位置全部完成、顺序正确、每个位置的最终尝试取得顶部与底部证据、所有已打开面板的排序标签验证通过。[inventory.go](../../internal/game/zzz/collect/inventory.go#L442)
- A2 的“位置完成”定义：最终尝试取得顶部与底部独立证据，且**最终尝试中每个视口的每个占用格位**要么在本次尝试访问/解决，要么在此前尝试的同一绝对格位已解决并逐行核验通过。重试不是一次“未解释的跳跃”：`attempts` 记录失败位置、重新建立顶部和继承集合；这些记录不完整时不能 `complete`。
- A2 的跳过还要求重试时排序标签已验证且与第一次尝试的同一标签一致；未验证时不跳过（停止或 A1，覆盖 partial），读到不同标签时停止。排序方向不可见是残余风险，不能把 `sort:"verified"` 当作方向证明（见第 8 节）。
- 重试成功不会把 `expectedCount` 变成已知；它仍只在有可信同范围总数时填写。
- 重试失败、崩溃恢复、排序标签未验证或不一致、转场不完整或逐行核验失败都必须阻止 `complete`，不能因为“大部分位置已完成”推断整仓。
- 已接纳但未处理/失败的条目不影响覆盖结论的“遍历完整性”，但会影响可用导出数量；这与现有语义一致。[架构第 5 节](scanner-architecture.md#完整性与身份)

### 5.4 界面

- **扫描进度：** 可以显示一行非阻塞状态，例如“位置 6：positionLost，正在自动重试（1/1）”，具体入口按现有扫描状态区实现。不要弹出模态对话框，也不要抢占前台；扫描期间恢复交互窗口或切换焦点本身会触发停止。
- **结果页：** 成功重试后可以显示“位置 6 自动重试 1 次后完成”；失败时沿用现有终止提示“扫描失败：positionLost（采集阶段）”，并保持 `partial` 覆盖。是否显示重试详情由 `observedScope` 决定，不改变结果行。
- **历史：** 覆盖列和计数不变；A2 不新增“继续扫描”按钮，也不新增跨记录合并入口。若未来采用 B/C，历史需要区分记录组或可续记录，那是新的界面设计。
- **校对/导出：** 不变。重试跳过的旧条目仍在结果列表中，未处理条目仍可查看证据；用户不会被要求为“自动重试”做额外确认。
- **无新增可调参数：** 重试次数、等待时长由应用管理，不暴露为扫描前设置，符合 T6 的“队列、OCR 线程、重试阈值不成为用户调参任务”。[T6 §1](t6-review-export-workflow.md#1-最小界面与完整使用流程)

### 5.5 日志与诊断

建议新增固定字段，沿用现有日志风格：

- 位置尝试结束：`youkai: collect: position=6 attempt=1 outcome=failed code=positionLost retry=1 ...`
- 重试准备：`youkai: collect: position=6 retry=1 preparing: wheel/filter/top ...`
- 继承格位：`youkai: collect: position=6 retry=1 inherited_rows=N skipped_rows=M row_check=passed ...`
- 行核验失败：`youkai: collect: position=6 retry=1 row_check=failed abs_row=R err=... action=stop ...`
- 重试结束：`youkai: collect: position=6 attempt=2 outcome=completed recovered=true ...`
- 现有 `position=N summary` 可增加 `attempts`、`recovered`、`skipped` 字段；耗时字段仍只用于观测，不参与决策。[development.md](../development.md#扫描耗时诊断)

## 6. 不变量与测试

### 6.1 必须始终成立的不变量

| 编号 | 不变量 | 验证方式 |
| --- | --- | --- |
| I1 | 每个 `seq` 只接纳一次，重试不复用、不重编号；未接纳动作不消耗序号 | 内核既有不变量 + 重试场景断言 |
| I2 | 同一位置的同一绝对格位在一次会话中最多产生一次 S 级接纳 | 绝对格位集合断言；重试时不点击已解决格位 |
| I3 | 重试只跳过第一次尝试中已经接纳 S 或确认非 S 的绝对格位，不跳过未知格位 | 继承集合与点击序列断言 |
| I4 | 最终尝试中每个视口的每个占用格位都被本次访问或先前解决并逐行核验覆盖；否则位置不能 `complete` | 位置账目、行图核验与访问序列断言 |
| I5 | 重走时每个要跳过的绝对行必须通过遮罩行图的灰度核验；占用位相同但行图不同、或第一次尝试存在重复行图而无法区分实例时，停止或退 A1，不跳过 | fake 序列 + 真机日志 |
| I6 | `capturedCount` 等于条目记录数；`excludedCount` 等于两者之差；重试不制造重复条目 | 检查点保存/加载与导出测试 |
| I7 | `coverage: complete` 只在六位置完成、排序已验证且重试标签与第一次一致、最终尝试顶部/底部成立时出现 | 覆盖账目测试 |
| I8 | 任何会话级故障、证据/存储故障、详情归属/识别缺口都不触发位置重试 | 故障码矩阵测试 |
| I9 | 重试准备必须重新验证转盘、`Active` 漏斗、几何和输入许可；排序标签必须已验证且与第一次尝试同一标签；未验证时不跳过（停止或 A1，partial），不同则停止 | fake 场景与真机验收 |
| I10 | 第一次尝试的证据和已接纳结果不删除、不改写；重试失败不把 `complete` 升级 | 记录/恢复测试 |
| I11 | 重试期间的停止、焦点丢失、几何变化立即中止，且不产生迟到接纳 | 并发/取消测试 |
| I12 | 重试不使用 `initialVerified`；首个未解决格位已选中时必须取得新的参照归属证据 | fake 场景断言 |
| I13 | 行图核验不得用于判断两件盘相同、跨尝试合并或删除观测；它只决定同一绝对格位能否继承 | 代码审查 + fake 序列 |

### 6.2 脚本化 fake 场景

以下场景应通过真实 `collect.Inventory` + 脚本化 `SessionView`/`Matcher` 驱动，不另写一套“重试状态机”替身；沿用现有 fake 画面序列风格。[replay_test.go](../../internal/game/zzz/collect/replay_test.go) [inventory_internal_test.go](../../internal/game/zzz/collect/inventory_internal_test.go)

1. **滚动中段 `positionLost`，重试成功：** 第一次尝试访问 N 件后在滚动处失败；第二次从顶部重走，跳过前 N 件，访问剩余格位并取得底部证据；断言无重复 seq、无重复点击、`capturedCount` 只增加新件、位置 `recovered`、六位置 `complete`。
2. **选中格守卫 `positionLost`，重试成功：** 选中框持续缺失超过 action budget；重试重新准备并跳过已接纳格位；断言守卫前的已接纳件不被重复接纳。另一个变体是重试点击同一行后选中框再次持续消失，断言重试不再重试并安全停止。
3. **底部短步（`16ef968e` 形态）：** `unsettled=false`、`rows=0`、误差 11.20 的序列；断言再增加一个 `ScrollBudget` 不会改变结论，位置安全停止且不伪造 `complete`。另一个变体是 `unsettled=true` 的序列，才测试延长等待是否能停稳。
4. **重试准备失败：** 第二次打开筛选后漏斗不是 `Active`，或转盘位置不对；断言不发送任何物品点击、位置保持不完整、终止码为对应的 `navigationMismatch`。排序标签未验证的变体断言不进入跳过路径（停止或 A1，覆盖 partial）；读到不同标签的变体断言停止，而不是继续跳过。
5. **重试期间失焦/几何变化：** 输入许可关闭或 frame 几何版本变化；断言立即中止、没有第三次尝试、没有迟到接纳。
6. **重试再次 `positionLost`：** 第二次尝试在另一处失败；断言只重试一次、位置不完整、会话停止、第一次尝试的条目仍在。
7. **不可重试故障矩阵：** `detailUnattributed`、`detailUnresolved`、`evidenceFailed`、`storageFailure`、`navigationMismatch`、`controllerFailed` 分别只产生一次尝试，不进入重试。
8. **绝对格位与像素行核验：** 构造占用模式完全相同但行图变化的序列（换位、排序方向翻转、置顶盘变化），断言逐行核验失败即停止，不跳过；再构造只有选中框/锁定角标闪烁的序列，断言遮罩后不误停。另构造第一次尝试出现重复行图的序列，断言不能用行图对应实例，停止或退 A1，不能靠内容去重处理。
9. **重试首件已选中：** 重试准备后第一个未解决格位已带选中框，且页面上仍显示上一件的详情；断言不使用 `initialVerified`，参照往返成功才接纳，参照失败则停止。
10. **未处理条目：** 第一次尝试的末件已被接纳但未处理；重试跳过其格位；断言该件仍为 `unprocessed`、`capturedCount` 不变、导出把它计入排除。
11. **账目与持久化：** 每次尝试更新同一 position 条目；`observedScope` 只有一条 position 6；失败后立即上报 scope 的检查点可读；重试中崩溃恢复为 `partial` 且不自动续扫。
12. **幂等与上限：** 两个位置各失败一次时最多重试两次；同一位置第二次失败不再重试；用户停止优先于重试预算。
13. **回归：** 无故障六位置扫描的条目数、序号、覆盖和导出与基线一致；相邻同详情、非 S 跳过、空位置、短列表等既有场景不受影响。

### 6.3 真机验收

**[推断] 以下步骤需要在用户确认方案并实现后执行；本草案本身不产生代码或真机结果。**

1. **基线对照：** 在 macOS 云客户端用同一库存快照跑一次无故障六位置扫描，记录总数、每位置访问数、序号范围、覆盖结论和导出 `capturedCount`/`excludedCount`。
2. **重试成功路径：** 复现一个自然的 `positionLost`（滚动或选中格守卫），或使用仅在测试构建中开启的一次性故障注入。确认日志依次出现第一次尝试失败、重试准备、继承行数、逐行核验通过、第二次尝试完成；导出总数与基线一致，无重复物品、无序号空洞（除既有失败/未处理项），`coverage: complete`。
3. **选中格守卫复测（`e3bf895b`）：** 先在 `awaitSelectionReturned` 修复后的构建上复测位置 6 的 seq 886 边界；确认当前代码是否能跨过该次停止。若修复后已能跨过，A2 对该实例的价值下降；若仍稳定停止，记录是否为末行点击触发的确定性行为，再决定 4.6 的处理方式。
4. **`16ef968e` 形态：** 在位置 1 末尾复现 3 行短步，确认 `unsettled=false`；不要在未改变证据的情况下重复等待预算。验证独立证据（如滑块位移交叉验证）或重新标定能否跨过 11.20；若不能，确认重试后安全停止且不声称 `complete`。
5. **重试失败路径：** 制造第二次 `positionLost`，确认只重试一次、会话停止、覆盖 `partial`、结果和证据保留；导出的 `capturedCount` 与条目数一致。
6. **不可重试路径：** 在一次扫描中触发 `focusLost` 或 `permissionRevoked`，确认没有位置重试，输入关闭，按既有收尾保存。
7. **崩溃恢复：** 在重试准备或重试遍历中强制结束进程，重新打开历史；确认记录可加载、未完成项为中断未处理、覆盖不高于 `partial`、没有自动续扫。
8. **边界物品核对：** 对 `e3bf895b` 的位置 6 边界（seq 886 附近）和 `16ef968e` 的位置 1 末行逐件核对：跳过的旧件仍在结果中，新增件不重复，未处理项可见。
9. **界面与日志：** 确认重试状态不抢焦点、不弹模态框，停止按钮/快捷键仍有效；历史与导出不出现新的未定义字段。

## 7. 需要用户决定的开放问题

| 编号 | 问题 | 选项 | 建议默认 | 影响 |
| --- | --- | --- | --- | --- |
| 1 | 采用哪种方案？ | A2（逐行核验 + 排序一致）/ A1 / B1 / B2 / C | **A2**，但仅在行图核验与排序标签一致前提下；若不愿承担行图存储/比较或方向不可见风险，或目标库存中行图无法区分重复行，**退到 A1**；B 仅在会话级故障或崩溃恢复成为主要痛点时另案 | 决定实现范围、合同是否升版和界面改动 |
| 2 | 每个位置重试几次、整个会话重试几次？ | 每位置 1 次 + 会话 2 次 / 每位置 1 次不设会话上限 / 其他 | **每位置 1 次，全会话 2 次** | 防止系统性故障下反复重试拖长错误状态 |
| 3 | 重试再次失败后是否继续后续位置？ | 停止 / 继续剩余位置并保持 `partial` | **先停止**；继续后续位置会改变现有“任一失败即停止”的用户决定，需要单独批准 | 影响可挽回的数据量和安全边界 |
| 4 | 已接纳条目如何处理？ | A2 继承跳过（逐行核验）/ A1 全量重走并标记取代 / 只补采未处理项 | **A2 继承跳过**；不自动补采未处理项 | 决定是否产生重复、是否需要 `excludedCount`/UI 说明 |
| 5 | 成功重试的位置能否声明 `complete`？ | 可以（满足并集、逐行核验与顶部/底部证据）/ 一律 `partial` | **可以**；否则重试失去完成整仓的意义 | 决定覆盖结论和导出语义 |
| 6 | 公共 JSON 是否要披露重试？ | 不披露（v2 不变）/ 增加字段（v3） | **不披露**；重试留在内部账目、日志和界面 | 决定是否需要 scan JSON v3 |
| 7 | 底部短步是否单独放宽证据？ | 只延长观察 / 新增“Bottom + 唯一短步 + 更长稳定”规则 / 重新标定或独立证据 / 保持 10.0 | **先区分 `unsettled`**：`unsettled=false` 不延长预算，改为重新标定或独立证据；不新增“Bottom + Lost”接受规则 | 决定 `16ef968e` 能否被覆盖，以及是否削弱既有权衡 |
| 8 | 重试是否自动进行，还是需要用户确认？ | 自动一次并显示进度 / 弹窗确认 / 只记录不重试 | **自动一次并显示非阻塞进度**；弹窗会抢焦点并触发停止 | 决定交互和扫描期间的焦点安全 |
| 9 | 是否需要跨会话/崩溃续扫？ | 暂不需要 / 采用 B 或 C | **暂不需要**；先验证 A2 的真机收益 | 决定是否启动 v3 或记录多段设计 |
| 10 | 未处理/失败的旧条目是否允许重读？ | 不重读（A2）/ 重读并标记旧条目被取代（A1） | **不重读**；保持无重复，接受少量未处理项 | 影响可用数据完整性和导出口径 |
| 11 | 是否先做 `awaitSelectionReturned` 修复后的真机复测，再决定 A2？ | 先复测 / 直接实现 A2 | **先复测**；`e3bf895b` 早于修复，不能证明当前仍会在同一处停止 | 决定 A2 对选中格守卫场景是否值得做 |
| 12 | 排序方向不可见是否可接受？ | 接受并依赖逐行核验 / 先补方向探针 / 退到 A1 | **先接受并记录残余风险，或在 A2 前补方向探针**；不能把 `sort:"verified"` 当作方向证明 | 决定 A2 的跳过证据是否足够严格 |

## 8. 事实、推断与未验证事项

**已确认的事实（基线）**

- 当前遍历在第一个位置错误处停止，后续位置不执行；每个位置有独立账目和独立准备。[inventory.go](../../internal/game/zzz/collect/inventory.go#L594)
- `positionLost` 同时覆盖滚动位置丢失和选中格守卫；`navigationMismatch` 覆盖页面、转盘、筛选、滚动无位移等另一类问题。[traversal.go](../../internal/game/zzz/collect/traversal.go#L63)
- 序号、`visitIndex`、`capturedCount`、`excludedCount` 和覆盖结论的现有语义。[handle.go](../../internal/scan/handle.go#L91) [checkpoint.go](../../internal/record/checkpoint.go#L330) [export.go](../../internal/app/export.go#L251)
- 当前 `observedScope` 只在预检后和 `Collect` 返回后上报；重试中不会持续落盘。[controller.go](../../internal/game/zzz/collect/controller.go#L262)
- T5 的排序探针只验证“稀有度”标签，不读取升降序方向；`sort:"verified"` 不声称方向已验证。[T5 §3](t5-zzz-inventory-traversal.md#3-开始前必须核对的页面条件)
- `e3bf895b` 的现场结果见报告；该次运行早于 `awaitSelectionReturned` 修复，修复后未复测；`16ef968e` 只有本次任务输入，未独立复核。

**推断（需要实现或验收）**

- A2 的绝对格位继承不能靠 `Occupied` 证明；必须对每个要跳过的绝对行保留遮罩行图并逐行核验。即使如此，排序方向翻转、行图完全相同但顺序变化等情况仍可能不可见；如果第一次尝试存在重复行图，行图不能证明实例身份，必须停止或退 A1。这些是残余风险，不能按内容去重处理。
- 重试成功的位置可以诚实声明 `complete`；这取决于并集覆盖、逐行核验和最终顶部/底部证据，而不是自动重试本身。
- `16ef968e` 的 `unsettled=false` 已用满 5 秒预算且没有 candidate；延长观察不能据此改变结论，只能靠重新标定或独立证据。第 4.5 节把它单列为开放问题。
- A2 不需要 scan JSON v2 升版；如果只改不透明的 `observedScope`/`capture`，内部检查点信封也不必升级。
- 重试的时间成本约为“滚动经过已解决行 + 点击未解决行 + 一次位置准备”，低于整位置重走，但尚未用真机计时；逐行行图核验还会增加内存与比较成本。

**未验证**

- `16ef968e` 的原始日志、帧对和误差细节；本文只使用评审提供的摘要与日志。
- `e3bf895b` 在 `awaitSelectionReturned` 修复后的现场行为；该复测尚未执行。
- 真实客户端在两次尝试之间是否可能改变列表顺序、排序方向、当前装备置顶或折叠状态；需要专门的真机序列验证。
- **残余风险：** T5 的排序探针只验证“稀有度”标签，不读取升降序方向；即使重试时标签已验证且与第一次一致，方向翻转仍不可见。逐行核验能发现多数重排，但不能证明方向，必须作为残余风险保留。
- 重试期间的云客户端动画、选中框回弹和滚动惯性是否与第一次尝试一致；需要更多样本。
- 行图核验的灰度阈值、遮罩范围、内存上限和逐行比较耗时尚未标定；需要专门样本和性能测量。
- 绝对格位继承在 Windows 本地的行为；本文的现场证据只来自 macOS 云客户端。
