# Youkai 深色模式颜色 token

基于 Evelyn / Sunna / Kimi 第二轮独立讨论，以及 MyGo v0.5.5 的 `ui.Theme`、`Table`、`Modal`、`paint` 源码。

## 结论

**不要扩展或 fork MyGo 的 `ui.Theme`。** 在 `internal/ui` 增加一层语义 token，组件只引用语义 token；只把 MyGo 已暴露的字段映射回 `ui.Theme`。MyGo 内置组件不会读取新字段，这一点必须明确隔离。

```text
L0  MyGo Theme（运行时）
    Background / Surface / SurfaceHover / SurfacePressed / Border
    Text / TextMuted / Accent / AccentHover / AccentPressed / AccentText
    Danger / Warning / Success / Selection / Focus / Scrollbar

L1  Youkai Tokens（internal/ui/tokens.go）
    surface.*  border.*  text.*  interact.*  status.*  intent.*

L2  组件 token（只给复用或语义特殊的组件）
    table.row  evidence.plate  confirm.bar  danger.zone  status.badge
```

不建 zinc-50…950 之类的原始色阶：MyGo 已经提供跟随系统外观的基础色，再维护一套色阶会出现两个事实来源，也会破坏高对比度和系统外观切换。

## 四个正交维度

命名必须区分“层级、交互态、数据状态、操作意图”，不能出现 `warning-hover`、`selected-warning` 这类跨维度复合名。

| 维度 | 回答的问题 | 命名 | 取值 |
|---|---|---|---|
| 层级 | 这块东西在哪一层 | `surface.*`、`border.*`、`text.*` | `window / content / card / header / overlay / sunken / evidence` |
| 交互态 | 用户此刻在对它做什么 | `interact.*` | `hover / pressed / selected / focus / disabled` |
| 数据状态 | 这条数据处于什么校对状态 | `status.<name>.{fg,bg,border}` | `pending / failed / confirmed / auto / unread / unprocessed / partial` |
| 操作意图 | 这个按钮会造成什么后果 | `intent.*` | `primary / neutral / destructive` |

规则：

- 数据状态不带交互态。选中一行“待校对”时，底色由 `interact.selected` 决定，“待校对”只通过图标和文字表达。
- 状态色与意图色分开。“失败”是记录状态，“删除”是操作意图；色相可以共用，token 必须分开。
- “警告”是严重度，不是校对状态。只有真正需要人工处理的行才用琥珀色；装备字段告警建议降为中性 ⓘ。
- “未读取”是有效状态，不是禁用。它必须看得清，使用 `status.unread.fg`，不能复用 `text.disabled`。

## 推荐深色配色

选择**轻微蓝灰**，比纯 zinc 更有 macOS 暗色质感，也能避免所有面板糊成同一种灰。强调色固定为 Youkai 蓝，不跟随系统强调色；系统强调色可以以后用于焦点/选区，但不要用于状态色。

### 表面与边框

| Token | Dark | Light | 用途 |
|---|---|---|---|
| `surface.window` | `#101318` | `#FFFFFF` | 标题栏、标签栏、状态栏 |
| `surface.content` | `#151920` | `#FFFFFF` | 页面底 |
| `surface.card` | `#1B2029` | `#FFFFFF` | 表格卡、详情卡、统计卡、设置分组 |
| `surface.control` | `#222833` | `#F4F4F5` | MyGo 控件面、输入、按钮 |
| `surface.header` | `#222833` | `#FAFAFB` | 表头、分段控件底 |
| `surface.overlay` | `#29303C` | `#FFFFFF` | 模态、下拉、toast |
| `surface.sunken` | `#0F1217` | `#F0F0F2` | 输入井、表脚、进度轨、空状态 |
| `surface.evidence` | `#090C11` | `#F4F4F5` | 证据截图底板（唯一近黑，不参与普通面板阶梯） |
| `interact.hover` | `rgba(255,255,255,.045)` | `#F4F4F5` | 行/按钮 hover |
| `interact.pressed` | `rgba(255,255,255,.08)` | `#DDDDE1` | 按下态 |
| `border.subtle` | `#242A34` | `#ECECF0` | 行分隔、卡片内分隔 |
| `border.default` | `#303845` | `#D9D9DE` | 卡片、输入框、表格外沿 |
| `border.strong` | `#46505F` | `#8A8A93` | 必需边界、浮层、active 输入框 |

深色层级相邻亮度差只有约 1.05–1.10:1，**1px 边框是必需的**。普通卡片、表格、详情不加阴影；只有模态/弹层使用 `Scrim + ShadowOverlay`。

### 文本

| Token | Dark | Light | 用途 |
|---|---|---|---|
| `text.primary` | `#F2F4F8` | `#18181B` | 标题、数值、主列 |
| `text.secondary` | `#C3C9D3` | `#3F3F46` | 正文、表单值、自动接受 |
| `text.muted` | `#98A1AE` | `#6B6B73` | 标签、说明、列头 |
| `text.disabled` | `#6F7886` | `#8A8A93` | 仅禁用；不要叠加 MyGo 的 50% 透明度 |
| `text.code` | `#C9D1D9` | `#3F3F46` | 路径、记录 ID、等宽文本 |

