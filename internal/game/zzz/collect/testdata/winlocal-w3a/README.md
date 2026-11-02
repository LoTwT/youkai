# W3a Windows 本地客户端样本裁剪夹具

本目录是 [W3a Windows 本地客户端样本轮](../../../../../../docs/reports/2026-11-02-windows-local-sample-round.md)
的回归夹具：只保留判定所需的**小裁剪**，不提交 1920×1080 完整帧。

## 来源与溯源

源证据目录（只读，Mac 副本）：

```text
~/Library/Logs/Youkai Dev/probes/20261102-w3a/
  SHA256SUMS
  S1/  # 长列表拖拽，约 28 件、约 1.4 屏，1 fps
  S2/  # 筛选面板打开，排序“稀有度”，第 0/5/9 帧
  S3/  # 面板关闭，鼠标停在列表 R2C3
```

`manifest.tsv` 每行是一件夹具：

| 列 | 含义 |
| --- | --- |
| `name` | 夹具文件名（不含 `.png`） |
| `source` | 证据目录内的源帧相对路径 |
| `sha256` | 完整源帧的 SHA-256（`SHA256SUMS` 中同一值） |
| `region` | 源帧内的裁剪区域 `x0,y0,x1,y1`（右、下开区间） |
| `expect` | 回归测试断言：排序匹配、面板开/关或滑块端点 |

裁剪保持源帧坐标作为图像边界，生产匹配器可直接按绝对坐标读取，不需要
在仓库中保留整帧。夹具像素未量化、未缩放，与源帧逐像素一致。

## 夹具分组

- `sort-s2-0000/0005/0009`：S2 第 0/5/9 帧的排序区域
  `(1285,124)-(1467,201)`（182×77）；三帧在面板区域 x≥1230 完全一致。
  该区域也是 `assets/sort-label-rarity-winlocal.png` 的来源。
- `sort-s3-0000`：S3 面板关闭帧的同一区域，用于断言不匹配。
- `panel-open-1/2` 与 `panel-closed-1/2`：S2/S3 帧上面板两个 1×1 颜色探针
  `(1781,39)` 与 `(1290,65)`。
- `thumb-top/bottom/mid`：S1 的 x=55 滚动条列 `(55,145)-(56,941)`，分别来自
  顶部帧 `frame-0120`、底部帧 `frame-0899` 和拖动中帧 `frame-0133`。

## 回归测试

`winlocal_w3a_internal_test.go`：

- `TestWindowsLocalW3ASortSamples`：S2 三帧匹配、S3 同区域不匹配，并记录 MAD。
- `TestWindowsLocalW3APanelProbes`：两个面板探针在 S2 为 open、S3 为 closed。
- `TestWindowsLocalW3AScrollbarEnds`：顶部/底部/拖动中帧的滑块起止与两端判定。
- `TestWindowsLocalW3ASortVerifiesCoverage`：有模板时 `sort=verified`，六个完整
  位置后 `coverage=complete`。
- `TestWindowsLocalW3AScaledTierDropsSortTemplate`：缩放档继续丢弃排序声明。
- `TestWindowsLocalW3AManifestProvenance`：来源、SHA-256 与裁剪范围。
