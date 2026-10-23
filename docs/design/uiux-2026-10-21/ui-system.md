# Youkai UI 系统清单

> **历史文档（2026-10-21）：** 本文件描述改版前的 UI 系统，已被 2026-10-22 round-3 实现取代；当前界面、token 与布局以 `internal/ui` 和测试为准。

这份文档把“界面里有什么、每个东西有哪些状态、颜色从哪里来、开发怎么落地”收在一处。图示见 [README](README.md)，深色 token 细节见 [dark-tokens.md](dark-tokens.md)。

## 1. 设计原则

1. **一个窗口，四个任务页。** 扫描、结果与校对、历史、设置；历史和设置是二级页面。
2. **一屏一个主操作。** Accent 只给当前页面最重要的动作，例如“确认并下一项”。
3. **状态必须可读。** 颜色只是第三重编码；每个状态同时有固定文字和不同形状的图标。
4. **危险操作远离常规操作。** 删除类统一 Danger 分组，默认焦点在“取消”。
5. **同一语义只维护一份值。** 组件只引用 token，不直接写 hex；浅色/深色是同一套语义的两套映射。
6. **证据不被装饰。** 截图不调色、不叠遮罩；靠底板、面积和边框控制眩光。

## 2. 信息架构

```text
主窗口
├── 扫描              入口：准备 → 进度 → 收尾
├── 结果与校对        主工作区：筛选 → 表格 → 详情 + 证据 → 确认
├── 历史              二级：修订列表 → 打开 / 导出 / 清理
└── 设置              二级：关窗、存储、更新、快捷键
```

全局：

- 顶部 Tabs：扫描 / 结果与校对（待校对 badge）/ 历史（条数 badge）/ 设置。
- 全局状态条：阶段、保存状态、当前筛选、关键快捷键。
- 托盘：空闲“打开主窗口”；扫描中“停止扫描并打开”；独立“停止扫描”；“退出”。
- 导出：统一模态，显示纳入/排除数、覆盖结论、文件名；不是一级页面。

## 3. 页面清单

### 3.1 扫描页

| 区域 | 内容 | 主操作 |
|---|---|---|
| 页面头 | 标题、上次扫描时间、扫描设置、打开数据目录 | — |
| 前台提示 | 扫描期间会自动操作游戏；保持前台 | — |
| 扫描准备 | 客户端、游戏窗口、布局与缩放、扫描范围、保存位置、停止方式，6 项检查 | — |
| 扫描范围 | 1–6 号位、全部套装、按等级排序、预计时长、自动保存 | — |
| 底部操作 | 开始扫描（⌘R）、打开游戏、开始后可隐藏到托盘 | 开始扫描 |
| 右栏 | 上次扫描统计、停止方式、扫描源、数据目录 | 查看上次结果 |

扫描中替换为：

- 阶段：准备中 / 采集中 / 收尾中 / 已结束。
- 位置进度：`位置 n/6`。
- 四个计数卡：已采集、已产出、失败、待处理；失败 > 0 时才用 Danger。
- 保存状态：已保存 / 有内容待写入 / 保存失败 + 重试保存。
- 结束原因：正常结束 / 用户取消 / 故障结束，带阶段和可读原因。

### 3.2 结果与校对页

| 区域 | 内容 | 主操作 |
|---|---|---|
| 页面头 | 本次/历史记录标题、保存条数、覆盖结论、导出 JSON/CSV | 导出 |
| 摘要 | 可用记录、待校对、失败/未处理、覆盖结论 | — |
| 筛选栏 | 全部 / 待校对 / 失败 / 未处理；搜索；套装；槽位；下一个待校对（⌘J） | 下一个待校对 |
| 结果表 | 序号、套装、槽位、等级、主词条、校对状态、问题 | — |
| 详情栏 | 序号、状态、主词条、副词条四列、校验结果、证据截图 | 确认并下一项 |
| 底部操作条 | 编辑、确认并下一项（⌘↵）、跳过、保存修改（⌘S） | 确认并下一项 |

状态口径：

- 待校对：需要人工处理。
- 已确认：人工确认。
- 自动接受：自动通过，默认中性色，不铺绿。
- 失败：识别失败。
- 未处理：未运行识别，不是错误。
- 未读取：字段级有效状态，必须看得清。
- 部分覆盖：会话级横幅，不消失。
- 待校对 0 时必须同时说明“失败/未处理 Y 件不会导出”。

### 3.3 历史页

