# 文档索引

## 文档导航

- [MyGo + Go 扫描器架构](specs/scanner-architecture.md)：公共架构与首版范围的权威来源，包括模块边界、应用与会话契约、隐藏恢复退出、结果与存储、字段校验范围、验收与成本。
- [扫描器首版实施清单](specs/scanner-implementation-plan.md)：四个内部里程碑、代码落点、Mac 开发与 Windows 测试方式、需尽早取得的证据和验证命令；尚未开始实施。
- [T1：MyGo 界面与构建分发](specs/t1-mygo-ui-distribution.md)：已确认的产品选择、MyGo v0.5.5 选型、资源与安装、浏览器唤起依据和界面验收。
- [T2：平台采集与输入](specs/t2-platform-capture-input.md)：三种客户端范围、Windows GDI/SendInput 与 macOS ScreenCaptureKit/Quartz 方案、坐标单位、显示配置候选、帧时序与平台故障。
- [T3：OCR 运行库、模型与样本](specs/t3-ocr-runtime-model-samples.md)：固定的绑定、运行库与模型组合及制品核查、预处理与解码基准、三组样本与识别质量门槛。
- T4：扫描会话与结果契约：直接在[架构第 5 节](specs/scanner-architecture.md#5-扫描会话与结果语义)维护。
- [T5：绝区零仓库遍历与详情归属](specs/t5-zzz-inventory-traversal.md)：页面预检、选中与详情归属、滚动衔接、末尾判断和完整性验收。
- [T6：用户操作、结果校对与导出](specs/t6-review-export-workflow.md)：准备到导出的操作流程、逐件校对、两种导出用途、历史与存储管理。
- [扫描结果 JSON v1](specs/scan-json-format.md)：自有文件契约，附[合成示例](examples/scan-usable-v1.json)；尚未实现。
- [T7：构建打包与交付验收](specs/t7-public-release-acceptance.md)：首版交付门槛、无付费证书的打包路线、最低系统候选。
- [扫描器背景与参考实现](scanner-handoff.md)：项目起因、参考扫描器研究结论和与 fairy 的边界。
- [开发指南](development.md)：环境要求与当前实际可用的构建、测试、MyGo 开发命令。
- [M1 Windows 实机测试报告](reports/2026-10-17-m1-windows-test.md)：首份实测报告——Package workflow 安装包在 Windows 11 实机的下载核对、安装、识别自检、启动、第二实例、数据目录与卸载结果，含未覆盖项与观察项。
- [M1 macOS 实机测试报告](reports/2026-10-17-m1-macos-test.md)：DMG 安装版在 macOS（Apple Silicon）实机的下载核对、安装、Gatekeeper 首次打开与放行、识别自检、启动、第二实例、数据目录与移除结果，含未覆盖项与观察项。

## 目录约定

`docs/` 初始化时只创建本索引，其他文件和目录有实际内容时再创建；可沿用已有的合理结构。下表说明常见内容的归属，不限定全部可用路径，表中路径均相对于 `docs/`。

其他独立的项目级指导可用准确的主题名命名为 Markdown 文件，平铺在 `docs/` 下；需要组织多份相关文档时，可按主题建立子目录。

| 路径 | 内容归属 |
|---|---|
| `index.md` | 已有文档的导航与阅读场景。 |
| `architecture.md` | 全局架构、模块关系、依赖边界与共享约束。 |
| `development.md` | 环境准备、开发方式、项目特有的编码约定、测试与验证方法。 |
| `release.md` | 版本管理、发布流程、发布专属检查与发布后验证。 |
| `specs/` | 专项需求、设计、必要的调研结论与取舍依据、接口合同、验收标准。 |
| `references/` | 供查阅的领域资料、第三方约束与资料来源，由相关规格或实现引用。 |
| `decisions/` | 需要独立保存的重要决策历史、当时的背景与取舍。 |
| `reports/` | 实机与制品测试的实测报告，以日期与主题命名；记录至少包含的内容见[实施清单第 5 节](specs/scanner-implementation-plan.md#5-验证命令与执行记录)。 |

## 内容维护

- 全局结构与共享约束由架构文档维护，具体功能的设计与行为由相关规格维护；同一项知识只维护一份正文，其他位置引用。
- 日常开发与验证方法归入 `development.md`，发布流程引用共用的方法或已有脚本，仅单独说明发布所需的步骤与检查。
- 参考资料保留事实与来源；被项目采用的设计选择和约束写入相关规格，并引用支撑资料。
- 简短的设计理由直接写在相关规格中；需要独立追溯决策时再创建 `decisions/` 中的记录，由当前规格引用。
- 草案与尚未确认的内容明确标注；被替代的决策指向后续决定，保留必要的历史背景。
