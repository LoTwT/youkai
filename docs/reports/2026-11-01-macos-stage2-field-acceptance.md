# 2026-11-01 macOS 阶段 2 真机全量验收

> 本报告是这次验收数据的唯一权威来源；T5、决策与索引只引用结论。验收对象是阶段 2 滚动判定（`ScrollEvidenceMatcher`）在 macOS 云客户端的独立完整扫描。

## 结论

- 在 `9a151dd` 的 dev 构建上，macOS 云客户端完成一次全量扫描：1,017 件，六个位置全部 `complete`，`coverage=complete`，排序验证为“稀有度”，`capturedCount=1017`，0 件未处理。
- 59 次滚动全部 `displaced`：56 次 `selection+feature`，3 次 `selection+feature+scrollbar`；0 undecided、0 lost、0 次滚动重试；59/59 有 manifest，每次滚动都有 2 次停稳确认。
- 本次满足[决策 §4.3](../decisions/2026-10-28-scanner-rearchitecture.md)与[§6](../decisions/2026-10-28-scanner-rearchitecture.md#6-阶段路线)对阶段 2“另采独立完整扫描验收”的要求（macOS 云客户端）。第一次尝试在目标解析阶段因窗口几何失败，未向游戏发送任何输入；调整窗口后第二次尝试通过。
- 这不是 [T5 §6](../specs/t5-zzz-inventory-traversal.md#6-保存什么证据怎样验收)的三客户端、每客户端至少三次人工基准验收；Windows 未测，导出与截图未执行，位置级降级路径未在真机触发。

## 环境与构建

| 项目 | 值 |
| --- | --- |
| 系统 | macOS 27.0.1（26A434），Apple M1 Pro |
| 显示 | 主显示器 3024 × 1964 Retina（1512 × 982 点）；可见区 (56, 0, 1456, 949) |
| 游戏窗口 | 《云·绝区零》macOS 云客户端，`com.miHoYo.cloudgames.Nap`；1164 × 762 @ (309, 96)，完整落在主显示器内 |
| 游戏版本 | 3.3.0（操作者提供；证据目录未保存版本读数） |
| 构建分支 | `field/stage2-acceptance-20261101`（基于 `main` `9a151dd`，即 #101），HEAD `9a151dd`，dirty tracked files 0 |
| 应用 | Youkai Dev 0.0.1，commit `9a151dd02fcb`，darwin/arm64，dev bundle，`CGO_ENABLED=1`，ad-hoc 签名（`com.lotwt.youkai.dev`，无 TeamIdentifier），`vcs.modified=false` |
| 主程序 SHA-256 | `f37431ee…`（完整值见 `attempt2/preflight.txt`） |
| 启动 | 直接运行 dev bundle；`YOUKAI_GAME_SOURCE=traverse`、`YOUKAI_DEV_AUTOSTART=1`；`YOUKAI_SCROLL_MATCHER` 未设置，默认阶段 2 |
| 数据目录 | `~/Library/Application Support/Youkai Dev` |
| 会话 | `76bc33aa-4e2e-4b5d-951b-021eadaa9c1a` |

扫描开始时观察到的转盘位置为 2（`observedScope.startPosition=2`）；该字段只是准备前的观测元数据，遍历仍按位置 1→6 执行。扫描期间保持游戏窗口在前台，未移动鼠标或切换应用；人工预期是六个位置全量完成、覆盖结论可用。

## 步骤

1. 开扫前先把旧日志与 diagnostics 复制到 `pre-scan-backup/`（275 个文件，SHA-256 全部通过）；预检记录构建、git、窗口几何与日志状态。
2. **第一次尝试（10:25）：** 10:25:45 启动，10:25:47 接受会话 `ffedad34-1889-4088-9593-25c99b2f41a8`，10:25:47.960 以 `targetNotFound` 停止；0 captured / 0 produced，检查点 generation 1 为 0 件，10:26:32 `exit wrap-up complete`。
3. **失败原因与处理：** 游戏窗口为 1458 × 835 @ (110, 43)，右边界 1568 pt；主显示器可见区右边界 1512 pt，超出 56 pt。扫描器要求游戏窗口完整落在一块显示器内，因此在目标解析阶段拒绝；日志没有 `collect:` 行，说明未进入任何采集或输入。把窗口调整为 1164 × 762 @ (309, 96) 后，右边界 1473、下边界 858，均落在主显示器可见区内。
4. **第二次尝试（10:48）：** 10:48:01 启动，10:48:02.721 接受会话 `76bc33aa-4e2e-4b5d-951b-021eadaa9c1a`；默认阶段 2 判定；六个位置依次准备、遍历；11:03:10.532 以 `completed / verifiedEnd / capturing` 结束；11:03:26.609 `exit wrap-up complete`。接收至结束共 14 分 08 秒。

## 结果

### 1. 全量扫描

| 指标 | 结果 |
| --- | --- |
| 采集 / 产出 / 未处理 | 1,017 / 1,017 / 0 |
| 终止 | `completed / verifiedEnd / capturing` |
| 覆盖 | `complete`（`observedScope.coverage=complete`） |
| 排序 | `verified`，`sortLabel=稀有度`；方向未读取 |
| 位置 | 6/6 `complete`，`topVerified`/`bottomVerified` 均 true |
| 检查点 | generation 130（1,017 件）；目录保留 129/130 |
| 证据 | 10,937 个文件，约 74 MB |
| 物品状态 | 1,017 `produced`，1,017 `autoAccepted` |
| 详情归属 | 1,002 `transitionVerified`、9 `referenceVerified`、6 `initialVerified`；0 `detailUnattributed` / `detailUnresolved` |
| 位置级降级 | 0 个不完整位置；无 `positionFailureLimit` |
| 后续故障 | 0 |
| 平台收尾 | `captureStopped=true drained=true released=true hotkeyErr=<nil>` |

### 2. 位置账目

| 位置 | 视口 | 访问 = 接纳 | 滚动 | 末次滚动 | 顶部 | 底部 | 完整 |
| --- | ---: | ---: | ---: | ---: | --- | --- | --- |
| 1 | 6 | 99 | 5 | 4 行 | 是 | 是 | 是 |
| 2 | 7 | 112 | 6 | 3 行 | 是 | 是 | 是 |
| 3 | 7 | 106 | 6 | 2 行 | 是 | 是 | 是 |
| 4 | 15 | 230 | 14 | 1 行 | 是 | 是 | 是 |
| 5 | 16 | 249 | 15 | 2 行 | 是 | 是 | 是 |
| 6 | 14 | 221 | 13 | 3 行 | 是 | 是 | 是 |
| 合计 | 65 | 1,017 | 59 | — | 6/6 | 6/6 | 6/6 |

六个位置都记录 `S filter active, top confirmed`。顶部确认每个位置首次尝试即接受（`the list stayed still`，`top attempts=1`，`top_ms` 5.31–5.34 s），没有第二次 5 秒等待。位置 1 的末次滚动仍为 4 行，另行做了 1 次底部确认（`bottom_ms=6461`）；其余位置的末次滚动为 1–3 行短步，没有另做底部确认（`bottom_ms=0`）；六位置 `bottomVerified` 均为 true。

本次总数 1,017 件；与 2026-10-26 的 1,007 件不是同一时点库存，不做差异结论。

### 3. 滚动判定

59 次成功滚动在 attempt2 日志中产生 118 行 `collect: scroll position=`（每次尝试一行、汇总一行）；按汇总行统计：

- 59 次全部 `displaced`，0 `undecided`，0 `lost`。
- `attempts=1` 共 59 次，滚动重试 0；物品 `retries=0` 共 1,017 次。
- basis：`selection+feature` 56 次，`selection+feature+scrollbar` 3 次。
- 59/59 的 manifest `settle_observations=2`，attempt2 日志 `settle_confirm=none` 为 0。

| 位置 | 滚动 | `selection+feature` | `selection+feature+scrollbar` |
| --- | ---: | ---: | ---: |
| 1 | 5 | 5 | 0 |
| 2 | 6 | 6 | 0 |
| 3 | 6 | 6 | 0 |
| 4 | 14 | 14 | 0 |
| 5 | 15 | 14 | 1 |
| 6 | 13 | 11 | 2 |

三次滑块佐证的明细：

| 位置 | 视口 | 前后帧 | 行数 | NCC 差距 |
| --- | --- | --- | ---: | ---: |
| 5 | vp0 → 1 | 29974 → 30059 | 4 | 0.039 |
| 6 | vp0 → 1 | 42335 → 42420 | 4 | 0.038 |
| 6 | vp8 → 9 | 48368 → 48453 | 4 | 0.022 |

位置 5 与位置 6 的 vp0 → 1 正是 2026-10-31 判 undecided 的首屏首滚（当次帧号不同）；本次在滑块佐证下判 `displaced`，判定细则见 [T5 §5](../specs/t5-zzz-inventory-traversal.md#5-滚动衔接重复物品与末尾) 的“2026-10-31 阶段 2 验收与修补”。

每次前进的行数分布：54 次 4 行、2 次 3 行、2 次 2 行、1 次 1 行。末次短步与底部确认的对应关系见位置账目。

### 4. 时序

| 指标 | 中位数 | p90 | 最大 |
| --- | ---: | ---: | ---: |
| `motion_ms` | 6 | 16 | 255 |
| `settle_ms` | 48 | 58 | 366 |
| `drag_ms` | 1390 | 1399 | 1421 |
| `total_ms` | 1506 | 1540 | 1821 |

`settle_ms` 最大 366 ms 与 `motion_ms` 最大 255 ms 都出现在位置 4 vp13 → 14 的 1 行底部短步；其余 4 个较长的 settle（333–350 ms）也都是底部短步。每次滚动的停稳确认都是 2 次。

> p90 沿用证据分析脚本的取法：59 个样本按升序取第 ⌊0.90 × 59⌋ = 53 个值；按 nearest-rank 会取第 54 个，`settle` 为 59、`motion` 为 18、`drag` 为 1400、`total` 为 1557。正文采用前者，以与 `analysis.txt` 一致。

### 5. 诊断

- 新增诊断目录 59 个：8 个 `scroll-ok`（含关键帧 PNG）+ 51 个 `scroll-ok-manifest`（仅 manifest）；新增失败目录 0 个，新增 legacy PNG 0 个。
- 59 份 manifest 的 position / viewport / before_frame / after_frame 与 59 条滚动汇总逐项一致（差异 0）；`motion_frame` / `settled_frame` 缺失 0。
- 滚动日志全部解析（0 unparsed）；诊断目录总数 84 = 25 个旧条目 + 59 个新条目。
- 位置级降级：0 个不完整位置，attempt2 日志无 `position=N incomplete`，检查点无降级记录；未触发 #101 的降级路径。

### 6. 权限与证据保全

- **运行证据：** 整轮 59 次滚动、1,017 次物品访问持续成功；attempt2 日志没有权限、采集或输入故障，平台会话以 `captureStopped=true drained=true released=true` 收尾。证据目录没有 TCC 授权状态的独立读数，因此只把这次成功运行记为运行证据，不作为权限审计结论。
- **预扫备份：** `pre-scan-backup/` 的 275 个文件 SHA-256 全部通过；旧轮转日志 `.1`–`.3`、`youkai.crash.log` 与备份逐字节一致；25 个旧诊断条目 `diff -r` 无差异。
- **日志未轮转：** `logs-after/youkai.log` 共 2,544 行 = 60 行 2026-10-31 尾部 + 10 行 attempt1 + 2,474 行 attempt2；`.1`–`.3` 的大小与时间戳不变，没有 `.4`。
- **统计切片：** 2026-10-31 尾部含 2 行 `outcome=undecided`，不属于本次；attempt2 统计从 `2026/11/01 10:48:01` 起切片。仅按 `2026/11/01` 前缀会把 attempt1 的 10 行也算入（attempt1 没有滚动或物品行，不影响本报告计数）。
- **校验和：** `SHA256SUMS` 清单内 attempt1 17/17、attempt2 102/102、pre-scan-backup 275/275 通过；清单不覆盖全部 74 MB 证据文件。

## 问题与观察

### 第一次尝试：窗口未完整落在主显示器内

`targetNotFound` 是通用消息，几何文件给出了具体原因：窗口右边界超出主显示器可见区 56 pt。扫描器在目标解析阶段就拒绝，没有发送任何输入，符合 fail-closed 预期。这也说明运行前把窗口完整放回一块显示器是必要步骤；本次通过调整窗口解决，未修改代码。

### 详情归属的参照往返

本次 1,002 件以 `transitionVerified` 接纳，9 件走相邻同详情的参照往返并以 `referenceVerified` 接纳，6 件为 `initialVerified`；没有 `detailUnattributed` 或 `detailUnresolved`。9 次参照往返都成功。最末件（位置 6、vp13、slot 16、seq 1017）也通过参照往返接纳，`reference_ms=1427`、`total_ms=3637`。该路径不是阶段 2 滚动判定，但属于本次全量扫描的实际行为。

### 顶部确认

六个位置的首次顶部确认即接受，没有出现 2026-10-26 的第二次 5 秒等待。这里只记录本次结果，不能据此删除其他位置或客户端的顶部复查逻辑。

## 未覆盖项与不能推出的结论

- **只有一次完整扫描。** 本次不能证明所有画面与时序下滚动都始终为 `displaced`；3 次滑块佐证只证明该修补路径在这些实例上生效，不是穷尽验证。
- **T5 §6 的三客户端验收未完成。** 本次只是 macOS 云客户端的阶段 2 独立验收；Windows 两种客户端未测，不能宣称 Windows 阶段 2 通过，也不能替代每客户端至少三次人工基准库存扫描。
- **位置级降级未在真机触发。** 本次 0 个不完整位置、无 `positionFailureLimit`，因此 #101 的降级路径没有真机证据；不能由本次结果推断该路径在真机可用。
- **未做导出、截图与界面复核。** 本次用 `YOUKAI_DEV_AUTOSTART=1` 自动启动扫描，结束后没有进入结果页操作；进程完成 `exit wrap-up complete`。没有点击导出或截图，`~/Downloads` 没有本次的导出文件，也没有复核历史重开、逐件校对、设置与更新。
- **排序与计数口径有限。** `sort=verified` 只证明面板标签模板匹配，不读取升降序方向；界面不显示数量，本次未做计数对账。`coverage=complete` 是当前实现的覆盖口径（六位置账目、顶部/底部证据、排序标签），不等同于阶段 3 的蓝色填充与面板底部控件范围证明，也不持续检查扫描中的排序/筛选变化。
- **权限只取得运行证据。** 没有 TCC 授权状态的独立读数，也没有权限撤回或采集失败的故障注入。
- **本次未运行 Go 测试或 `-race`。** 既有的偶发 `-race` 失败与本次扫描无关，不能据此认为它已解决。
- **构建范围有限。** 这是 ad-hoc 签名的 dev bundle，不是打包或公证制品；结论只适用于该构建。
- **游戏版本 3.3.0 来自操作者记录。** 证据目录没有保存版本读数，不能由证据复核该版本号。

## 证据

```text
验收根目录:
~/Library/Logs/Youkai Dev/probes/20261101-full-acceptance/

attempt1/:
  analysis.txt, analysis.json, checkpoint-final.json,
  window-geometry.txt, preflight.txt, run.out, SHA256SUMS

attempt2/:
  analysis.txt, analysis.json, basis-by-position.txt,
  checkpoint-final.json, window-geometry.txt, preflight.txt, run.out,
  logs-after/youkai.log, diagnostics/, SHA256SUMS

pre-scan-backup/:
  logs/, diagnostics/, SHA256SUMS

会话与最终检查点:
~/Library/Application Support/Youkai Dev/sessions/76bc33aa-4e2e-4b5d-951b-021eadaa9c1a/
  checkpoints/130.json
  evidence/（10,937 个文件，约 74 MB）

第一次尝试的检查点:
~/Library/Application Support/Youkai Dev/sessions/ffedad34-1889-4088-9593-25c99b2f41a8/checkpoints/1.json
```

## 复现方式

```text
YOUKAI_GAME_SOURCE=traverse YOUKAI_DEV_AUTOSTART=1 "<dev bundle>/Contents/MacOS/Youkai Dev"
```

1. 打开《云·绝区零》并进入驱动盘仓库，确认窗口完整落在一块显示器内（本次为 1164 × 762）。
2. 保持游戏窗口在前台，不移动鼠标；应用按 `YOUKAI_DEV_AUTOSTART` 自动开始扫描。
3. 等待 `completed / verifiedEnd / capturing` 与 `exit wrap-up complete`；本次未执行导出或截图。

## 附录：计数复核命令

以下命令只读证据，输出文件写在 `/tmp`。`R` 指向本次验收目录。

```text
R="$HOME/Library/Logs/Youkai Dev/probes/20261101-full-acceptance"
L="$R/attempt2/logs-after/youkai.log"

# attempt2 切片：从 10:48:01 起，排除 10-31 尾部与 attempt1
awk '/^2026\/11\/01 10:48:01/{f=1} f' "$L" > /tmp/youkai-1101-attempt2.log
```

**滚动与位置**

```text
# 118 = 59 次 × 2 行
grep -c 'collect: scroll position=' /tmp/youkai-1101-attempt2.log

# 59 条汇总行；attempts 全部为 1
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | wc -l
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | grep -o 'attempts=[0-9]*' | sort | uniq -c

# 59 displaced
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | sed -E 's/.*outcome=([a-z]+).*/\1/' | sort | uniq -c

# 56 selection+feature / 3 selection+feature+scrollbar
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | sed -E 's/.*basis=([a-z+]+).*/\1/' | sort | uniq -c

# 每位置滚动次数 5/6/6/14/15/13
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | sed -E 's/.*position=([0-9]+) viewport_from=.*/\1/' | sort -n | uniq -c

# 前进行数分布
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | grep -o 'rows=[0-9]*' | cut -d= -f2 | sort -n | uniq -c
```

**时序**

```text
# 中位数 = 第 30 个，p90 = 第 53 个（分析脚本口径），最大值 = 第 59 个
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | grep -o 'motion_ms=[0-9]*' | cut -d= -f2 | sort -n | sed -n '30p;53p;59p'
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | grep -o 'settle_ms=[0-9]*' | cut -d= -f2 | sort -n | sed -n '30p;53p;59p'
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | grep -o 'drag_ms=[0-9]*' | cut -d= -f2 | sort -n | sed -n '30p;53p;59p'
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | grep -o 'total_ms=[0-9]*' | cut -d= -f2 | sort -n | sed -n '30p;53p;59p'
```

**manifest 与检查点**

```text
# 59 个新诊断目录；59 份 manifest 的 settle_observations 均为 2
find "$R/attempt2/diagnostics" -maxdepth 1 -type d -name '20261101*' | wc -l
jq -r '.settle_observations' "$R"/attempt2/diagnostics/20261101*/manifest.json | sort | uniq -c

# manifest 的 position/viewport/前后帧与日志逐项比对
grep 'collect: scroll position=.*attempts=' /tmp/youkai-1101-attempt2.log | sed -E 's/.*position=([0-9]+) viewport_from=([0-9]+) viewport_to=([0-9]+).*before_frame=([0-9]+) after_frame=([0-9]+).*/\1 \2 \3 \4 \5/' | sort -n > /tmp/youkai-1101-log-pairs.txt
for m in "$R"/attempt2/diagnostics/20261101*/manifest.json; do jq -r '[.position, (.viewport-1), .viewport, .before_frame, .after_frame] | @tsv' "$m" | tr '\t' ' '; done | sort -n > /tmp/youkai-1101-manifest-pairs.txt
diff /tmp/youkai-1101-log-pairs.txt /tmp/youkai-1101-manifest-pairs.txt

# 检查点汇总
jq -r '[.session.sessionId, .session.termination.kind, .session.termination.code, .session.coverage.status, .session.capturedCount, (.items|length), (.evidence|length), .session.observedScope.sort, .session.observedScope.sortLabel, .session.observedScope.coverage] | @tsv' "$R/attempt2/checkpoint-final.json"
jq -r '.session.observedScope.positions[] | [.position, .viewports, .visits, .complete, .topVerified, .bottomVerified] | @tsv' "$R/attempt2/checkpoint-final.json"
```

**物品与详情归属**

```text
grep -c 'collect: item ' /tmp/youkai-1101-attempt2.log
grep 'collect: item ' /tmp/youkai-1101-attempt2.log | sed -E 's/.*association=([A-Za-z]+).*/\1/' | sort | uniq -c
grep 'collect: item ' /tmp/youkai-1101-attempt2.log | sed -E 's/.*retries=([0-9]+).*/\1/' | sort | uniq -c
grep -c 'trying one reference round trip' /tmp/youkai-1101-attempt2.log
```

**第一次尝试的几何**

```text
grep 'bounds=.*"Height": 835' "$R/attempt1/window-geometry.txt"
grep 'screen frame' "$R/attempt1/window-geometry.txt" | head -1
# 110 + 1458 = 1568；54 + 1458 = 1512；差 56 pt
```

**日志切片与证据保全**

```text
wc -l < "$R/attempt2/logs-after/youkai.log"
grep -c '^2026/10/31 ' "$R/attempt2/logs-after/youkai.log"
awk '/^2026\/11\/01 10:(25|26)/' "$R/attempt2/logs-after/youkai.log" | wc -l
grep -n 'outcome=undecided' "$R/attempt2/logs-after/youkai.log"

(cd "$R/attempt1" && shasum -a 256 -c SHA256SUMS | grep -c ': OK$')
(cd "$R/attempt2" && shasum -a 256 -c SHA256SUMS | grep -c ': OK$')
(cd "$R/pre-scan-backup" && shasum -a 256 -c SHA256SUMS | grep -c ': OK$')

for f in $(ls "$R/pre-scan-backup/diagnostics"); do diff -r -q "$R/pre-scan-backup/diagnostics/$f" "$HOME/Library/Logs/Youkai Dev/diagnostics/$f"; done
```