| 区域 | 内容 | 主操作 |
|---|---|---|
| 页面头 | 总条数、最近一次、打开数据目录、导出历史 | 打开记录 |
| 筛选栏 | 日期区间、今天/近 7 天/近 30 天/全部、搜索、来源 | — |
| 修订表 | 日期、扫描源、采集、可用/排除、待校对、覆盖、截图、大小、操作 | 打开 |
| 存储概览 | 记录数据、截图、缓存、日志、磁盘可用；清缓存、清旧日志、刷新 | — |
| 清理区 | 删除全部截图、删除全部记录；预览影响范围 | — |

行内只保留“打开”和“⋯”菜单；导出、删除截图、删除记录进入菜单，避免每行五个按钮。

### 3.4 设置页

| 分组 | 内容 |
|---|---|
| 窗口与启动 | 隐藏到托盘继续运行 / 停止扫描并退出 |
| 存储管理 | 记录、截图、缓存、日志占用；清缓存、清旧日志、刷新；删除入口 |
| 软件更新 | 当前版本、构建号、自动检查、检查更新、状态 |
| 快捷键 | 开始、停止、确认并下一项、保存、放弃、下一个待校对 |
| 托盘与窗口 | 空闲、扫描中、退出三种菜单文案 |
| 关于 | 版本、数据目录、诊断日志、开源许可 |

## 4. 组件清单

### 4.1 窗口与导航

- 窗口：1280×800 设计画布，最小 860×560；标题栏、Tabs、内容区、状态条。
- Tabs：激活项 Accent 文字 + 2px 下划线；badge 用 Warning/中性。
- 全局状态条：左侧状态、当前筛选；右侧快捷键。
- 模态：导出确认、删除确认、证据放大；Scrim + Overlay + 焦点陷阱。

### 4.2 操作控件

| 组件 | 变体 | 状态 |
|---|---|---|
| Button | primary / secondary / ghost / danger | default / hover / pressed / disabled / focus |
| IconButton | 24×24、透明底 | hover / focus / disabled |
| TextInput | 单行、数值、日期 | default / focus / invalid / disabled |
| Select | 套装、槽位、单位、管理状态 | default / open / disabled |
| SearchField | 表格/历史筛选 | default / focus / filled |
| Segmented | 全部/待校对/失败/未处理 | selected / hover |
| Checkbox / Radio | 批量选择、设置 | checked / unchecked / disabled |
| Switch | 自动检查更新 | on / off / disabled |
| Keycap | 快捷键提示 | — |

### 4.3 数据展示

- Table：固定列宽、表头、行 hover、行 selected、排序、空态、加载态。
- StatusBadge：pending / failed / confirmed / auto / unread / unprocessed；文字 + 图标 + 颜色。
- StatCard：标签、数值、单位、语义色；失败 > 0 才 Danger。
- DetailPanel：标题、状态、字段、副词条、校验、操作条。
- SubstatGrid：序号、属性、数值+单位、强化档位；四列对齐。
- EvidencePlate：截图底板、证据角标、缩放按钮、缺失态。
- Callout：info / warning / danger；只用于小面积提示。
- EmptyState：标题、说明、下一步按钮；虚线边框。
- LoadingState：Spinner + 文字；旧内容必须清空或标记过期。
- Toast/Notice：操作成功、取消、失败；统一位置，不散落。

### 4.4 危险操作

- DangerZone：浅色/深色 danger surface + 左侧 3px 红条。
- ConfirmBar：标题、影响范围、取消（默认焦点）、确认删除。
- 文案必须写清对象：“删除此记录 / 删除选中的 3 条 / 删除全部截图”，不复用同名“删除当前记录”。

## 5. 状态矩阵

### 5.1 应用状态

| 状态 | 界面表达 | 禁止 |
|---|---|---|
| 空闲 | 扫描页准备、上次结果入口 | 显示未注册的快捷键 |
| 准备中 | 预检项逐项显示 | 假装已就绪 |
| 扫描中 | 位置 n/6、四计数、停止入口 | 允许导出/清理/确认 |
| 收尾中 | 停止输入 → 完成识别 → 保存 | 只显示“收尾中”无步骤 |
| 已结束 | 结束原因、保存状态、进入结果 | 隐藏失败/未处理 |
| 保存失败 | 全局横幅 + 重试保存 | 只在小区域显示 |
| 退出中 | 停止 → 保存 → 释放资源 | 允许新操作 |

### 5.2 记录状态

