# Youkai 扫描结果 JSON 与 CSV v2

更新日期：2026-11-19。本文只维护当前公共导出契约：JSON 使用 `formatVersion: 2`，CSV 使用第 5 节的固定列序。两种格式均由 [`internal/app/export.go`](../../internal/app/export.go) 生成，管理状态为必需字段，装备状态与装备者为可选字段；装备信息未解析时保留空值和 warning，不阻止可用导出。P-12 第一步的独立补扫记录仍使用 v2：`scope.slots` 只列本次会话选中的号位，覆盖为 `partial`，计数等式与 CSV 列序不变。会话、保存与物品身份以[公共架构第 5 节](scanner-architecture.md#5-扫描会话与结果语义)为准；操作流程见 [T6](t6-review-export-workflow.md)。

## 1. 两种导出格式，同一数据源

首版只有两种导出格式，均来自同一次已保存修订、同一批可用记录；不重新 OCR、不改单位、不重新编号：

| 格式 | 文件 | 内容 |
| --- | --- | --- |
| JSON | `youkai-<日期>-<短会话 ID>.json` | 当前可用记录及来源、范围、覆盖、排除数量，和只含实际引用条目的目录子集。 |
| CSV | `*.youkai.csv` | 同一批记录的单表宽表；一行一件盘，副词条展开为 `sub1` 至 `sub4`。 |

两种文件都不含截图或原始识别审计，都是单向导出，不是跨次库存同步指令，缺少某件物品不表示应删除它。公共文件仅导出已收尾、封存且保存成功的快照；运行中的排队、运行和取消句柄继续由内部持久化契约维护，不进入公共文件。

所有 JSON 使用 UTF-8、无 BOM、无注释、无重复键，字段名大小写严格匹配。对象键顺序无语义，物品按采集序号递增输出。`formatVersion` 使用整数，未知版本拒绝解释；v2 不接受未定义字段或枚举，新增会改变读取契约的字段时升版，不预设动态插件、任意扩展包或迁移框架。

## 2. 公共外壳

时间采用 UTC 文本 `YYYY-MM-DDTHH:mm:ss.sssZ`；计数、序号、修订号和像素尺寸均为 JSON 整数，最大为 `9007199254740991`，不以浮点数承载标识。

| 字段 | 类型与含义 |
| --- | --- |
| `format` / `formatVersion` | 固定 `"youkai.scan"` / `2`。 |
| `exportedAt` | 此文件导出时间。 |
| `producer` | `{ "name": "youkai", "version": string }`，版本非空。 |
| `session` | 下表定义的来源会话。 |
| `catalog` | `{ "namespace": "youkai.zzz", "revision": string, "sets": object, "agents": object }`。两张对象表均为十进制字符串键到显示名称的映射，只携带本文件实际引用的条目。 |
| `fieldPolicy` | 固定 `{ "management": "required", "equipped": "optional" }`；含义见第 3 节。 |
| `validationRules` | `{ "id": "zzzDriveDisc", "version": string }`。当前导出使用应用内置规则，版本非空；`version` 是内置资料表的修订号，表内容变化即递增，与 `catalog.revision` 相同。不是原始识别资源版本，见下方当前校验契约。 |
| `excludedCount` | 本文件排除的已采集记录数；等于 `session.capturedCount - items.length`。 |
| `items` | 第 3 节的记录数组。可用项为零时允许空数组，不丢失来源与覆盖信息。 |

### 标识与目录子集

套装和代理人都使用 fairy 数值 ID：套装如河豚电音 `31100`，代理人如耀嘉音 `1311`。公共 JSON 的 `data.setId` 和 `data.equipped.agentId` 是数值；`catalog.sets`、`catalog.agents` 的键是该 ID 的十进制字符串（JSON 对象键只能是字符串），值是可读显示名，例如 `"31100": "河豚电音"`、`"1311": "耀嘉音"`。读取端按数值 ID 关联，显示名称不承担身份。

`catalog` 只导出文件实际引用的条目：`sets` 只含 `items[].data.setId` 出现过的套装，`agents` 只含 `data.equipped.agentId` 非空的代理人。不导出固定资料中的完整 30 套或 60 人表，也不因某个套装或代理人曾在其他会话出现而补录。目录只是文件自解释的名称投影，不能取代应用内置的固定校验资料。资料修订记录版本，已发布 ID 保持身份；新增套装或代理人属于资料更新，不必提升文件结构版本。

单件驱动盘没有可读的唯一游戏 ID，仍用 `session.id + items[].sequence` 标识一次观测；`catalog` 不是单件盘索引，不能用来跨会话合并物品。属性完全相同的两件盘各保留一条记录，禁止用属性哈希、数组位置或目录键充当物品身份。

| `session` 字段 | 类型与含义 |
| --- | --- |
| `id` | 小写标准 UUID 文本；与 `items[].sequence` 共同标识一次观测。 |
| `savedRevision` | 已持久化的结果修订号，非负整数；与单件 `revision`、UI 快照版本分开。 |
| `game` / `entity` | v2 固定 `zzz` / `driveDisc`。一文件只有一种游戏和实体。 |
| `client` | `windowsLocal`、`windowsCloud`、`macosCloud`，对应三种已确认入口。 |
| `locale` | v2 固定 `zh-CN`，对应用户已确认的首版游戏识别语言；不表示已经通过识别验收，也不规定应用界面语言。 |
| `startedAt` / `endedAt` | 开始和实际收尾时间；中断恢复且结束时刻不可知时，`endedAt` 为 `null`，不拿恢复时间冒充结束。 |
| `termination` | `{ "kind": "completed" 或 "cancelled" 或 "failed" 或 "interrupted", "code": string, "phase": "preparing" 或 "capturing" 或 "finalizing" }`。保留首个结束触发。 |
| `scope` | `{ "rarity": "S", "slots": [1,2,3,4,5,6], "minLevel": 0, "maxLevel": 15, "sets": "all", "locked": "all", "discarded": "all", "equipped": "all" }`。这些是游戏筛选条件与声明范围，不是 `data` 字段。首版全量扫描为全部套装、六个号位，且不按锁定、弃置或装备状态排除盘；缩小范围须明确更新契约，不能把 UI 临时筛选偷换为来源范围。**P-12 第一步的独立补扫记录**（2026-11-19）是明确的会话范围收窄：`slots` 只列该会话选中的号位，`coverage.status` 固定为 `partial`；独立补扫的计数只属于本次补扫会话，CSV 列序保持原样。补扫范围数据损坏（`positions`/`rescanOf` 缺失、空值、类型错误或非法范围）时拒绝导出，不能按六号位生成误导文件；旧整仓记录仍为六号位。 |
| `coverage` | `{ "status": "complete" 或 "partial" 或 "unknown", "expectedCount": integer 或 null }`。总数只有同范围可信证据时才填；它不是 `complete` 的必要条件。 |
| `capturedCount` | 当前修订内已接纳的 S 级物品观测数，包括后来失败或未处理项。导航尝试、参照物截图、重访前被截住的失败、明确非 S 项及合并时归档的观测不计入。 |
| `slotSources` | 仅合并了补扫号位的记录写出。数组按号位升序，每个条目包含 `slot`、采集来源的 `sessionId` 与 `savedRevision`，以及 UTC 会话时间窗 `startedAt` / `endedAt`。未到达的号位不列来源，结束时刻未知时为 `null`。 |

按号位合并的记录保留原记录的 ID、声明范围与原会话的终止原因。合并后的范围来自不同时间的采集，`coverage.status` 为 `partial`，`expectedCount` 为 `null`。`slotSources` 的时间是采集会话起止时间，各号位可能来自同一个会话。[合并的用户流程与归档规则](t6-review-export-workflow.md#合并回原记录p-12-第二步)。

**重访口径：** 只有尚未确认采集的失败（如详情归属未确认）才稍后重访；重访在接纳前截住，不分配 `sequence`，不产生第二条观测。已采集但 OCR/字段有问题的件交给现有逐件校对流程，不重访。旧尝试只留内部诊断，不计入任何件数、不进 `excludedCount`。因此 `capturedCount` 只计已接纳观测，`capturedCount = items.length + excludedCount`；`expectedCount` 只在有可信同范围证据时填写，且不是完整覆盖的必要条件（[决策 §4.5](../decisions/2026-10-28-scanner-rearchitecture.md#45-k4完整性)、[§9.4(a)](../decisions/2026-10-28-scanner-rearchitecture.md#94-scan-json-formatmd已同步选定-a)）。

**位置范围未证明（[决策 §4.6](../decisions/2026-10-28-scanner-rearchitecture.md#46-k5故障分级)）：** 该位置此前已接纳物品仍在 `items` 中、不计入 `excludedCount`，`coverage.status` 只报 `partial`；完整可用性与标注口径见 [T5 §3](t5-zzz-inventory-traversal.md#3-开始前必须核对的页面条件)。位置准备失败或位置证明失败（T5 §5 的位置级降级，2026-10-31 已实现）同样只令 `coverage.status=partial`；位置级原因留在会话内部 `observedScope`、诊断与结果界面，不进入 v2。P-12 第一步的独立补扫记录同样只报 `partial`，其 `scope.slots` 收窄到本次选中号位；独立补扫与按号位合并分别导出。补扫预检失败、未打开输入且没有号位账目时仍报 `partial`，终止阶段表达“没有开始”，不伪造位置结论。

`termination.code` 为非空诊断代码，建议使用 `verifiedEnd`、`userCancelled`、`focusLost`、`geometryChanged`、`permissionRevoked`、`navigationMismatch`、`positionFailureLimit`、`evidenceFailed`、`runtimeFailure`、`storageFailure` 或 `interrupted`。`evidenceFailed` 表示已接纳截图的投影或入队失败，保留原始错误；`positionFailureLimit` 表示不完整位置累计达到 3 个而主动停止整次扫描（T5 §5）；它们只用于解释，不授权额外动作；读取端可展示不认识的代码，不能因此把 `kind` 改为正常完成。后续故障不会覆盖首个触发。

`termination.phase` 是首个结束触发所在的阶段，不是文件导出时的状态。正常遍历到末尾通常为 `completed + capturing`；准备阶段已验证空库存可为 `completed + preparing`，不统一改写成 `finalizing`。

范围描述预期扫描什么，`coverage` 描述实际覆盖到哪里，`excludedCount` 描述导出舍弃多少已采集记录，三者独立。即使 `coverage.status=complete`，仍可能有排除项。`complete` 不由 OCR 成功率或 `expectedCount` 单独推出；有可信总数时必须与采集数一致，否则保留冲突并降为 `partial` 或 `unknown`。按 T5 的严格排序规则，只有排序标签已读取并验证时才允许 `complete`；未验证时只能为 `partial` 或 `unknown`。

## 3. 一件驱动盘

每条记录必需 `sequence`、`revision`、`quality`、`issues`、`validation`、`data`。公共文件只包含可用记录：`data` 及必需字段均已知，质量已自动接受或人工确认，当前校验通过。下文的未知状态说明字段含义及排除原因，不表示必需字段为空的记录也能导出。

| 字段 | 规则 |
| --- | --- |
| `sequence` | 记录内稳定序号。原始观测从 1 开始编号，合并加入的观测在历史最大序号之后分配新号，保留的观测不重编号。重访在接纳前截住，不分配序号。禁止用属性哈希或数组位置充当身份。 |
| `origin` | 仅合并引入的观测写出，包含原采集会话的 `sessionId` 和原 `sequence`。当前记录的标识仍用 `session.id + items[].sequence`。 |
| `revision` | 原始结果为 0；每次已保存的人工修改或整件确认递增。只反映此件变化。 |
| `quality` | 可用记录为 `autoAccepted` 或 `humanConfirmed`；`needsReview`、`notEvaluated` 的记录不进入公共文件。记录本修订的识别或人工确认状态，不说明库存覆盖，也不能代替当前资料校验。 |
| `issues` | 本修订的识别或人工校对问题数组，元素为 `{ "field": string 或 null, "code": string, "severity": "error" 或 "warning", "message": string }`。`null` 表示整件问题。 |
| `validation` | `{ "status": "passed", "issues": array }`，问题结构同 `issues`，可保留 warning。对应本件当前 `revision` 与顶层 `validationRules`；`failed`、`unavailable` 的记录不进入公共文件，规则见下文。 |
| `data` | 以下全部字段组成的非空对象；尚无规范化结果的记录不能导出，不得用空对象冒充成功识别。 |

| `data` 字段 | 类型与业务语义 |
| --- | --- |
| `setId` | fairy 套装数值 ID 或 `null`。非空时必须是整数，并能在 `catalog.sets` 的十进制字符串键中解析。 |
| `rarity` | 固定 `S`，来自已验证的画面或筛选证据，不从文件声明凭空补齐。 |
| `slot` / `level` | 槽位为 1 至 6，等级为 0 至 15；未知分别为 `null`。 |
| `mainStat` | 第 4 节的词条对象或 `null`。 |
| `substats` | 按画面从上至下的词条对象数组（第 4 节，含强化档位），最多四条；数量不明为 `null`，确认没有可见副词条才用 `[]`。已知存在但未读出的行保留带未知值的词条对象，不能删掉该行。 |
| `managementState` | `"locked"`、`"discarded"`、`"none"` 或 `null`。真实游戏状态只有前三态；`null` 表示未读到、不可读或证据冲突。没有 `unknown` 枚举值。 |
| `equipped` | 整体可为 `null`（未读到）；读到后为 `{ "state": "equipped" 或 "unequipped", "agentId": number 或 null }`。`{ "state": "equipped", "agentId": null }` 是未完成观测，作为可选字段产生 warning 后仍可进入可用数据。 |

`managementState` 的三态互斥由图标决定：盘格右下角红色垃圾桶为 `discarded`，灰色锁为 `locked`，无图标为 `none`。列表盘格右下角角标是主要依据；顶部“拆解”左侧指示只确认锁定/未锁定维度，不能区分弃置与无标记，弃置/无标记仍由列表角标区分。列表与顶部在锁定维度冲突或任一不可读时记 `null`。锁定角标带有闪光动画，单帧可能漏检；多帧采样与颜色加形状分类仍是 PR 6a 的待验证项，不得把未验证结果写成 `none`。

装备状态从详情页等级右侧头像判断并匹配代理人身份，列表格右上角头像确认；冲突或不可读时整个 `equipped` 记 `null`。当前代理人装备和他人装备都属于 `equipped`，当前装备置顶只影响遍历顺序，不构成额外的数据状态。`unequipped` 时 `agentId` 必须为 `null`；`equipped` 且 `agentId` 非空时，`agentId` 必须是 fairy 代理人数值 ID（例如耀嘉音 `1311`），并能在 `catalog.agents` 中解析；`agentId=null` 只产生 warning，不阻止导出。

未知、否定和空集合不能互换。`managementState=null` 不等于 `none`，`equipped=null` 不等于 `unequipped`，`substats=[]` 只陈述读到了空列表。识别不到装备者不等于未装备；管理状态与装备分别保留，不能仅凭用户回忆的图标位置合并或补默认值。

`fieldPolicy` 在 v2 固定为 `{ "management": "required", "equipped": "optional" }`：

- `management: "required"`：`managementState` 必须为 `locked`、`discarded` 或 `none`，不能为 `null`。
- `equipped: "optional"`：`equipped` 可为 `null`，也可为 `{ "state": "equipped", "agentId": null }`；`unequipped` 时 `agentId` 必须为 `null`，`equipped` 且 `agentId` 非空时必须是可在目录中解析的 fairy 数值 ID。装备状态或装备者未解析只产生 warning，不阻止记录进入可用数据；`equipped=null` 仍不等于 `unequipped`，不得补默认值。

按 v2 的校对方案，可用数据接受所有必需字段已知、`validation.status=passed`、问题数组没有 `error`，且 `quality` 为 `autoAccepted` 或 `humanConfirmed` 的记录。`validation.issues` 中可以保留可选的 `equipped` warning。自动接受记录无需逐件人工确认，问题记录经校对确认后再纳入；说明见 [T6](t6-review-export-workflow.md#导出前校对的已确认规则)。仅保存局部修改不会自动确认整件。修改自动接受或已确认条目后，新修订进入 `needsReview`，整件确认后才可输出。手动补录可使原始失败或未处理记录变为可用，但不得改写其原始处理结局。

### 当前校验

`validation` 是对当前数据的可重算结论，与原始识别和人工确认分开。新扫描、保存人工修改或重开历史时，使用应用随包且明确固定的当前规则重新检查字段策略、资料标识、单位、槽位、等级及数值格式；不下载新规则，不用历史确认代替检查。顶层 `validationRules` 记录实际所用规则。

- `passed`：全部必需检查完成且无错误，`validationRules` 必须非空；可选的 `equipped` warning 可以保留。
- `failed`：已发现缺字段或领域冲突，`validation.issues` 至少有一个 `error`。
- `unavailable`：尚无已知字段冲突，但缺少必需字段或规则无法解释该目录版本，不能完成全部检查；至少有一个说明资料缺口的 `error`，不能作为可用项导出。`equipped` 缺失或装备者未解析不属于 `unavailable`，只作为 warning。

重开历史记录时，界面同时显示原确认状态和当前问题，将 `failed/unavailable` 列入待处理；允许查看原图、保存未完成修改，但禁止整件确认与可用导出，直至当前检查通过。`equipped` warning 不阻止整件确认或可用导出。界面不会把人工修订的 `needsReview` 自动升为自动接受。

仅更新校验而未发生人工修改或确认时，保存只增加 `session.savedRevision`，不增加单件 `revision`；规则和结果均未变则不制造新修订。后台校验绑定所读的物品修订和规则版本，旧回包不得覆盖新值。导出基于同一已保存修订，按当前内置规则重验并冻结数据快照，供 JSON 与 CSV 共用；JSON 中每件均为 `passed`，规则对象非空，空记录文件也保留规则标识与版本。只读消费端不能把旧校验当作当前应用的验收结果，重新使用或转换时仍应按自己的校验契约检查。

## 4. 精确数值和单位

主词条使用 `{ "attribute": string 或 null, "value": string 或 null, "unit": "flat" 或 "percent" 或 null }`；副词条的词条对象另有必需的 `enhancementRolls` 字段（整数或 `null`）：画面显示 `+N` 记 N，无 `+N` 记 0，行存在但未读出为 `null`，取值 0–5；主词条无此字段。它是画面档位的原始观测，不做任何数值或强化次数推算（[架构字段校验](scanner-architecture.md#字段校验范围与资料来源)）。未读出部分以 `null` 保留；完整主词条的三个字段与完整副词条的四个字段均非空。

- `value` 为非负十进制字符串，规范写法匹配 `^(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`。不接受指数、正负号、千分位、单位后缀、前导零或多余末尾零。零使用 `"0"`；数值为零是否业务合法另行校验。
- `flat` 表示固定点数，`percent` 表示显示百分数，例如 `"9.6" + "percent"` 就是 9.6%，并非比例值 0.096。保留的是画面可见精度，不声称得到游戏内部未显示的小数。
- `"9.60%"` 的原始写法在 OCR 记录中保留，规范值为 `"9.6"`。实际使用定点、十进制或有理数解析；未来某个消费接口需要比例时，在其适配边界明确除以 100。
- `score` 属于模型分数，可用普通有限 JSON 数值；它不参与游戏数值计算，也不表示正确概率。不要为所有数字都引入十进制包装。

| `attribute` | 含义 | 合法 `unit` |
| --- | --- | --- |
| `health`、`attack`、`defense` | 生命值、攻击力、防御力 | `flat` 或 `percent` |
| `critRate`、`critDamage` | 暴击率、暴击伤害 | `percent` |
| `penetration` | 穿透值 | `flat` |
| `penetrationRatio` | 穿透率 | `percent` |
| `anomalyProficiency` | 异常精通 | `flat` |
| `anomalyMastery`、`impact` | 异常掌控百分比、冲击力百分比 | `percent` |
| `energyRegen` | 能量自动回复百分比 | `percent` |
| `physicalDamage`、`fireDamage`、`iceDamage`、`electricDamage`、`etherDamage`、`windDamage` | 对应属性伤害加成 | `percent` |

此表定义编码，不宣布所有名称、属性和布局已获实机支持。槽位与主词条类型组合、副词条类型、强化档位 0–5 由固定资料校验，见[架构字段校验](scanner-architecture.md#字段校验范围与资料来源)；副词条不能重复相同的 `attribute + unit`，强化档位不参与重复判定。首版不按等级核对具体数值，`value` 记录画面读到的值。OCR 得到不在规则中的类型或名称时，保留原始文字和问题，不自动改为最近的合法值。

十进制字符串是一项格式设计取舍：每条多两个引号，省去二进制浮点往返误差和百分数约定不清的问题，不增加用户运行库。JSON 标准允许解析器限制数值精度，也明确提示重复键会导致不同读取行为，因此本契约额外要求精确字符串和重复键拒绝。[RFC 8259 第 4、6、8.1 节](https://www.rfc-editor.org/rfc/rfc8259)

## 5. CSV 导出

CSV 是单表宽表，一行一件可用盘，记录集合、顺序与同次 JSON 一致；副词条按画面顺序展开到 `sub1` 至 `sub4`。文件使用带 BOM 的 UTF-8，便于电子表格识别中文；首行为表头，逗号分隔，行末为 LF。含逗号、双引号或换行的字段加双引号，字段内的双引号写成两个。空值写为空字段，不写 `null` 文本；`0` 是有效数值，不能当作空值。

下表逐列列出实际表头，顺序由 `csvHeader()` 固定；`data` 均指同次 JSON 的 `items[].data`。

| 序号 | 列名 | 来源与含义 |
| --- | --- | --- |
| 1 | `sequence` | `items[].sequence`；保留采集序号及间断。 |
| 2 | `sessionId` | `session.id`；每行重复当前记录 ID，与 `sequence` 共同标识该记录中的观测。 |
| 3 | `revision` | `items[].revision`，不是 `session.savedRevision`。 |
| 4 | `quality` | `items[].quality`；可用行只为 `autoAccepted` 或 `humanConfirmed`。 |
| 5 | `validationStatus` | `items[].validation.status`；可用行固定为 `passed`。 |
| 6 | `setId` | `data.setId`，十进制整数文本。 |
| 7 | `setName` | 当前内置资料按 `setId` 查得的显示名，与同次 JSON 的 `catalog.sets` 对应。 |
| 8 | `rarity` | `data.rarity`，固定 `S`。 |
| 9 | `slot` | `data.slot`。 |
| 10 | `level` | `data.level`。 |
| 11 | `mainStatAttribute` | `data.mainStat.attribute`。 |
| 12 | `mainStatValue` | `data.mainStat.value`；保留规范十进制文本，不换算百分数。 |
| 13 | `mainStatUnit` | `data.mainStat.unit`。 |
| 14 | `sub1Attribute` | `data.substats[0].attribute`。 |
| 15 | `sub1Value` | `data.substats[0].value`；数值语义同主词条。 |
| 16 | `sub1Unit` | `data.substats[0].unit`。 |
| 17 | `sub1Rolls` | `data.substats[0].enhancementRolls`；`0` 写为 `0`。 |
| 18 | `sub2Attribute` | `data.substats[1].attribute`。 |
| 19 | `sub2Value` | `data.substats[1].value`。 |
| 20 | `sub2Unit` | `data.substats[1].unit`。 |
| 21 | `sub2Rolls` | `data.substats[1].enhancementRolls`。 |
| 22 | `sub3Attribute` | `data.substats[2].attribute`。 |
| 23 | `sub3Value` | `data.substats[2].value`。 |
| 24 | `sub3Unit` | `data.substats[2].unit`。 |
| 25 | `sub3Rolls` | `data.substats[2].enhancementRolls`。 |
| 26 | `sub4Attribute` | `data.substats[3].attribute`。 |
| 27 | `sub4Value` | `data.substats[3].value`。 |
| 28 | `sub4Unit` | `data.substats[3].unit`。 |
| 29 | `sub4Rolls` | `data.substats[3].enhancementRolls`。 |
| 30 | `managementState` | `data.managementState`；可用行只为 `locked`、`discarded` 或 `none`。 |
| 31 | `equippedState` | `data.equipped.state`；`equipped=null` 时为空。 |
| 32 | `equippedAgentId` | `data.equipped.agentId`，十进制整数文本；未解析装备者、`unequipped` 或 `equipped=null` 时为空。 |
| 33 | `agentName` | 当前内置资料按 `equippedAgentId` 查得的显示名，与同次 JSON 的 `catalog.agents` 对应；没有代理人 ID 时为空。 |

不足四条副词条时，对应的整组四列为空，不补零或移动其他行；已纳入的副词条必须完整。可用行的必需字段非空，装备相关列允许为空；`equippedState=equipped` 时，`equippedAgentId` 和 `agentName` 也可能为空。显示名称只用于阅读，不能替代数值 ID。

CSV 每行带 `sessionId`、套装名称和可解析的代理人名称，但不包含完整会话外壳、`session.savedRevision`、范围、覆盖、`catalog` 对象、`excludedCount`、规则版本或问题数组。需要这些元数据及装备 warning 的具体原因时导出 JSON。CSV 没有单独的格式版本列，读取端应核对完整表头；空记录文件仍带 BOM 和表头，来源会话等信息需从同次 JSON 获取。JSON 与 CSV 不包含截图、原始识别或修订历史，清理截图不影响已经导出的文件。

## 6. 示例与校验边界

[JSON 示例](../examples/scan-usable-v2.json)和[同源 CSV 示例](../examples/scan-usable-v2.csv)为合成记录，用于解释格式；资源版本、规则版本、会话和字段均不构成游戏扫描或模型准确性证据。示例演示人工确认记录、两类六号位百分比主词条、副词条强化档位、当前校验、三种管理状态、已装备代理人和已取消会话的部分结果。CSV 的三行与 JSON 的序号 2、3、4 一致；首行带来源会话、河豚电音和耀嘉音，后两行未装备，代理人 ID 与名称均为空。

公共文件没有应用内导入入口。消费端校验时按顺序检查：

1. **文件与结构：**UTF-8、重复键、大小写、格式版本、必需字段、允许字段、枚举、时间、整数和十进制语法。一次 JSON 解码不能单独满足所有契约要求。
2. **关联与对账：**会话及序号唯一、顺序、采集与排除数、目录标识、修订连续性和当前校验。`capturedCount = items.length + excludedCount`；同一序号只出现一次；`catalog` 只含实际引用条目，数值 ID 与十进制字符串键一致。重访只对尚未确认采集的失败发生，且在接纳前截住、不分配新序号；旧尝试不进公共文件，也不改变本条等式。
3. **领域与质量：**固定资料中的套装与代理人数值 ID、槽位与主词条组合、副词条类型、单位、等级范围、副词条强化档位 0 至 5、字段策略和当前修订确认状态；数值只检查格式。遍历全部合法属性与单位，包含六号位 `impact + percent`、`anomalyMastery + percent`。公共文件的 `managementState` 只接受三种真实状态；`null` 表示未解析，不能纳入可用导出，也不能补成 `none`。`equipped` 的不变量按第 3 节检查。结构合法不等于识别正确，人工确认也不保证客观零错误。
4. **往返：**同属性双盘仍为两件；`null` 不变否定，`managementState=null` 不写成 `none`；导出再读取保留精确值、部分覆盖和原始序号；JSON 与 CSV 来自同一修订和同一批记录。没有截图时仍能解释缺失，不伪造证据。

当前生成端使用 Go `encoding/json` 的 `Encoder`，关闭 HTML 转义并使用两空格缩进；CSV 使用 `encoding/csv.Writer`，先写入 UTF-8 BOM。这里的文件版本 `2` 指 Youkai 公共 JSON 契约，不表示使用了某个 JSON 标准库版本。编码实现见 [`internal/app/export.go`](../../internal/app/export.go)，不应把消费端建议误当作已经实现的公共文件导入器。

外部工具适配不在本版范围；出现明确需求后再增加独立转换器并核对真实导入，不改变本格式的原始语义。
