# 装备者圆形头像资源与匹配

本目录把 fairy 的 60 个代理人目录中 `partnerInfo.iconPath` 指向的 57 张
`IconRoleCircle` 圆形头像作为离线资源嵌入（`assets/*.webp`，共约
532 KB），供详情页等级右侧头像与列表格右上角头像的装备者身份匹配使用。
资源在构建时通过 `go:embed` 打包，运行时不联网。

## 来源与缺失

- 来源：fairy `packages/data/integrated/agents/<id>/data.json` 的
  `partnerInfo.iconPath`，按 fairy/nanoka 规则取文件名、扩展名改
  `.webp`，从 `https://static.nanoka.cc/assets/zzz/` 取得。
- 57 个可用头像；`1381`（零号·安比）、`1531`（星徽·比利）、
  `1551`（佩洛伊斯）在 fairy 中没有 `iconPath`，catalog 中 `Avatar`
  留空，匹配器对它们只能返回 unknown。
- `1581`（蕾米埃尔）与 `1621`（洛克茜）的上游文件字节完全相同；
  匹配器检测到相同归一化图像时明确返回 unknown，不按 ID 顺序猜一个。

## 归一化与判定

1. 详情头像和列表头像各自由 `collect` 按 `Layout.DetailAvatar` /
   `Cell.Avatar` 裁出，经 `Observation.Context` 交给 app，并作为证据
   PNG 写入记录（不进入导出 JSON）。
2. 匹配器在裁剪块内按亮度阈值找出圆形头像的方形包围盒（阈值取裁剪
   边缘中位亮度 + 30，限制在 25–90），缩放到 32×32；比较半径为 13
   的圆形区域，避开游戏外圈和资源透明角，并在 ±1 像素内搜索最佳对齐。
3. 分数为灰度 NCC 与 R/G/B 三通道 NCC 的平均；详情与列表分别匹配，
   取两者平均分排序。
4. 仅当以下全部成立才返回 fairy 数值 ID：
   - 平均分 ≥ 0.90；
   - 详情分和列表分各自 ≥ 0.88；
   - 详情、列表各自的第一名与平均分第一名一致；
   - 第一名与第二名平均分差 ≥ 0.08；
   - 第一名不是已知的相同头像对（1581/1621）。
   任一不成立即 `agentId=null`，证据字符串记录最高候选、分数、次优与
   间隔。

## 真实帧可行性结论

唯一的独立真值帧是 `s2-70-unlocked-equipped`（报告记录该盘由仪玄
1371 装备；源帧 SHA-256
`37507eed8f92e5587a2886f89a2beee763a8aa566f2ade21e02761de96a0d64f`）。
在完整源帧上：

- 全部 57 个资源中最高候选为 1151（露西），平均分 0.457
  （详情 0.332、列表 0.581）；次优 1181（格莉丝）0.444，间隔 0.013。
- 已知真值 1371（仪玄）平均分仅 0.120（详情 0.219、列表 0.022）。

因此**真实帧与 fairy 圆形头像资源的差异过大，无法可靠区分代理人**。
本 PR 保留 `agentId=null`，不按最高候选硬填；真实帧回归
（`TestRealFrameIdentityStaysUnknown`）固定这一结论。匹配框架、资源
嵌入、像素传递和数据填充规则已就绪，待获得与实拍一致的头像资源后再
启用真实身份填充。

## 测试

- `TestEmbeddedAvatarCatalog`：57 个文件可解码、尺寸 142×142、与
  catalog 的 `Avatar` 一一对应；3 个缺失 ID 为空。
- `TestSyntheticMatchesEveryAvailableAgent`：每个资源按真实裁剪几何
  缩放并加确定性 ±3 噪声后，匹配器返回其 ID（最低分 0.979）；
  1581/1621 这对相同资源必须返回 unknown。
- `TestSyntheticAmbiguityStaysUnknown`：混合两个头像或纯色圆盘时
  不返回 ID。
- `TestRealFrameIdentityStaysUnknown`：使用本目录 `testdata/` 下从
  `s2-70-unlocked-equipped/frame-0001.png` 裁出的详情（800,250）–
  （844,294）与列表（139,193）–（171,225）头像，断言
  `AgentID == 0` 且证据记录分数。两张裁剪只包含头像像素，不含账号
  或整帧信息。
