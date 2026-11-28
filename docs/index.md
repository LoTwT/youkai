# 文档索引

## 文档导航

按任务选择入口。新增、移动或移除文档时，同步更新相对链接。

- 下载安装、开始扫描或反馈问题，见[项目 README](../README.md)。

### 开发、发布与验收

- 配置环境、运行应用、复现诊断或执行测试时，查阅[开发指南](development.md)。
- 修改模块边界、会话、存储或支持范围时，查阅[扫描器架构](specs/scanner-architecture.md)。
- 查阅里程碑、实施顺序和验证记录时，使用[首版实施清单](specs/scanner-implementation-plan.md)。
- 核对 Windows 三轮实扫、安装、签名升级、免验决定或首发后检查时，查阅 [M4 验收记录](specs/scanner-implementation-plan.md#m4双平台交付验收)。
- 准备标签、签名制品或验证更新渠道时，查阅[发布与自动更新](release.md)。
- 确定 Windows 安装包、支持系统和交付门槛时，查阅 [T7 构建打包与交付验收](specs/t7-public-release-acceptance.md)。
- 查阅项目起因、参考扫描器和与 fairy 的边界时，使用[项目背景](scanner-handoff.md)。

### 专项规格

| 文档 | 查阅场景 |
| --- | --- |
| [T1 MyGo 界面与构建分发](specs/t1-mygo-ui-distribution.md) | 原生界面、应用资源、安装与浏览器唤起。 |
| [T2 平台采集与输入](specs/t2-platform-capture-input.md) | 窗口身份、客户区、DPI、支持档位、输入许可与采集时序。 |
| [T3 OCR 运行库、模型与样本](specs/t3-ocr-runtime-model-samples.md) | 固定依赖、资源摘要、预处理和识别质量评估。 |
| [T4 扫描会话与结果契约](specs/scanner-architecture.md#5-扫描会话与结果语义) | 接纳记账、取消、恢复、保存与资源所有权。 |
| [T5 绝区零仓库遍历与详情归属](specs/t5-zzz-inventory-traversal.md) | 旧仓库与驱动仓库入口、筛选范围、滚动衔接、号位证据、相同盘、末尾、覆盖和 Windows 实测。 |
| [T6 用户操作、结果校对与导出](specs/t6-review-export-workflow.md) | 逐件校对、按号位补扫与合并、导出预检、历史和清理。 |
| [扫描结果 JSON/CSV v2](specs/scan-json-format.md) | 公共字段、空值、计数及列序。另见同源 [JSON](examples/scan-usable-v2.json) 与 [CSV](examples/scan-usable-v2.csv) 示例。 |

### 界面设计与资产

- [第五版视觉与实现约束](design/uiux-2026-11/13-v5-visual-system.md)维护当前组件、主题和布局规则。
- [2026-11 重设计第一阶段](design/uiux-2026-11/README.md)保存功能清单、约束、用户旅程和状态矩阵。
- [第二阶段信息架构](design/uiux-2026-11/07-information-architecture.md)通往逐屏规格、交互规则与线框；当前呈现同时查阅第五版视觉规则。
- [第三阶段实施计划](design/uiux-2026-11/12-implementation-plan.md)记录 PR 拆分与实机验证安排。
- [2026-10-21 UI 设计基线](design/uiux-2026-10-21/README.md)用于追溯旧版设计。
- [项目图标原稿](design/icon-2026-11-17/a-vortex-eye.png)用于再生成应用、窗口、安装器和托盘图标，命令见 [T1 图标约定](specs/t1-mygo-ui-distribution.md#应用与托盘图标)。

### 决策与历史方案

- [扫描器重新架构决策](decisions/2026-10-28-scanner-rearchitecture.md)记录范围、覆盖、故障恢复和客户端 profile 的取舍。
- [按位置重试与续扫草案](specs/position-retry-design.md)保留旧方案及其被取代的部分。当前恢复契约见 T5。

### 实测与升级报告

| 报告 | 查阅场景 |
| --- | --- |
| [M1 Windows 安装](reports/2026-10-17-m1-windows-test.md)、[M1 macOS 安装](reports/2026-10-17-m1-macos-test.md) | 早期安装、识别自检、启动和卸载基线。 |
| [M1 自动更新](reports/2026-10-18-m1-auto-update-test.md) | 早期测试渠道升级链与重启证据。 |
| [M2 macOS 输入](reports/2026-10-18-m2-macos-field-round1.md)、[M2 macOS 样本](reports/2026-10-19-m2-macos-sample-round.md) | 云客户端采集、输入与调参样本。 |
| [M3 规模测试与验收清单](reports/2026-10-20-m3-acceptance-checklist.md) | 合成数据规模测试和历史 macOS 整仓轮次。 |
| [macOS 真机验收 10-26](reports/2026-10-26-macos-field-acceptance.md)、[macOS 阶段 2 验收 11-01](reports/2026-11-01-macos-stage2-field-acceptance.md) | 全量扫描、导出、停止与滚动证据。 |
| [Windows 本地探针](reports/2026-11-01-windows-local-probe.md)、[W3a 样本轮](reports/2026-11-02-windows-local-sample-round.md)、[早期全量扫描](reports/2026-11-02-windows-local-field-acceptance.md) | 客户区、权限、排序样本和早期全扫基线。 |
| [Windows 托盘诊断](reports/2026-11-03-windows-tray-diagnosis.md)、[Windows 第三阶段界面验收](reports/2026-11-14-windows-uiux-acceptance.md) | 托盘恢复诊断和第五版之前的交互证据。 |
| [11-14 依赖升级](reports/2026-11-14-dependency-upgrade.md)、[MyGo v0.10.0 升级](reports/2026-11-19-mygo-v0.10.0-upgrade.md) | 历史适配、固定依赖、测试结果及各轮未验证边界。 |
| [Windows 安装保护与单号位补扫](reports/2026-11-19-windows-install-and-rescan-acceptance.md) | 运行中拒绝覆盖或卸载、5 号位独立补扫与导出。 |
| [Windows 驱动仓库入口离线标定](reports/2026-11-22-windows-drive-storage.md) | 9×4 混排、滚轮一行、卡片/详情号位与私有样本回放；真机验收未完成。 |

## 目录约定

`docs/` 初始化时只创建本索引，其他文件和目录有实际内容时再创建；可沿用已有的合理结构。下表说明常见内容的归属，不限定全部可用路径，表中路径均相对于 `docs/`。

其他独立的项目级指导可用准确的主题名命名为 Markdown 文件，平铺在 `docs/` 下；需要组织多份相关文档时，可按主题建立子目录。

| 路径 | 内容归属 |
|---|---|
| `index.md` | 已有文档的导航与阅读场景。 |
| `specs/scanner-architecture.md` | 全局架构、模块关系、依赖边界与共享约束。 |
| `development.md` | 环境准备、开发方式、项目特有的编码约定、测试与验证方法。 |
| `release.md` | 版本管理、发布流程、发布专属检查与发布后验证。 |
| `scanner-handoff.md` | 项目背景、参考实现与功能边界。 |
| `specs/` | 专项需求、设计、必要的调研结论与取舍依据、接口合同、验收标准。 |
| `design/` | 界面设计、交互稿、视觉资产和相关实施计划。 |
| `examples/` | 由规格引用的合成示例和文件格式样本。 |
| `references/` | 供查阅的领域资料、第三方约束与资料来源，由相关规格或实现引用。 |
| `decisions/` | 需要独立保存的重要决策历史、当时的背景与取舍。 |
| `reports/` | 实机与制品测试的实测报告，以日期与主题命名；记录至少包含的内容见[实施清单第 5 节](specs/scanner-implementation-plan.md#5-验证命令与执行记录)。 |

## 内容维护

- 全局结构与共享约束由架构文档维护，具体功能的设计与行为由相关规格维护；同一项知识只维护一份正文，其他位置引用。
- 日常开发与验证方法归入 `development.md`，发布流程引用共用的方法或已有脚本，仅单独说明发布所需的步骤与检查。
- 参考资料保留事实与来源；被项目采用的设计选择和约束写入相关规格，并引用支撑资料。
- 简短的设计理由直接写在相关规格中；需要独立追溯决策时再创建 `decisions/` 中的记录，由当前规格引用。
- 较长的文档在开头给出概要或章节结构，便于按章节查阅。
- 草案与尚未确认的内容明确标注；被替代的决策指向后续决定，保留必要的历史背景。
