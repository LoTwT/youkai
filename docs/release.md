# 发布与自动更新

更新日期：2026-11-21。本文维护标签、更新渠道、密钥、发布步骤和发布后检查。日常构建见[开发指南](development.md)，交付条件见 [T7](specs/t7-public-release-acceptance.md)，实测结果见 [M4](specs/scanner-implementation-plan.md#m4双平台交付验收)。

首版 Release 只发布 Windows x64。macOS 继续使用开发和 Package 构建。平台范围见[架构 §6](specs/scanner-architecture.md#6-首版功能范围)。

[test-v0.0.5](https://github.com/LoTwT/youkai/releases/tag/test-v0.0.5) 已按该范围发布，只有 Windows 更新清单。MyGo v0.10.0 的 [`latestTagged`](https://github.com/egoist/mygo/blob/ebde9399baf2734b3361856a53f4036859973d1d/internal/update/github.go) 按标签选择 Release，选择时不区分平台。此前安装的 macOS 测试版检查这一发布时可能遇到更新清单 404。该行为是从源码得出的判断，未在 Mac 实测。

## 版本号与两条渠道

- **版本号**。各渠道内按 `major.minor.patch` 递增。最早的测试发布是 `test-v0.0.1`，首个正式版是 `v0.0.1`。发布以标签中的版本为准，workflow 只修改运行检出中的 `mygo.json`。构建中间文件放在 `RUNNER_TEMP`，避免给 Go 的 VCS 戳记加上 `-modified`。戳记核对见[开发指南](development.md#诊断日志)。
- **测试渠道**：标签 `test-v<版本>`（如 `test-v0.0.1`）。发布为**普通 Release**（本项目未调用 `SetPrereleases(true)`，更新器默认只接受已发布、非草稿、非预发布的版本），但**不设为 latest**，标题与说明注明“测试版，勿用”。测试渠道构建的应用只检查 `test-v` 标签的 Release。
- **正式渠道**。标签 `v<版本>`。配置 `updates.tagPrefix="v"` 的 MyGo 构建从 `releases/latest/download/` 读更新清单。首个正式版发布前，GitHub 的回退仍可能把普通测试 Release 作为 latest，`test-v0.0.5` 已观测到该情况。首个正式发布应显式设 `make_latest=true`。之后较旧的正式草稿不能覆盖较新的正式 latest。读取失败或状态不一致时 workflow 停在草稿。发布后检查见下节。
- **渠道配置**。workflow 将 `updates.tagPrefix` 固定为标签的 `test-v` 或 `v`。两条渠道使用同一密钥，更新公钥自 `0.0.1` 随包内置。
- **更新说明**：每个发布版本在 [CHANGELOG.md](../CHANGELOG.md) 有 `## <版本号>` 小节（两渠道共用同一版本号的小节），小节文字进入该版本的更新清单（`update-<target>.json` 的 `notes`），即应用内“发现新版本”时显示的说明；可用简短中文。**缺失对应小节时发布构建失败**，草稿 job 也会在构建前先检查。

## 更新配置与密钥

`mygo.json` 的 `updates`（源文件只含公钥与仓库，渠道前缀由发布 workflow 写入）：

```json
"updates": {
  "publicKey": "1NEJ3nMW2Y8VP1uPVJSj1LHkPsxQlDnFVA2H+4X7Ii0=",
  "github": "LoTwT/youkai"
}
```

- 公钥与私钥为 Ed25519 对（`go tool mygo keygen` 生成，私钥文件为 `mygo-update.key` 的内容）。**私钥只存于用户的密码管理器与仓库 secret `MYGO_UPDATER_PRIVATE_KEY`，绝不进仓库**；丢失私钥会使已安装应用无法再收到更新（搁浅），泄露则可向用户推送任意代码。
- 发布构建经 `MYGO_UPDATER_PRIVATE_KEY` 签名；私钥与公钥不匹配时 `mygo build` 直接失败，缺失时跳过更新档案并告警——发布 workflow 的产物校验会因缺少 `youkai-<版本>-<target>.tar.gz` / `update-<target>.json` 而失败，不会发出一个“没有更新的发布版”。
- `mygo build` 在安装包旁产出：应用归档 `youkai-<版本>-<target>.tar.gz`、可选的 `.delta` 差量更新（默认对之前 3 个版本生成，取不到已发布档案时跳过并告警）与更新清单 `update-<target>.json`（版本、说明、日期、归档与差量的 URL/大小/签名）。Release workflow 在全部本地校验和识别自检成功后另行上传，清单最后上传。不能在构建时使用 `-upload`，否则校验失败前制品已对外上传。
- 应用侧（`mygo.Updater`）：`Enabled()` 要求构建时嵌入了更新源与公钥、非开发运行（`mygo dev`）、且能写入安装位置（macOS 的 `.app`、Windows 的每用户安装目录）；`Check` 读清单并比较版本，`Install` 下载（优先差量、失败回退整包）、按内置公钥验签、替换应用本体。安装成功后应用走明确退出收尾（保存失败的会话会拒绝退出，窗口留在重试），**收尾完成后自动重启进入新版本**——MyGo 的 `App.Relaunch` 在退出被拒（如收尾未完成）时会自行取消，重启由应用在收尾完成后重新发起；数据根占用的释放跟随退出路径——收尾清理成功时随收尾先行释放，清理失败时保持到重启前一刻，由退出钩子在 MyGo 保存窗口状态之后、启动新进程之前释放，两条路径下新进程都能立即接管。检查时机：启动后一次、应用持续运行期间每 24 小时一次（`internal/ui` 的 `updateCheckInterval` 常量）、更新区按钮手动触发；检查与安装都不激活窗口。Windows“应用和功能”的版本显示（HKCU 卸载项 `DisplayVersion`）由应用启动时刷新到当前版本——NSIS 安装器只在安装当天写一次，MyGo 更新器只替换文件不写注册表；NSIS 卸载器删除整个键、从不读该值，刷新不影响卸载。数据根与记录目录不在替换范围内，历史数据保留。
- 不参与更新的是 **Package workflow 的测试构建**：构建前移除 `mygo.json` 的 `updates`，不嵌入更新配置、不产出更新档案，识别自检断言 `updates=disabled`（见[开发指南](development.md#打包-workflow)）。开发运行（`mygo dev`）同样关闭更新。本地 `go tool mygo build` 带 `updates` 而无私钥时会嵌入更新源（产物可收到正式渠道更新）但不产出更新档案，构建日志会警告——需要完全关闭时同样先移除 `updates`。

## 发布步骤

1. 确认版本相对同一渠道的上一发布版递增，[CHANGELOG.md](../CHANGELOG.md) 有对应小节，并核对已合并到 `main` 的目标提交。
2. 在已核对的提交上打标签并推送。下列 `<版本>` 与 `<提交>` 需替换为本次获准发布的值。
   - 正式版用 `git tag v<版本> <提交>`，再执行 `git push origin v<版本>`。
   - 测试版用 `git tag test-v<版本> <提交>`，再执行 `git push origin test-v<版本>`。
3. `Release` workflow（[release.yml](../.github/workflows/release.yml)）自动执行：
   1. **草稿**。校验标签、对应的 CHANGELOG 小节和更新配置。查询 Release 列表后创建草稿或复用同标签的唯一草稿。列表无法完整读取、格式不合规或同标签状态无法确定时停止。
   2. **构建**。写入版本和渠道，下载固定依赖，随包放入 VC++ DLL，生成安装器、签名更新归档和差量。校验布局、摘要以及指向本标签的更新清单，并运行 OCR 自检断言 `updates=enabled`。再次确认仍是草稿后上传制品，最后上传更新清单。
   3. **发布**。按草稿的 Release ID 校验必需资产，按资产 ID 下载并验证更新清单，生成可公开访问的下载区块，保留原更新说明。发布前重新确认同一 ID 仍为草稿。测试渠道使用 `make_latest=false`。正式渠道在完整校验当前 latest 与已发布版本后选择 `make_latest`。状态解析见 [release-state.sh](../scripts/release-state.sh)，下载区块见 [generate-release-downloads.mjs](../scripts/generate-release-downloads.mjs)。
4. 仅对草稿使用 **Re-run failed jobs**。它沿用原运行的 SHA/ref 和 workflow 定义，允许覆盖同名草稿资产。已发布版本需要修正时发新版本。标签或变更日志有误时，先修正提交与尚未发布的标签，再触发新运行。相同标签的运行串行，正式发布的 latest 判断与写入共享一个队列。自动运行期间避免手动改同一草稿。
   - 早期 `test-v0.0.1` 曾因安装器存储名与本地名不同需补发布。修正和当时的发布证据见 [M1](specs/scanner-implementation-plan.md#m1正式工程资源与会话底座)。
5. 首次正式发布前确认签名密钥已配置并核对渠道和标签。构建缺安装器或更新档案时产物校验会失败。没有可用的历史归档时可以无差量。发布后按下节核对渠道指向。

Release 资产一览（`<版本>` 为标签版本，表列为 GitHub 上的存储名）：

| 文件 | 说明 |
| --- | --- |
| `youkai-setup-<版本>-amd64.exe` | Windows NSIS 每用户安装包 |
| `youkai-<版本>-windows-amd64.tar.gz` | 更新安装用的应用归档（签名） |
| `update-windows-amd64.json` | 更新清单（含版本、说明、签名，缓存禁用） |
| `install.ps1` / `SHA256SUMS` | 辅助安装脚本和摘要。Windows workflow 校验后上传。 |
| `youkai-<旧>-to-<版本>-<target>.delta` | 使用已发布归档生成的可选差量。 |

`test-v0.0.5` 已发布上表的 Windows 资产，包括三个差量，验签结果见 [M4](specs/scanner-implementation-plan.md#windows-签名测试发布)。较早的 `test-v0.0.1` 使用旧安装器文件名 `Youkai.Setup.0.0.1.amd64.exe`。

Windows 安装保护与模板校验见 [T1](specs/t1-mygo-ui-distribution.md)。构建中修改 NSIS 或安装脚本时必须按适配流程重新核对最终制品摘要和签名更新资产，详见[模板适配记录](reports/2026-11-14-dependency-upgrade.md#r3-核心范围收敛与最小验证)。

## 发布后验证

- **资产核对**。公开页面含上表必需资产。Windows 清单的 `version` 与标签一致，归档 URL 指向本标签。确认摘要、随包 VC++ DLL 和构建自检记录。
- **识别自检**。`YOUKAI_SELFTEST=1` 在安装布局下能读取随包固定图。首版安装版断网检查的免验决定见 [M4](specs/scanner-implementation-plan.md#windows-安装验收记录)。
- **升级实测清单**。按 [T7](specs/t7-public-release-acceptance.md#2-首版交付门槛)留证。历史双平台结果见 [M1 报告](reports/2026-10-18-m1-auto-update-test.md)，当前 Windows 测试渠道见 [M4](specs/scanner-implementation-plan.md#windows-签名测试发布)。
  1. 从旧版启动或手动检查，确认显示新版本及 CHANGELOG 中的更新说明。
  2. 安装后确认自动重启到新版本，界面、诊断日志的 `version=` 与 Windows 卸载项版本一致。确认升级前可读的历史仍可重开。
  3. 用一次性密钥[本地演练](development.md#自动更新的本地演练)验证坏签名被拒并能继续使用原版本。
  4. 验证扫描、收尾或待重试保存期间禁止安装，保存完成后恢复可用。
  5. 核对 `update check`、下载验签、安装和重启日志。差量回退整包时分别记录下载量。macOS 恢复发布时再核对升级后的 Gatekeeper 和录屏、辅助功能权限。
- **渠道核对**。首个正式版发布后，确认仓库 `latest` 的标签和 Windows 更新清单都指向该正式版，并确认测试客户端仍只读 `test-v` 渠道。待有下一正式版本时，用已安装的正式版验证同渠道升级。
