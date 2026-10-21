# Youkai UI/UX 设计稿（2026-10-21）

基于 Evelyn / Sunna / Kimi 三份独立只读评审的交叉汇总。核心判断：状态模型已经严谨，主要问题是信息架构、校对导航和异步状态呈现；不需要更换 MyGo。完整 UI 清单与开发规范见 [ui-system.md](ui-system.md)。

代码当前已升级至 MyGo v0.6.2；本目录的静态稿与 PNG 仍按 v0.5.5 源码绘制，未重绘。

## 图

浅色与深色使用同一套 semantic token，只是两套映射。

| 页面 | Light | Dark |
|---|---|---|
| 01 信息架构 | [PNG](png/01-ia.png) | [PNG](png/01-ia-dark.png) |
| 02 扫描 | [PNG](png/02-scan.png) | [PNG](png/02-scan-dark.png) |
| 03 结果与校对 | [PNG](png/03-review.png) | [PNG](png/03-review-dark.png) |
| 04 历史 | [PNG](png/04-history.png) | [PNG](png/04-history-dark.png) |
| 05 设置 | [PNG](png/05-settings.png) | [PNG](png/05-settings-dark.png) |
| 06 组件与状态 | [PNG](png/06-components.png) | [PNG](png/06-components-dark.png) |
| 07 Token 对照 | [PNG](png/07-tokens.png) | — |

高清 2× 版本在同名 `png/` 文件；可编辑源文件为 `*.html` + [`style.css`](style.css) + [`dark.css`](dark.css)。`*-dark.html` 只是同一份 HTML 加了 `data-theme="dark"` 和 `dark.css`。

## 目标结构

```text
一个窗口
├── 扫描             入口；准备 → 进度 → 收尾
├── 结果与校对       主工作区；筛选 → 表格 → 详情/证据 → 确认
├── 历史             二级；修订列表 → 打开 / 导出 / 清理
└── 设置             二级；关窗、存储、更新、快捷键
```

- 顶部 Tabs 优先，不在首版引入侧栏：1020 宽下侧栏会挤压结果表。
- 结果与校对不拆成两个页面；校对是结果页的筛选与模式。
- 历史、存储、更新、关窗设置从结果页移出，释放首屏高度。
- 全局状态条只保留：阶段、保存状态、当前筛选、关键快捷键。
- 导出统一为模态，显示纳入/排除数、覆盖结论、文件名。

## 关键交互决定

- 扫描准备显示 6 项预检：客户端、窗口、布局、扫描范围、保存位置、停止方式。
- 扫描中显示“位置 n/6”，把已采集、已产出、失败、待处理分成四个计数卡片。
- 扫描完成自动进入结果页，并选中第一个待校对项。
- 结果页提供“待校对”筛选、状态列和“下一个待校对”。
- 详情栏默认显示证据截图；副词条按“属性 / 数值+单位 / 强化档位”四列对齐。
- 固定操作条：编辑（次要）、确认并下一项（唯一 Accent 主操作）、跳过。
- 删除类操作统一 Danger 色并分组；确认条默认焦点在“取消”，Enter 不触发删除。
- 历史行内只保留“打开”和“⋯”，避免每行堆叠五个按钮。
- 托盘扫描中显示“停止扫描并打开”，不再静默停止。
- 异步打开历史记录时立即清空旧内容，加载完成前禁用导出、删除、确认。

## 深色模式

深色 token 的完整方案在 [dark-tokens.md](dark-tokens.md)：内部语义 token 层、深色表面阶梯、强调色拆分、状态色、证据截图处理和验收清单。

## 视觉基线

不直接把 MyGo 的 Light/Dark 常量当组件颜色，而是：

```text
MyGo Theme → Youkai semantic tokens → component tokens
```

- 浅色/深色共用一套语义名：`surface.card`、`text.secondary`、`accent.fill`、`status.pending.fg`。
- `accent.fg` 与 `accent.fill` 分离，避免深色下浅蓝文字和实心按钮互相拖累对比度。
- 深色层级靠 surface 台阶 + 1px border，不靠阴影。
- 所有值见 [dark-tokens.md](dark-tokens.md) 和 [07-tokens.png](png/07-tokens.png)。

设计稿按 1280×800 画布呈现，实际窗口最小 860×560。落地后需在 1020×700、860×560、1440×900 三个尺寸做真机截图验收，重点看详情栏、表格列宽和中文长属性名。

## 状态

这些是静态设计稿，没有修改 `internal/ui`，也没有实现任何交互。落地顺序建议先修异步旧记录数据风险，再做四页壳，最后逐页迁移。
