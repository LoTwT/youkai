// Package zzz is the ZZZ (Zenless Zone Zero) game adaptation: the drive-
// disc data tables copied once from fairy and, on top of them, the field
// parsing of one drive-disc detail page. Parsing covers the fixed text
// lines the recognition stage reads; page traversal, evidence and the
// wiring into the scan flow are later M2 work.
//
// The tables are a one-time copy from the fairy repository at commit
// 726c9116eced4c528f11a71f612807d29d8678a4 (Nanoka 3.2 snapshot, 60
// agents and 30 drive-disc sets), taken 2026-10-18:
//
//   - sets and agents: packages/data/integrated/drive-discs and
//     packages/data/integrated/agents — the English detail name is the
//     public identity (fairy consumption spec), the Chinese display name
//     comes from details.zh.json, and the numeric source id is the fairy
//     entity id;
//   - main stats by slot and the ten substats:
//     packages/data/definitions/attributes/drive-disc-affixes.json;
//   - attribute identifiers: packages/shared/src/attributes.ts.
//
// The copy is deliberate and manual (architecture section 6): no JSON
// resources, no generator script. When the game adds sets or agents,
// extend the tables against fairy by hand and bump CatalogRevision. The
// zh-CN attribute display names of affixes.go come from the six-disc
// sample of internal/recognition/testdata plus nine names supplied by
// the user on 2026-10-18 and verified name-by-name against real client
// screens in the 2026-10-19 macOS sample round, which corrected ether
// to 以太伤害加成 (see there).
package zzz

// CatalogRevision identifies the revision of these data tables. It is the
// version the scan JSON contract records as catalog.revision and
// validationRules.version: bump it whenever table content changes, for
// example when new sets or agents are added after a game update, or a
// display name is corrected against real screens (revision 2: the ether
// correction of 2026-10-19).
const CatalogRevision = "2"

// Set is one drive-disc set of the catalog. ID is the stable youkai
// identifier generated from the fairy English name (never reused or
// renamed after release); FairyID is fairy's numeric source id; Name is
// the zh-CN display name the detail page shows.
type Set struct {
	ID      string
	FairyID string
	Name    string
}

// Agent is one equipper agent of the catalog, addressed the same way as
// Set. Agents are not parsed by the detail-line parser of this package
// yet; the table exists so set and agent resolution share one catalog.
type Agent struct {
	ID      string
	FairyID string
	Name    string
}

// sets and agents are the full Nanoka 3.2 catalogs, in fairy source-id
// order. Youkai IDs are derived from the English names (first token
// lower-cased, remaining tokens capitalized, non-alphanumerics dropped);
// the English source name is kept as a comment per row.
//
// Name is the fairy details.zh.json name verbatim, except for set 33700
// ("雪兔梦游仙境 "), whose upstream name carries a trailing space that is
// trimmed here so exact-name lookups work.
var sets = []Set{
	{ID: "woodpeckerElectro", FairyID: "31000", Name: "啄木鸟电音"},  // Woodpecker Electro
	{ID: "pufferElectro", FairyID: "31100", Name: "河豚电音"},       // Puffer Electro
	{ID: "shockstarDisco", FairyID: "31200", Name: "震星迪斯科"},     // Shockstar Disco
	{ID: "freedomBlues", FairyID: "31300", Name: "自由蓝调"},        // Freedom Blues
	{ID: "hormonePunk", FairyID: "31400", Name: "激素朋克"},         // Hormone Punk
	{ID: "soulRock", FairyID: "31500", Name: "灵魂摇滚"},            // Soul Rock
	{ID: "swingJazz", FairyID: "31600", Name: "摇摆爵士"},           // Swing Jazz
	{ID: "chaosJazz", FairyID: "31800", Name: "混沌爵士"},           // Chaos Jazz
	{ID: "protoPunk", FairyID: "31900", Name: "原始朋克"},           // Proto Punk
	{ID: "infernoMetal", FairyID: "32200", Name: "炎狱重金属"},       // Inferno Metal
	{ID: "chaoticMetal", FairyID: "32300", Name: "混沌重金属"},       // Chaotic Metal
	{ID: "thunderMetal", FairyID: "32400", Name: "雷暴重金属"},       // Thunder Metal
	{ID: "polarMetal", FairyID: "32500", Name: "极地重金属"},         // Polar Metal
	{ID: "fangedMetal", FairyID: "32600", Name: "獠牙重金属"},        // Fanged Metal
	{ID: "branchBladeSong", FairyID: "32700", Name: "折枝剑歌"},     // Branch & Blade Song
	{ID: "astralVoice", FairyID: "32800", Name: "静听嘉音"},         // Astral Voice
	{ID: "shadowHarmony", FairyID: "32900", Name: "如影相随"},       // Shadow Harmony
	{ID: "phaethonSMelody", FairyID: "33000", Name: "法厄同之歌"},    // Phaethon's Melody
	{ID: "yunkuiTales", FairyID: "33100", Name: "云岿如我"},         // Yunkui Tales
	{ID: "kingOfTheSummit", FairyID: "33200", Name: "山大王"},      // King of the Summit
	{ID: "dawnSBloom", FairyID: "33300", Name: "拂晓生花"},          // Dawn's Bloom
	{ID: "moonlightLullaby", FairyID: "33400", Name: "月光骑士颂"},   // Moonlight Lullaby
	{ID: "whiteWaterBallad", FairyID: "33500", Name: "沧浪行歌"},    // White Water Ballad
	{ID: "shiningAria", FairyID: "33600", Name: "流光咏叹"},         // Shining Aria
	{ID: "bunnyInWonderland", FairyID: "33700", Name: "雪兔梦游仙境"}, // Bunny in Wonderland
	{ID: "notesFromTheChained", FairyID: "33800", Name: "囚徒手记"}, // Notes From the Chained
	{ID: "wutheringSalon", FairyID: "33900", Name: "呼啸沙龙"},      // Wuthering Salon
	{ID: "theSkyAblaze", FairyID: "34000", Name: "拂晓行纪"},        // The Sky Ablaze
	{ID: "featheredFate", FairyID: "34100", Name: "谶羽之誓"},       // Feathered Fate
	{ID: "thornedRose", FairyID: "34200", Name: "荆棘玫瑰"},         // Thorned Rose
}