亮色 `text.muted` 建议从设计稿的 `#71717A` 调到 `#6B6B73`；`#71717A` 在 `#F4F4F5` 上约 4.40:1，不达 AA。

### 强调色

把“文字/图标用的 Accent”和“实心填充用的 Accent”分开，这是深色下最关键的修正。

| Token | Dark | Light | 用途 |
|---|---|---|---|
| `accent.fg` | `#60A5FA` | `#2563EB` | 链接、活动标签、图标、强调文字 |
| `accent.hover` | `#93C5FD` | `#1D4ED8` | 文字 hover |
| `accent.pressed` | `#3B82F6` | `#1E40AF` | 文字 pressed |
| `accent.fill` | `#2563EB` | `#2563EB` | 主按钮、MyGo 原生选中行背景 |
| `accent.fillHover` | `#1D4ED8` | `#1D4ED8` | 实心 fill hover |
| `accent.fillPressed` | `#1E40AF` | `#1E40AF` | 实心 fill pressed |
| `accent.onFill` | `#FFFFFF` | `#FFFFFF` | on `accent.fill` |
| `accent.soft` | `rgba(96,165,250,.14)` | `#EFF6FF` | 选中、badge、soft 背景 |
| `accent.softHover` | `rgba(96,165,250,.20)` | `#DBEAFE` | soft hover |
| `accent.border` | `rgba(96,165,250,.38)` | `#BFDBFE` | soft 边框 |

`#2563EB` + 白字约 5.2:1，`#1D4ED8` + 白字约 6.3:1，`#1E40AF` + 白字约 8.6:1。不要使用 MyGo 当前的 `#3B82F6` + 白字（约 3.68:1）或 `#60A5FA` + 白字（约 2.54:1）作为实心填充。

### 状态与意图

| Token | Dark foreground | Dark soft bg | Dark border | 备注 |
|---|---|---|---|---|
| `status.pending` | `#FBBF24` | `rgba(251,191,36,.12)` | `rgba(251,191,36,.30)` | 仅“待校对/部分覆盖”；配圆点 + 文字 |
| `status.failed` | `#F87171` | `rgba(248,113,113,.12)` | `rgba(248,113,113,.32)` | 配 ✕/▲ + “识别失败” |
| `status.confirmed` | `#4ADE80` | `rgba(74,222,128,.10)` | `rgba(74,222,128,.28)` | 配 ✓ + “已确认” |
| `status.auto` | `text.secondary` | transparent | `border.default` | 自动接受用中性色，避免整表发绿 |
| `status.unread` | `#858FA0` | transparent | `border.default` 虚线 | 仍写“未读取”，不是禁用 |
| `status.unprocessed` | `text.muted` | transparent | transparent | 与“失败”区分：不是错误，只是未运行 |
| `coverage.partial` | `#FBBF24` | `rgba(251,191,36,.10)` | `rgba(251,191,36,.30)` | 用横幅，不用 badge |
| `intent.destructive.fg` | `#F87171` | `rgba(248,113,113,.08)` | `rgba(248,113,113,.32)` | 红字 + 红边，不用整块红底 |
| `intent.destructive.solid` | `#DC2626` | — | — | 最终确认按钮；白字约 4.8:1 |

红色只给“失败”和“危险操作”两处；红色文字优先用 300 级 `#F87171`，`#EF4444` 只用于小图标和 2–3px 竖条。同一屏不要大面积同时出现红/琥珀/绿。

### 焦点、选中、遮罩

| Token | Dark | Light | 用途 |
|---|---|---|---|
| `interact.selected` | `rgba(96,165,250,.14)` + 3px 左条 `#60A5FA` | `#EFF6FF` + 3px `#2563EB` | 设计稿选中行；需自定义或 patch MyGo |
| `interact.focus` | `rgba(147,197,253,.85)` | `rgba(37,99,235,.85)` | 2px 焦点环 + 1px 间隙；仅键盘态 |
| `selection` | `rgba(96,165,250,.30)` | `rgba(37,99,235,.25)` | 文本选区 |
| `scrim` | `rgba(4,7,12,.64)` | `rgba(24,24,27,.32)` | 模态遮罩 |
| `shadow.overlay` | `rgba(0,0,0,.55)` | `rgba(24,24,27,.18)` | 仅模态/弹层 |

## MyGo 实现约束

这些约束决定了“设计 token”和“实际能生效的 token”必须分开记录：