| 状态 | 文字 | 图标 | 默认动作 |
|---|---|---|---|
| 待校对 | 待校对 | ◐ | 确认/编辑 |
| 已确认 | 已确认 | ✓ | 下一项 |
| 自动接受 | 自动接受 | ✓ 空心 | 可复查 |
| 失败 | 识别失败 | ✕/▲ | 查看证据/重扫 |
| 未处理 | 未处理 | ⊘ | 查看原因 |
| 未读取 | 未读取 | 虚线值框 | 补全字段 |
| 部分覆盖 | 部分覆盖：原因 | 横幅 | 查看覆盖详情 |

### 5.3 交互状态

- hover：行/按钮背景变化，不使用 Accent。
- selected：行浅底 + 左侧 Accent 条（目标设计）；首版可用 MyGo 原生实心 Accent，但状态文字改 onFill + 图标。
- focus：2px 焦点环 + 1px 间隙，仅键盘态。
- disabled：专用 disabled 色，不叠加 50% 透明度；旁边写原因。

## 6. Design tokens

### 6.1 分层

```text
L0  MyGo Theme
L1  Youkai semantic tokens (internal/ui/tokens.go)
L2  component tokens
```

**不建原始色阶。** MyGo 已经提供跟随系统外观的基础色；再建 zinc-50…950 会产生两个事实来源，也会破坏高对比度和系统外观切换。

### 6.2 命名

四个正交维度：

| 维度 | 命名 | 示例 |
|---|---|---|
| 层级 | `surface.*` / `border.*` / `text.*` | `surface.card`、`text.secondary` |
| 交互态 | `interact.*` | `interact.hover`、`interact.focus` |
| 数据状态 | `status.<name>.{fg,bg,border}` | `status.pending.fg` |
| 操作意图 | `intent.*` | `intent.destructive.border` |

禁止 `warning-hover`、`selected-warning` 这类跨维度复合名。

### 6.3 浅色 / 深色

完整值见 [dark-tokens.md](dark-tokens.md) 和 [07-tokens.png](png/07-tokens.png)。核心：

| 语义 | Light | Dark |
|---|---|---|
| window | `#FFFFFF` | `#101318` |
| content | `#FFFFFF` | `#151920` |
| card | `#FFFFFF` | `#1B2029` |
| header | `#FAFAFB` | `#222833` |
| sunken | `#F0F0F2` | `#0F1217` |
| evidence | `#F4F4F5` | `#090C11` |
| text.primary | `#18181B` | `#F2F4F8` |
| text.secondary | `#3F3F46` | `#C3C9D3` |
| text.muted | `#6B6B73` | `#98A1AE` |
| text.disabled | `#8A8A93` | `#6F7886` |
| accent.fg | `#2563EB` | `#60A5FA` |
| accent.fill | `#2563EB` | `#2563EB` |
| accent.onFill | `#FFFFFF` | `#FFFFFF` |
| danger | `#B91C1C` | `#F87171` |
| warning | `#B45309` | `#FBBF24` |
| success | `#15803D` | `#4ADE80` |

注意：

- `accent.fg` 和 `accent.fill` 必须拆开；MyGo 当前 `#3B82F6` + 白字只有 3.68:1。
- 深色层级靠 surface 台阶 + 1px border，不靠阴影。
- 证据截图用 `evidence` 底板，不加滤镜。
- 状态色不跟随系统强调色；只让 Accent 系跟随（如果产品决定跟随）。

### 6.4 字号、间距、圆角

| Token | 值 |
|---|---|
| 页面标题 | 25pt / 700 / -0.024em |
| 区域标题 | 12.5–13pt / 650 |
| 正文 | 12–12.5pt |
| 辅助文字 | 10.5–11pt |
| 表格 | 12pt，行高 40pt |
| 间距 | 4pt 基数：4/6/8/10/12/16/20 |
| 圆角 | 6 / 7 / 8 / 10 / 14 |
| 焦点环 | 2px + 1px 间隙 |

## 7. MyGo 落地

### 7.1 已落地结构

`internal/ui/tokens.go` 已新增，并在 `View` 开头调用 `ApplyTheme(c)`；`tokens_test.go` 覆盖浅/深色值、运行时指标保留、高对比度和对比度阈值。

```go
// internal/ui/tokens.go
type Tokens struct {
    SurfaceWindow, SurfaceContent, SurfaceCard, SurfaceHeader Color
    SurfaceSunken, SurfaceOverlay, SurfaceEvidence Color
    BorderSubtle, BorderDefault, BorderStrong Color
    TextPrimary, TextSecondary, TextMuted, TextDisabled, TextCode Color
    AccentFG, AccentFill, AccentFillHover, AccentOnFill Color
    AccentSoft, AccentBorder Color
    DangerFG, DangerSoft, DangerBorder, DangerSolid Color
    WarningFG, WarningSoft, WarningBorder Color
    SuccessFG, SuccessSoft, SuccessBorder Color
    RowHover, RowSelected, RowSelectedBorder Color
    Focus, Selection, Scrim Color
    TableHeader, TableHeaderText, TableBorder Color
    EvidenceSurface, EvidenceBorder Color
}
```