var agents = []Agent{
	{ID: "anby", FairyID: "1011", Name: "安比"},              // Anby
	{ID: "nekomata", FairyID: "1021", Name: "猫又"},          // Nekomata
	{ID: "nicole", FairyID: "1031", Name: "妮可"},            // Nicole
	{ID: "soldier11", FairyID: "1041", Name: "「11号」"},      // Soldier 11
	{ID: "yidhari", FairyID: "1051", Name: "伊德海莉"},         // Yidhari
	{ID: "corin", FairyID: "1061", Name: "可琳"},             // Corin
	{ID: "caesar", FairyID: "1071", Name: "凯撒"},            // Caesar
	{ID: "billy", FairyID: "1081", Name: "比利"},             // Billy
	{ID: "miyabi", FairyID: "1091", Name: "雅"},             // Miyabi
	{ID: "koleda", FairyID: "1101", Name: "珂蕾妲"},           // Koleda
	{ID: "anton", FairyID: "1111", Name: "安东"},             // Anton
	{ID: "ben", FairyID: "1121", Name: "本"},                // Ben
	{ID: "soukaku", FairyID: "1131", Name: "苍角"},           // Soukaku
	{ID: "lycaon", FairyID: "1141", Name: "莱卡恩"},           // Lycaon
	{ID: "lucy", FairyID: "1151", Name: "露西"},              // Lucy
	{ID: "lighter", FairyID: "1161", Name: "莱特"},           // Lighter
	{ID: "burnice", FairyID: "1171", Name: "柏妮思"},          // Burnice
	{ID: "grace", FairyID: "1181", Name: "格莉丝"},            // Grace
	{ID: "ellen", FairyID: "1191", Name: "艾莲"},             // Ellen
	{ID: "harumasa", FairyID: "1201", Name: "悠真"},          // Harumasa
	{ID: "rina", FairyID: "1211", Name: "丽娜"},              // Rina
	{ID: "yanagi", FairyID: "1221", Name: "柳"},             // Yanagi
	{ID: "zhuYuan", FairyID: "1241", Name: "朱鸢"},           // Zhu Yuan
	{ID: "qingyi", FairyID: "1251", Name: "青衣"},            // Qingyi
	{ID: "jane", FairyID: "1261", Name: "简"},               // Jane
	{ID: "seth", FairyID: "1271", Name: "赛斯"},              // Seth
	{ID: "piper", FairyID: "1281", Name: "派派"},             // Piper
	{ID: "hugo", FairyID: "1291", Name: "雨果"},              // Hugo
	{ID: "orphieMagus", FairyID: "1301", Name: "奥菲丝&「鬼火」"}, // Orphie & Magus
	{ID: "astraYao", FairyID: "1311", Name: "耀嘉音"},         // Astra Yao
	{ID: "evelyn", FairyID: "1321", Name: "伊芙琳"},           // Evelyn
	{ID: "vivian", FairyID: "1331", Name: "薇薇安"},           // Vivian
	{ID: "zhao", FairyID: "1341", Name: "照"},               // Zhao
	{ID: "pulchra", FairyID: "1351", Name: "波可娜"},          // Pulchra
	{ID: "trigger", FairyID: "1361", Name: "「扳机」"},         // Trigger
	{ID: "yixuan", FairyID: "1371", Name: "仪玄"},            // Yixuan
	{ID: "soldier0Anby", FairyID: "1381", Name: "零号·安比"},   // Soldier 0 - Anby
	{ID: "juFufu", FairyID: "1391", Name: "橘福福"},           // Ju Fufu
	{ID: "alice", FairyID: "1401", Name: "爱丽丝"},            // Alice
	{ID: "yuzuha", FairyID: "1411", Name: "柚叶"},            // Yuzuha
	{ID: "panYinhu", FairyID: "1421", Name: "潘引壶"},         // Pan Yinhu
	{ID: "yeShunguang", FairyID: "1431", Name: "叶瞬光"},      // Ye Shunguang
	{ID: "manato", FairyID: "1441", Name: "真斗"},            // Manato
	{ID: "lucia", FairyID: "1451", Name: "卢西娅"},            // Lucia
	{ID: "seed", FairyID: "1461", Name: "「席德」"},            // Seed
	{ID: "banyue", FairyID: "1471", Name: "般岳"},            // Banyue
	{ID: "dialyn", FairyID: "1481", Name: "琉音"},            // Dialyn
	{ID: "sunna", FairyID: "1491", Name: "千夏"},             // Sunna
	{ID: "aria", FairyID: "1501", Name: "爱芮"},              // Aria
	{ID: "nangongYu", FairyID: "1511", Name: "南宫羽"},        // Nangong Yu
	{ID: "cissia", FairyID: "1521", Name: "希希芙"},           // Cissia
	{ID: "starlightBilly", FairyID: "1531", Name: "星徽·比利"}, // Starlight - Billy
	{ID: "promeia", FairyID: "1541", Name: "普罗米娅"},         // Promeia
	{ID: "pyrois", FairyID: "1551", Name: "佩洛伊斯"},          // Pyrois
	{ID: "velina", FairyID: "1561", Name: "维琳娜"},           // Velina
	{ID: "norma", FairyID: "1571", Name: "诺姆"},             // Norma
	{ID: "remielle", FairyID: "1581", Name: "蕾米埃尔"},        // Remielle
	{ID: "sigrid", FairyID: "1591", Name: "希格莉德"},          // Sigrid
	{ID: "claret", FairyID: "1611", Name: "克拉蕾"},           // Claret
	{ID: "roxy", FairyID: "1621", Name: "洛克茜"},             // Roxy
}

