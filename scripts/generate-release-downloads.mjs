import { readFileSync } from "node:fs";

let repoArg = null;
let tagArg = null;
let releasePath = null;
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  const arg = args[i];
  if (arg === "--repo") {
    repoArg = args[++i];
    if (!repoArg) throw new Error("--repo requires owner/repo");
  } else if (arg === "--tag") {
    tagArg = args[++i];
    if (!tagArg) throw new Error("--tag requires a release tag");
  } else if (arg.startsWith("--")) {
    throw new Error(`Unknown option: ${arg}`);
  } else if (releasePath === null) {
    releasePath = arg;
  } else {
    throw new Error(`Unexpected argument: ${arg}`);
  }
}
if (!releasePath) {
  throw new Error(
    "Usage: node scripts/generate-release-downloads.mjs [--repo owner/repo] [--tag tag] <release.json>",
  );
}

const release = JSON.parse(readFileSync(releasePath, "utf8"));
const match = /^(test-v|v)(\d+\.\d+\.\d+)$/.exec(release.tag_name);
if (!match) throw new Error(`Unsupported release tag: ${release.tag_name}`);

const [, prefix, version] = match;
const tag = tagArg ?? release.tag_name;
if (tag !== release.tag_name) {
  throw new Error(`Release tag ${release.tag_name} does not match expected tag ${tag}`);
}
const repo = repoArg ?? repoFromRelease(release);
const [owner, name] = parseRepo(repo);
// Draft assets can carry /releases/download/untagged-…/ browser URLs. Build
// the public URL from the verified repository, tag and stored asset name.
const downloadBase = `https://github.com/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/releases/download/${encodeURIComponent(tag)}`;

function repoFromRelease(release) {
  const parsed = /^https:\/\/api\.github\.com\/repos\/([^/]+)\/([^/]+)\/releases\/\d+$/.exec(
    release.url ?? "",
  );
  if (!parsed) {
    throw new Error("Cannot determine the repository; pass --repo owner/repo");
  }
  return `${parsed[1]}/${parsed[2]}`;
}

function parseRepo(repo) {
  const parsed = /^([^/]+)\/([^/]+)$/.exec(repo);
  if (!parsed) throw new Error(`Invalid repository: ${repo}`);
  return [parsed[1], parsed[2]];
}

const start = "<!-- youkai-downloads:start -->";
const end = "<!-- youkai-downloads:end -->";
const testWarning = "测试版，勿用：此版本只用于自动更新渠道的联调，正式使用请安装最新正式版。";

function download(assetName) {
  const asset = release.assets.find((asset) => asset.name === assetName);
  if (!asset) {
    throw new Error(`Missing download asset: ${assetName}`);
  }
  return `${downloadBase}/${encodeURIComponent(assetName)}`;
}

let body = release.body ?? "";
const blockStart = body.indexOf(start);
if (blockStart !== -1) {
  const blockEnd = body.indexOf(end, blockStart);
  if (blockEnd === -1) throw new Error("Unclosed Youkai downloads section");
  body = body.slice(0, blockStart) + body.slice(blockEnd + end.length);
}
body = body.trim();
// Older test releases put this warning directly in the original notes.
if (prefix === "test-v" && body.startsWith(testWarning)) {
  body = body.slice(testWarning.length).trimStart();
}

const lines = [start];
if (prefix === "test-v") lines.push(testWarning, "");
lines.push(
  "## 下载安装",
  "",
  "| 系统 | 适用设备 | 安装包 |",
  "| --- | --- | --- |",
  `| **Windows** | x64（Intel / AMD） | **[下载 EXE](${download(`youkai-setup-${version}-amd64.exe`)})** |`,
  "",
  "<details>",
  "<summary>安装提示与文件校验</summary>",
  "",
  "- **Windows**：安装包未签名，首次打开可能出现系统安全提示。",
  `- **文件校验**：下载 [SHA256SUMS](${download("SHA256SUMS")})，核对下载文件的 SHA-256。`,
  "",
  "下方 Assets 还包含自动更新清单、更新归档和安装脚本；直接安装请使用上面的链接。",
  "",
  "</details>",
  "",
  "## 更新内容",
  end,
  "",
  body,
);
process.stdout.write(`${lines.join("\n").trimEnd()}\n`);