```go
func ApplyTheme(c *ui.Context) *Tokens {
    base := *c.Theme()
    tok := tokensFor(base.Dark)
    base.Background = tok.SurfaceContent
    base.Surface = tok.SurfaceHeader
    base.Border = tok.BorderDefault
    base.Text = tok.TextPrimary
    base.TextMuted = tok.TextMuted
    base.Accent = tok.AccentFill
    base.AccentText = tok.AccentOnFill
    // ... 其余 Theme 字段
    c.SetTheme(&base)
    return tok
}
```

在 `View` 构建子控件之前调用 `ApplyTheme(c)`。

### 7.2 MyGo 内置组件的限制

- `Table` 表头没有背景色，表头文字固定 `TextMuted`。
- `Table` 选中行硬编码 `Theme.Accent` + `Theme.AccentText`。
- `Modal` 遮罩硬编码 40% 黑。
- `TextInput` 与 `Button` 共用 `Theme.Surface`。
- `Divider` 使用 `Theme.Border`；不要把 `Theme.Border` 提成强边框。
- 禁用元素整体乘 0.5 透明度。

处理顺序：

1. **Theme 覆盖**：能直接设的字段全部设。
2. **app 组合/自绘**：表头、分隔线、证据底板、确认条、危险区。
3. **MyGo 上游 patch**：需要 `TableHeader` / `RowSelected` / `Scrim` 时。
4. **最后才 fork**：只在改布局/行为且上游不接受时。

### 7.3 组件 token 对照

| 组件 | 主要 token |
|---|---|
| Window | `surface.window`、`border.default` |
| Tabs | `surface.window`、`text.muted`、`accent.fg` |
| Card | `surface.card`、`border.default` |
| Table header | `table.header`、`table.headerText`、`border.default` |
| Table row | `text.secondary`、`border.subtle`、`row.hover`、`row.selected` |
| Status badge | `status.*.fg/bg/border`、`text.*` |
| Primary button | `accent.fill`、`accent.onFill` |
| Danger button | `danger.fg`、`danger.border`、`control.bg` |
| Evidence plate | `evidence.surface`、`evidence.border`、`accent.fg` |
| Confirm bar | `surface.sunken`、`border.default`、`accent.fill` |
| Danger zone | `danger.soft`、`danger.border`、`danger.fg` |

## 8. 可访问性

- 正文 `text.primary` 对卡片 ≥ 12:1；`text.secondary` ≥ 7:1；`text.muted` ≥ 4.5:1。
- 所有状态必须有文字 + 图标；灰度/色盲下仍可区分。
- 焦点环只在键盘态出现，2px + 1px 间隙，不能被滚动容器裁切。
- 禁用控件文字 ≥ 3:1；不靠颜色单独表达禁用。
- 证据截图不调色；放大超过 100% 用最近邻插值。
- 支持系统深/浅色；如果加手动切换，持久化选择并写进设置页。

## 9. 验收清单

1. 1020×700、860×560、1440×900 三尺寸无裁切。
2. 浅/深色下页面/卡片/表头/证据/模态五层可分辨。
3. 所有删除确认默认焦点在“取消”，Enter 不触发删除。
4. 待校对筛选、下一个待校对、状态列可用。
5. 旧记录加载中导出/删除/确认被禁用。
6. 证据缺失、加载中、失败都有下一步。
7. 系统强调色、高对比度、文字缩放不破版。
8. 真实 1000 行结果 + 截图滚动/内存通过。
9. 托盘动态文案、Dock、第二实例、深链接行为一致。
10. 没有任何组件直接写 hex；CSS 变量与 Go token 同名。

## 10. 实施顺序

1. P0 正确性：异步旧记录、托盘文案、停止方式、待校对口径。
2. 四页壳：Tabs + 单一滚动区 + 全局状态条。
3. 结果/校对：筛选、状态列、下一个待校对、副词条四列、固定操作条。
4. 历史/导出/清理：修订表、导出模态、危险分组。
5. 准备/证据/设置：预检、证据缩放、快捷键、托盘文案。
6. 深色 token：`internal/ui/tokens.go` + Theme 映射 + MyGo 缺口标注。