// setByName and setByID index the set table; agentByName and agentByID
// index the agents. Lookups are exact — no trimming, no case folding.
var (
	setByName   = make(map[string]Set, len(sets))
	setByID     = make(map[string]Set, len(sets))
	agentByName = make(map[string]Agent, len(agents))
	agentByID   = make(map[string]Agent, len(agents))
)

func init() {
	for _, s := range sets {
		setByName[s.Name] = s
		setByID[s.ID] = s
	}
	for _, a := range agents {
		agentByName[a.Name] = a
		agentByID[a.ID] = a
	}
}

// LookupSetByName resolves a zh-CN set display name to its catalog entry.
func LookupSetByName(name string) (Set, bool) {
	s, ok := setByName[name]
	return s, ok
}

// LookupSetByID resolves a youkai set identifier to its catalog entry.
func LookupSetByID(id string) (Set, bool) {
	s, ok := setByID[id]
	return s, ok
}

// LookupAgentByName resolves a zh-CN agent display name to its catalog
// entry.
func LookupAgentByName(name string) (Agent, bool) {
	a, ok := agentByName[name]
	return a, ok
}

// LookupAgentByID resolves a youkai agent identifier to its catalog entry.
func LookupAgentByID(id string) (Agent, bool) {
	a, ok := agentByID[id]
	return a, ok
}
