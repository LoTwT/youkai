# 扫描器背景与参考实现

整理日期：2026-10-15（2026-10-16 精简）。状态：背景资料。本文保留项目起因、参考扫描器的研究结论和与 fairy 的边界；交接时的待定事项均已由后续规格收敛，当前设计以[架构](specs/scanner-architecture.md)和各专项规格为准。

来源：[fairy 中的原始讨论](lody://session/f29b5ca0-ed64-4be9-b08b-4829bb1c9393?workspace=jh71jrcd7n3s48y9vbscr9p8w98dgyyz)。

## 1. 用户目标

用户从研究 `ZztIsolation/zzz_calculator` 的驱动盘扫描功能开始，随后澄清：

> 我不是要迁移，我是想做个新的，同时未来也可能支持其他游戏的通用扫描器。

> 我主要是想使用 https://mygo.egoist.dev/，所以想问 go。

因此 youkai 是新建的扫描器，以 MyGo 为桌面框架，先做绝区零驱动盘，未来可能支持其他游戏；不迁移参考项目代码。项目位于 `/Users/caoyujie/codes/youkai`，独立于 fairy。

## 2. 参考扫描器的研究结论

- [zzz_calculator 研究快照](https://github.com/ZztIsolation/zzz_calculator/tree/d58e276973c3df944bc36fc81a783f3d17018708)：计算器负责启动或连接扫描器并导入结果。
- [ZZZ-Scanner.Next 研究快照](https://github.com/ZztIsolation/ZZZ-Scanner.Next/tree/ff90891140016d3f1b738d73cb6cd9b291e17cec)：同作者的 C#/.NET 8/WinForms 扫描器（Scanner 1.0.49）。

已读源码的流水线：

```text
窗口定位与前置检查
  → 串行选择物品 / 导航仓库
  → 确认选中、详情变化及画面稳定
  → 截图并提取识别区域
  → 有界队列与 OCR 工作线程
  → 按采集顺序合并结果
  → 游戏词典与数值规则清洗
  → 完整或部分结果导出 / 流式发送
```

- 通过可见画面和鼠标操作扫描，不读游戏内存或注入。
- 截图使用 DXGI，GDI 回退；OCR 使用 PP-OCRv5 识别模型的 ONNX 推理，固定区域预处理、张量布局、归一化和 CTC 解码都是实现的一部分。
- 图像队列 48、OCR 工作线程至多 2 个；这是被研究实现的参数，不是 youkai 的默认值。
- 部分有限字段用模板识别，按阈值和候选差距决定是否回退 OCR；1.0.49 的识别区域随副词条数量变化。
- 扫描终止可能生成部分结果；计算器对部分导入关闭“删除未出现物品”。

值得继承的问题意识：

1. 导航正确性与 OCR 正确性是不同问题，必须确认截图属于刚选择的目标。
2. 两件物品可以属性完全相同，按属性去重会丢失真实库存。
3. 数值合法不足以证明识别和自动纠正正确，应保留识别依据。
4. 有界队列、结果排序、取消和部分成功需要共同设计。
5. 新游戏的差异常在仓库交互和详情就绪判定，换一份坐标配置通常不够。

已见限制：`NativeEdgeClick` 在某些路径中根据稳定的画面变化假定前进一行，而非测得滚动距离；成功导出没有保留全部原始 OCR 与置信依据；`ScanController` 职责集中。这些是阅读结论，未在现场复现。原项目报告的实机数量、分辨率和速度是作者数据，不是本项目实测。

其他参考：[YAS](https://github.com/wormtql/yas/blob/main/yas/src/lib.rs) 将截图、输入、OCR、坐标放在共享模块，游戏流程与结果语义留在各游戏实现（[原神仓库控制器](https://github.com/wormtql/yas/blob/main/yas-genshin/src/scanner_controller/repository_layout/controller.rs)），可参考模块边界。Go 标准库 [`plugin`](https://pkg.go.dev/plugin) 不支持 Windows，游戏适配器作为普通包编译进程序。

## 3. 与 fairy 的边界

fairy 的[包规范](../../fairy/docs/specs/packages.md)规定 `@randomplay/core` 负责计算，`@randomplay/data` 负责来源与标准化资料，私有 shared 维护共同契约。youkai 独立维护扫描能力、导出契约和发布节奏；复用 fairy 资料时只读取其公开、固定版本的数据制品，不依赖私有 shared，不修改 fairy。首版取用哪些资料见[架构字段校验](specs/scanner-architecture.md#字段校验范围与资料来源)。