- `ui.Theme` 有 19 个颜色位（`theme.go:7-56`），没有 card、table header、evidence plate、overlay 等层级。
- `Table` 表头没有背景色（`table.go:216`），设计稿的 `table.header` 不会自动出现。
- `Table` 选中行直接使用 `Theme.Accent` 作为背景（`table.go:561`），不是 `Selection`，也不会读取 `RowSelected`。
- 内置 `Modal`/`AlertDialog` 的遮罩硬编码 `RGBA(0,0,0,0.4)`（`widgets.go:183,601`）；app 自建对话框可用公开的 `DialogBase` 设置 backdrop/panel，不需要先改上游。
- `TextInput`/`Button` 共用 `Theme.Surface`（`textinput.go:18`），无法只靠 Theme 区分“输入井”和“按钮面”。
- `Divider` 使用 `Theme.Border`（`widgets.go:41`）；不要把 `Theme.Border` 提成 `BorderStrong`，否则所有分隔线都会变粗变亮。
- MyGo 对禁用元素整体乘 0.5 透明度（`paint.go:155`）；`TextMuted` 再乘 0.5 会降到约 2.4:1，禁用控件内部应使用 `TextPrimary`，或由 app 自己绘制禁用态。

因此：

1. `internal/ui/tokens.go` 放 Light/Dark 两套 `Tokens`，所有 hex 只出现一次。
2. `ApplyTheme(c)` 复制 `c.Theme()`，覆盖 MyGo 已有字段，保留 `Dark/FontSize/Spacing/Radius`；在 `view.go` 构建子控件之前调用 `c.SetTheme`。
3. app-composed 组件（卡片、表头、证据底板、确认条、危险区、状态徽标）显式使用 L1/L2 token。
4. `TableHeader`、`RowSelected`、`Scrim`、`EvidenceSurface` 标注为“设计 token，当前 MyGo 组件未消费”；不要误以为改 Theme 就能还原设计稿。
5. 首版不 fork MyGo：接受原生实心 Accent 选中行，`Theme.Accent` 用 `#2563EB` + `#FFFFFF`。如果坚持设计稿的浅底 + 左条，需要给 MyGo 提交上游 patch，或自建表格。

## 需要拍板的三个决定

1. **选中行**：设计稿是 `accent.soft + 3px 左条`，实现成本最低的是 MyGo 原生实心 Accent。建议首版用原生实心 Accent（`#2563EB` + 白字），并把选中行内的状态文字改为 `accent.onFill` + 图标形状；等 MyGo 上游支持 `RowSelected` 后再切回设计稿样式。
2. **是否跟随系统强调色**：建议固定 Youkai 蓝。系统强调色可能是黄/绿/紫，会让选中行、状态色和对比度不可控；如果产品坚持跟随，只让焦点/选区跟随，不要让状态色跟随，并逐一验证 macOS 的八种强调色。
3. **证据截图亮度**：建议默认不加滤镜，只靠 `surface.evidence` 底板、6–8pt 内边距、112–140pt 高度和 1px 边框控制眩光；放大查看器里可以提供默认关闭的“降低显示亮度”，但必须标注这是显示层处理，不改文件。

## 证据截图

- 底板 `#090C11`，1px `border.default`，圆角加在底板上，不裁剪截图。
- 图片按比例 contain，不拉伸、不铺满；高度上限 112–140pt。
- 默认不调亮度、不降饱和、不叠半透明黑；用户靠截图里的数字和颜色校对。
- 字段高亮只画 2px `accent.fg` 描边 + 1px 黑色外描边，不在文字上叠色块。
- 角标“证据 1/2 · 点击放大”放在底板上、截图外，不叠在截图上。
- 放大查看：遮罩 `rgba(0,0,0,.72)`，默认 1:1，超过 100% 用最近邻插值，底部注明“原始裁剪 · W×H px · 未处理”，Esc 关闭。
- 缺图：底板加斜线纹理 + 说明，不显示空白亮框。
- 设计稿里的深蓝假截图会掩盖眩光问题；深色验收必须换成真实亮红/白色游戏截图。

## 验收清单

1. **对比度**：`text.primary/secondary/muted` 对 `surface.card` 与选中行分别 ≥ 12 / 7 / 4.5；pill 文字对各自 soft ≥ 4.5；白字对 `accent.fill` ≥ 4.5；`#F87171` 对 danger-soft ≥ 4.5。用取色工具实测，不靠公式和字号。
2. **灰度层级**：整页转灰度，window / card / header / overlay / evidence 五层仍能分辨；相邻层分不清就加大亮度台阶，不要只加边框补救。
3. **截图融合**：放入真实最暗游戏截图，检查无白边闪烁、底板与卡片边界可辨、加载占位与“已删除”空态不像同一张图。
4. **键盘焦点**：纯键盘从表格走到“确认并下一项”，每个停靠点焦点环完整、不被滚动容器裁切、不与选中行填充融成一片；鼠标点击不出环。
5. **色觉模拟**：protanopia / deuteranopia 下，待校对/已确认/失败仅凭图标 + 文字仍可区分；危险确认条凭左条和边框形状可识别。
6. **禁用态**：截图检查最终合成像素，禁用控件文字 ≥ 3:1；不要用 `opacity` 叠加红色按钮，否则会变成脏褐色。
7. **尺寸**：1020×700、860×560、1440×900 三种窗口下，表头、详情操作条、设置卡片和证据底板不裁切。
