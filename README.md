<h1><img src="cmd/youkai/appicon.png" alt="" width="40" height="40" align="absmiddle"> Youkai</h1>

Youkai 是《绝区零》S 级驱动盘扫描器。扫描结果在本机识别和保存，可以对照截图校对、按号位补扫，并导出为 JSON 或 CSV。

**[下载 Windows 安装包](https://github.com/LoTwT/youkai/releases)** · [第一次扫描](#第一次扫描) · [反馈问题](#反馈问题)

## 下载与支持范围

在 [GitHub Releases](https://github.com/LoTwT/youkai/releases) 选择标签为 `v<版本>` 的正式版，下载并运行 `youkai-setup-<版本>-amd64.exe`。安装包随附识别资源，可用同一发布页的 `SHA256SUMS` 核对下载文件。`test-v<版本>` 属于测试渠道，请选择正式版安装。

安装包未做 Authenticode 代码签名。Windows 可能提示发布者未知或显示 SmartScreen 警告。智能应用控制（Smart App Control）或设备策略也可能直接阻止运行。

- **系统**为 Windows x64，安装实测使用 Windows 11 x64 23H2。详见[首版验收记录](docs/specs/scanner-implementation-plan.md#m4双平台交付验收)。
- **游戏**使用《绝区零》本地客户端和简体中文界面。
- **画面**使用窗口或无边框模式，游戏客户区为 **1920×1080**。

## 第一次扫描

Youkai 通过前台截屏和鼠标操作扫描仓库，无需加载驱动、向游戏注入代码或读取游戏内存。

1. 在游戏里打开「驱动盘仓库」（Windows 安装版默认入口），按「稀有度」排序，并把游戏窗口完整放在屏幕内，避开任务栏和其他窗口。
2. 打开 Youkai 的「扫描」页，按准备检查的提示调整设置。若列出多个游戏窗口，先选定这次要扫描的窗口。若准备检查提示权限不足，请从系统托盘退出 Youkai，再右键选择「以管理员身份运行」重新打开 Youkai。
3. 点击「开始扫描」。如果游戏没有自动切到前台，请在倒计时内切过去。扫描期间保持游戏在前台，暂时停下游戏操作。需要停止时按 `Ctrl+Alt+Shift+S`，再等 Youkai 完成保存。
4. 打开结果摘要，查看覆盖结论和待处理件。可以在「记录」里重开已保存的结果，按需补录或对照截图校对。
5. 导出为 JSON 或 CSV。导出前可查看纳入和排除数，文件保存在系统下载目录。

摘要提供「补扫不完整号位」时，可以只补扫选中的号位。补扫会保存为独立记录，完成的号位可以合并回原记录。详见[校对、补扫与导出](docs/specs/t6-review-export-workflow.md)。

## 反馈问题

请在 [GitHub Issues](https://github.com/LoTwT/youkai/issues) 提交问题，附上游戏客户区尺寸、窗口模式和复现步骤。在「设置」的「诊断」区点击「复制诊断信息」，可一并附上应用版本、系统版本和最近一次扫描的触发码。诊断信息不含外部窗口标题或用户主目录路径。应用无法打开时，请手动注明 Youkai 版本和 Windows 版本。

## 开发与许可

Youkai 使用 Go 和 MyGo。环境配置、运行和测试见[开发指南](docs/development.md)，其他规格和验收记录见[文档索引](docs/index.md)。

项目采用 [MIT 许可证](LICENSE)。随包依赖、模型和其他资源的许可见[第三方声明](resources/THIRD-PARTY-NOTICES.md)。
