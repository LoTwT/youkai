# 发布与自动更新

更新日期：2026-10-17。本文维护 youkai 的版本规则、两条发布渠道、打标签与发布步骤、更新密钥管理与发布后验证（含升级实测清单）；日常构建命令见[开发指南](development.md)，交付门槛见 [T7](specs/t7-public-release-acceptance.md)，自动更新的范围与规则见[架构第 6 节](specs/scanner-architecture.md#6-首版功能范围)。发布由 `LoTwT/youkai` 的 GitHub Releases 承担，也是 MyGo 内置更新的下载源（MyGo v0.5.5，行为依据其 `docs/updates.md`、`docs/github-actions.md` 与 `internal/update`）。

## 版本号与两条渠道

- **版本号**：`major.minor.patch`（语义化），首个发布版 `0.0.1`，随发布递增。发布时以**标签中的版本为准**：`mygo.json` 源文件保持开发版本（当前 `0.0.1`），发布 workflow 在构建前把标签版本与渠道前缀写入构建检出里的 `mygo.json` 副本（不提交），因此构建产物、`mygo.App.Version()` 与标签一致，不为发布提交版本号改动。
- **测试渠道**：标签 `test-v<版本>`（如 `test-v0.0.1`）。发布为**普通 Release**——MyGo 更新器只接受已发布、非草稿、非预发布的版本——但**不设为 latest**，标题与说明注明“测试版，勿用”。测试渠道构建的应用只检查 `test-v` 标签的 Release。
- **正式渠道**：标签 `v<版本>`。正式渠道构建读仓库 **latest Release** 的更新清单（MyGo 对前缀 `v` 与不设前缀等价：`tagged()` 为假，feed 为 `releases/latest/download/`）。选显式 `tagPrefix: "v"` 是为了在 workflow 里自文档；测试渠道 Release 发布时一律 `--latest=false`，但这只是不把测试版显式标记为 latest，挡不住 GitHub 的回退：仓库中没有显式 latest 时，`releases/latest` 返回最新发布的非草稿、非预发布 Release。因此在**第一个正式版（`v*`）发布前**，唯一的测试版仍会被 GitHub 当作 latest（`test-v0.0.1` 发布后实测 `GET /repos/LoTwT/youkai/releases/latest` 返回的正是它）。目前不存在正式渠道构建的安装，latest 指向测试版不影响更新隔离；第一个正式版必须以 `--latest=true` 发布（workflow 已如此），此后 latest 恒为正式版。即使走标签列表路径（`internal/update/github.go` 的 `HasPrefix(tag, prefix)`），前缀 `v` 也匹配不到 `test-v…`，两渠道不会串。
- **渠道隔离靠 `updates.tagPrefix`**：发布 workflow 按标签写入 `test-v` 或 `v`，应用内嵌的更新源随渠道固定；同一把更新密钥签名两个渠道，公钥自 `0.0.1` 随包内置（晚加需用户手动重装一次）。
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
- `mygo build -upload` 在安装包旁产出：应用归档 `youkai-<版本>-<target>.tar.gz`、可选的 `.delta` 差量更新（默认对之前 3 个版本生成，取不到已发布档案时跳过并告警）与更新清单 `update-<target>.json`（版本、说明、日期、归档与差量的 URL/大小/签名）。清单最后上传，应用不会看到指向缺失文件的清单。
- 应用侧（`mygo.Updater`）：`Enabled()` 要求构建时嵌入了更新源与公钥、非开发运行（`mygo dev`）、且能写入安装位置（macOS 的 `.app`、Windows 的每用户安装目录）；`Check` 读清单并比较版本，`Install` 下载（优先差量、失败回退整包）、按内置公钥验签、替换应用本体，之后 `App.Relaunch()` 按正常退出序列重启（保存失败的会话会拒绝退出，窗口留在重试）。数据根与记录目录不在替换范围内，历史数据保留。
- 不参与更新的是 **Package workflow 的测试构建**：构建前移除 `mygo.json` 的 `updates`，不嵌入更新配置、不产出更新档案，识别自检断言 `updates=disabled`（见[开发指南](development.md#打包-workflow)）。开发运行（`mygo dev`）同样关闭更新。本地 `go tool mygo build` 带 `updates` 而无私钥时会嵌入更新源（产物可收到正式渠道更新）但不产出更新档案，构建日志会警告——需要完全关闭时同样先移除 `updates`。

## 发布步骤

1. 确认目标版本号已在 [CHANGELOG.md](../CHANGELOG.md) 有小节、相对上一发布版递增，且要发布的提交已合并 `main`。
2. 在合并提交上打标签并推送（由主会话执行）：
   - 正式版：`git tag v0.0.2 && git push origin v0.0.2`
   - 测试版：`git tag test-v0.0.3 && git push origin test-v0.0.3`
3. `Release` workflow（[release.yml](../.github/workflows/release.yml)）自动执行：
   1. **草稿 job**：校验标签（`test-v`/`v` + `major.minor.patch`）、CHANGELOG 小节与 `updates` 配置，创建该标签的草稿 Release（两平台 job 上传到同一草稿）。
   2. **macOS / Windows job**：写入版本与渠道前缀 → fetchdeps（Windows 另取 VC++ 运行库）→ `CGO_ENABLED=1 go tool mygo build -platform … -upload`（私钥经 `MYGO_UPDATER_PRIVATE_KEY`，`GH_TOKEN` 供上传与差量计算）→ 校验打包布局与更新档案（清单版本与归档 URL 必须指向本标签）→ 在打包产物上运行识别自检并断言 `updates=enabled`。
   3. **发布 job**（两平台都成功后）：校验 Release 资产齐全（DMG、安装包、两平台的更新归档与清单；差量为可选），下载清单核对其版本与标签一致，然后把草稿发布：测试渠道 `--draft=false --prerelease=false --latest=false` 并改标题为“Youkai <版本>（测试版，勿用）”、说明前加警示；正式渠道 `--draft=false --prerelease=false --latest=true`，标题“Youkai <版本>”。
4. 失败处理：单平台失败可用 "Re-run failed jobs" 重跑，重跑以 `--clobber` 覆盖同一草稿里的同名资产；草稿 job 幂等（Release 已存在则不重建）。标签或 CHANGELOG 问题需改标签（删远端标签重打）或补小节后重新推送标签触发。
   - `test-v0.0.1` 即由此走过一遭：发布 job 的资产校验按带空格的本地名比对而失败（GitHub 存储名空格转 `.`，见下文 Release 资产一览；重跑执行的是标签提交里的同一份校验，必然再败），草稿与两平台 job 均已成功。主会话核对两份更新清单（`version` 均为 0.0.1、归档 `url` 指向 `test-v0.0.1`）后，按 workflow 原有发布步参数手动完成了发布（`--draft=false --prerelease=false --latest=false`、标题“Youkai 0.0.1（测试版，勿用）”、说明前加警示）。该校验缺陷已修正为按存储名比对（自 `0.0.2` 起的标签包含修正），后续发布不再需要此类手动补救。
5. **首次发布需重点看**：`MYGO_UPDATER_PRIVATE_KEY` 是否就位（否则两平台在产物校验步失败，报缺更新档案）；Windows 的 NSIS 是否成功下载（缺安装包会在校验步失败）；差量计算对首版只会记 "no version is published yet"（正常，不是失败）；发布 job 的资产清单是否与下表一致；发布后 latest 是否指向正式版。

Release 资产一览（`<版本>` 为标签版本，表列为 GitHub 上的存储名）：

| 文件 | 说明 |
| --- | --- |
| `Youkai.<版本>.arm64.dmg` | macOS 安装镜像 |
| `Youkai.Setup.<版本>.amd64.exe` | Windows NSIS 每用户安装包 |
| `youkai-<版本>-darwin-arm64.tar.gz` / `youkai-<版本>-windows-amd64.tar.gz` | 更新安装用的应用归档（签名） |
| `update-darwin-arm64.json` / `update-windows-amd64.json` | 各平台更新清单（含版本、说明、签名，缓存禁用） |
| `youkai-<旧>-to-<版本>-<target>.delta` | 差量更新（可选，首版无） |

安装包的本地文件名带空格（MyGo 命名为 `Youkai <版本> arm64.dmg`、`Youkai Setup <版本> amd64.exe`），但 GitHub 上传资产时会重命名含特殊字符的文件名，每个空格存为 `.`（`test-v0.0.1` 实测：`Youkai 0.0.1 arm64.dmg` 存为 `Youkai.0.0.1.arm64.dmg`），Release 页面与下载链接中即如上表。发布 job 的资产校验因此对期望名做同样的空格转 `.` 后按存储名比对；更新归档与清单的文件名不含空格，存储名与本地一致。

## 发布后验证

- **资产核对**：上表必需项齐全；`update-*.json` 的 `version` 与标签一致、`url` 指向本标签；Windows 安装包内含 VC++ DLL（发布 job 已自动核对，抽查 release 页面即可）。
- **识别自检**：`YOUKAI_SELFTEST=1` 在安装版上通过（workflow 已在打包布局上跑过，实机再确认一次安装布局）。
- **升级实测清单**（按 T7“自动更新”交付门槛，逐项留证）：
  1. 旧版检测到新版：启动后台检查与“检查更新”入口都显示新版本号与说明（说明应来自 CHANGELOG 小节）。
  2. 安装更新后：版本号与标签一致（诊断日志首行 `version=`），历史记录与数据根保留、可重开。
  3. 签名错误被拒：篡改清单或换密钥的档案使安装失败，界面如实显示错误（如 `mygo: installing the update: the update is not signed with the app's key`），应用继续运行原版本。可用一次性测试密钥对在本地演练（见[开发指南](development.md#自动更新的本地演练)，勿用正式密钥）。
  4. 扫描进行中（含收尾与待重试保存）禁止安装：更新区按钮禁用并说明原因；会话保存完成后可安装。
  5. macOS 升级后 Gatekeeper 与权限表现：ad-hoc 签名替换后首次打开是否再次拦截、录屏/辅助功能权限是否保留（TCC 随 bundle identifier 记录，替换重签名后的表现以实测为准），结果写回本节与 T7。
- 测试渠道发布后，另用安装了测试版应用的设备确认它只收到 `test-v` 渠道的更新、正式版只收到正式渠道更新。
