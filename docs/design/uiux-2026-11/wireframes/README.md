# 第二阶段低保真线框图

**第二阶段草案**（2026-11-03）。灰阶静态 HTML，1 CSS px = 1 DIP；窗口按默认尺寸 1180 × 760 DIP 绘制（08 为最小尺寸 960 × 640）。只表达结构、层级与尺寸，不表达视觉；视觉沿用 `internal/ui/tokens.go`。示例件数为虚构，只用于说明层级。

每张图右侧的编号标注说明设计理由与出处，规格正文见 [08](../08-screens.md) 与 [09](../09-window-and-interaction.md)。`png/` 是同名 HTML 的 1600 px 宽截图，方便预览。

| 线框 | 屏 / 状态 | 对应规格 |
| --- | --- | --- |
| [01](01-home-first-run.html) · [png](png/01-home-first-run.png) | 首页 · 首次进入（权限不满足） | 08 §1、§1.1 |
| [02](02-home-return.html) · [png](png/02-home-return.png) | 首页 · 回访（全部就绪，上次有待校对） | 08 §1、§1.3 |
| [03](03-home-preparing.html) · [png](png/03-home-preparing.png) | 首页 · 准备中 / 准备超时 | 08 §1.2 |
| [04](04-home-wrapup.html) · [png](png/04-home-wrapup.png) | 首页 · 扫描中（窗口可见）/ 收尾 / 保存失败 | 08 §1.2 |
| [05](05-summary-complete.html) · [png](png/05-summary-complete.png) | 记录 · 摘要（覆盖全部、1 件待校对） | 08 §2 |
| [06](06-summary-partial-fault.html) · [png](png/06-summary-partial-fault.png) | 记录 · 摘要（部分覆盖 / 故障结束） | 08 §2.1 |
| [07](07-review-default.html) · [png](png/07-review-default.png) | 记录 · 逐件（默认宽度） | 08 §3 |
| [08](08-review-narrow.html) · [png](png/08-review-narrow.png) | 记录 · 逐件（最小窗口、补录、保存失败） | 08 §3.2、09 §2 |
| [09](09-export-dialog.html) · [png](png/09-export-dialog.png) | 导出对话框（导出前 / 成功） | 08 §4 |
| [10](10-records.html) · [png](png/10-records.png) | 记录列表、行菜单与删除确认 | 08 §5、§9 |
| [11](11-settings.html) · [png](png/11-settings.png) | 设置（发现新版本时） | 08 §6 |
| [12](12-tray-and-notifications.html) · [png](png/12-tray-and-notifications.png) | 托盘菜单、结束通知、首次隐藏提示 | 08 §7–§9、09 §4.7 |

## 重新渲染

用 Google Chrome 的无头模式逐张截图（macOS 上中文由系统的苹方字体渲染；样式表依次回退到微软雅黑等 Windows 字体）：

```sh
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=1 --window-size=1600,900 \
  --screenshot=png/01-home-first-run.png "file://$PWD/01-home-first-run.html"
```
