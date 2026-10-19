# 像素匹配器掩码夹具（macOS 云客户端）

本目录是 [PR 4b-1](../../../../../docs/specs/t5-zzz-inventory-traversal.md)
像素匹配器的回归夹具：从 2026-10-19 M2 样本采集轮的 1920×1080
rawstream 帧中裁出匹配器声明读取的区域，其余像素涂成不透明黑色。
完整帧可能含账号信息与无关界面，留在采集现场 `/tmp/m2-samples/`，
不进仓库；本目录只保留判定所需的像素。

## 来源与溯源

`manifest.tsv` 每行是一件夹具：

| 列 | 含义 |
| --- | --- |
| `name` | 夹具文件名（不含 `.png`） |
| `source` | 采集目录内的 `frame-0001.png` 相对路径 |
| `sha256` | 完整源帧的 SHA-256（与识别样本 `lines.tsv` 的 27 件同源） |
| `mode` | `item`（不保留整块列表视口）或 `scroll`（保留列表视口供位移匹配） |
| `expect` | 回归测试断言的页面/选中/转盘/筛选/顶底/品质/条数等 |

`tools/zzzframemask` 先核对源帧 SHA-256 再生成，源目录漂移会直接失败，
不会静默产出不同夹具。27 件精选详情样本的来源与状态见
[识别样本 README](../../../../../recognition/testdata/macos-cloud/README.md)。

## 掩码与压缩口径

- 只复制 `Layout.ReadRegions` 声明的区域（格位选中边框与占用条、格位
  右下管理角标、格位右上头像、列表视口、滚动条、漏斗、面板探针、转盘弧段、
  七行详情、S 徽章、详情等级右侧头像、顶部“拆解”左侧指示、第四副词条
  探针）；区域外为 `(0,0,0,255)`。
- `item` 夹具不保留整块列表视口（格位探针仍逐个保留），把 PNG 压到
  约 40–80KB；`scroll` 夹具保留视口，约 220–240KB。
- 量化：详情行、滚动视口、详情头像与顶部指示按 16 级，格位管理与头像
  角标按 32 级（判定只依赖形状与粗色阶），其余区域按 16 级向下取整；
  选中边框里符合选中色的像素保持原值（`s2-s6-top-cell-r4c1` 的边框只有
  60 个选中像素，整体量化会掉到阈值以下）。徽章、漏斗、面板探针与第四
  副词条探针保持原值。
- 量化后的判定已用夹具测试与实测回放双重核对：同内容/不同内容的
  MAD、整行位移分类以及管理状态/头像存在性均不变。
- 当前总体积约 6.1MiB（70 件；新增格位管理与头像探针后较此前增加）。

## 重新生成

```sh
YOUKAI_M2_SAMPLES=/tmp/m2-samples \
  go run ./tools/zzzframemask -src /tmp/m2-samples
```

工具默认写入本目录，读取同目录的 `sources.tsv`（名称、来源目录、
源帧 SHA-256 与期望）；`-step`、`-detail-step`、`-viewport-step`、
`-state-step` 可调量化级别（默认 16/16/16/32），`-out` 可改输出目录。
`sources.tsv` 与 `manifest.tsv` 的列相同，前者是输入事实，后者由工具
带生成标记写出。

## 回归测试

- `TestMacCloudFixturesAgreeWithRecordedExpectations`：逐件断言页面、
  选中、转盘、筛选、顶底、品质与副词条数。
- `TestMacCloudManagementAndAvatarVerdicts`：在掩码夹具上断言锁定/弃置/
  无标记、顶部指示以及亮/暗/缺失头像。
- `TestMacCloudScrollDisplacements`：8 组记录帧对的
  Stationary/Displaced/Lost 与整行位移数。
- `TestMacCloudDetailComparison`：同内容、不同内容、近重复与
  中间带（未决）四类比较。
- `TestPixelMatcherReadsOnlyDeclaredRegions`：随机化所有声明区域外的
  像素，要求 Match / MatchScroll / CompareDetail 判定完全不变。
- `TestReplayFieldSamples`、`TestReplayViewportFromFieldFrames` 与
  `TestReplayManagementAndAvatarSamples`：设置 `YOUKAI_M2_SAMPLES` 后在
  完整未掩码源帧上重放；样本目录缺失时自动跳过。它们证明掩码没有改变
  判定，不替代真实客户端验收。
