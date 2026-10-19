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

// Set is one drive-disc set of the catalog. ID is fairy's numeric entity
// id and the public structured identity; Key is the internal readable
// alias derived from the fairy English name (first token lower-cased,
// remaining tokens capitalized, non-alphanumerics dropped), kept for
// diagnostics and internal lookups; Name is the zh-CN display name the
// detail page shows.
type Set struct {
	ID   int
	Key  string
	Name string
}

// Agent is one equipper agent of the catalog, addressed the same way as
// Set. Agents are not parsed by the detail-line parser of this package
// yet; the table exists so set and agent resolution share one catalog.
type Agent struct {
	ID   int
	Key  string
	Name string
}

// sets and agents are the full Nanoka 3.2 catalogs, in fairy source-id
// order. The fairy numeric id is the primary identity; the English source
// name remains as a per-row comment and its derived key as the internal
// alias.
//
// Name is the fairy details.zh.json name verbatim, except for set 33700
// ("雪兔梦游仙境 "), whose upstream name carries a trailing space that is
// trimmed here so exact-name lookups work.
var sets = []Set{
	{ID: 31000, Key: "woodpeckerElectro", Name: "啄木鸟电音"},  // Woodpecker Electro
	{ID: 31100, Key: "pufferElectro", Name: "河豚电音"},       // Puffer Electro
	{ID: 31200, Key: "shockstarDisco", Name: "震星迪斯科"},     // Shockstar Disco
	{ID: 31300, Key: "freedomBlues", Name: "自由蓝调"},        // Freedom Blues
	{ID: 31400, Key: "hormonePunk", Name: "激素朋克"},         // Hormone Punk
	{ID: 31500, Key: "soulRock", Name: "灵魂摇滚"},            // Soul Rock
	{ID: 31600, Key: "swingJazz", Name: "摇摆爵士"},           // Swing Jazz
	{ID: 31800, Key: "chaosJazz", Name: "混沌爵士"},           // Chaos Jazz
	{ID: 31900, Key: "protoPunk", Name: "原始朋克"},           // Proto Punk
	{ID: 32200, Key: "infernoMetal", Name: "炎狱重金属"},       // Inferno Metal
	{ID: 32300, Key: "chaoticMetal", Name: "混沌重金属"},       // Chaotic Metal
	{ID: 32400, Key: "thunderMetal", Name: "雷暴重金属"},       // Thunder Metal
	{ID: 32500, Key: "polarMetal", Name: "极地重金属"},         // Polar Metal
	{ID: 32600, Key: "fangedMetal", Name: "獠牙重金属"},        // Fanged Metal
	{ID: 32700, Key: "branchBladeSong", Name: "折枝剑歌"},     // Branch & Blade Song
	{ID: 32800, Key: "astralVoice", Name: "静听嘉音"},         // Astral Voice
	{ID: 32900, Key: "shadowHarmony", Name: "如影相随"},       // Shadow Harmony
	{ID: 33000, Key: "phaethonSMelody", Name: "法厄同之歌"},    // Phaethon's Melody
	{ID: 33100, Key: "yunkuiTales", Name: "云岿如我"},         // Yunkui Tales
	{ID: 33200, Key: "kingOfTheSummit", Name: "山大王"},      // King of the Summit
	{ID: 33300, Key: "dawnSBloom", Name: "拂晓生花"},          // Dawn's Bloom
	{ID: 33400, Key: "moonlightLullaby", Name: "月光骑士颂"},   // Moonlight Lullaby
	{ID: 33500, Key: "whiteWaterBallad", Name: "沧浪行歌"},    // White Water Ballad
	{ID: 33600, Key: "shiningAria", Name: "流光咏叹"},         // Shining Aria
	{ID: 33700, Key: "bunnyInWonderland", Name: "雪兔梦游仙境"}, // Bunny in Wonderland
	{ID: 33800, Key: "notesFromTheChained", Name: "囚徒手记"}, // Notes From the Chained
	{ID: 33900, Key: "wutheringSalon", Name: "呼啸沙龙"},      // Wuthering Salon
	{ID: 34000, Key: "theSkyAblaze", Name: "拂晓行纪"},        // The Sky Ablaze
	{ID: 34100, Key: "featheredFate", Name: "谶羽之誓"},       // Feathered Fate
	{ID: 34200, Key: "thornedRose", Name: "荆棘玫瑰"},         // Thorned Rose
}

var agents = []Agent{
	{ID: 1011, Key: "anby", Name: "安比"},              // Anby
	{ID: 1021, Key: "nekomata", Name: "猫又"},          // Nekomata
	{ID: 1031, Key: "nicole", Name: "妮可"},            // Nicole
	{ID: 1041, Key: "soldier11", Name: "「11号」"},      // Soldier 11
	{ID: 1051, Key: "yidhari", Name: "伊德海莉"},         // Yidhari
	{ID: 1061, Key: "corin", Name: "可琳"},             // Corin
	{ID: 1071, Key: "caesar", Name: "凯撒"},            // Caesar
	{ID: 1081, Key: "billy", Name: "比利"},             // Billy
	{ID: 1091, Key: "miyabi", Name: "雅"},             // Miyabi
	{ID: 1101, Key: "koleda", Name: "珂蕾妲"},           // Koleda
	{ID: 1111, Key: "anton", Name: "安东"},             // Anton
	{ID: 1121, Key: "ben", Name: "本"},                // Ben
	{ID: 1131, Key: "soukaku", Name: "苍角"},           // Soukaku
	{ID: 1141, Key: "lycaon", Name: "莱卡恩"},           // Lycaon
	{ID: 1151, Key: "lucy", Name: "露西"},              // Lucy
	{ID: 1161, Key: "lighter", Name: "莱特"},           // Lighter
	{ID: 1171, Key: "burnice", Name: "柏妮思"},          // Burnice
	{ID: 1181, Key: "grace", Name: "格莉丝"},            // Grace
	{ID: 1191, Key: "ellen", Name: "艾莲"},             // Ellen
	{ID: 1201, Key: "harumasa", Name: "悠真"},          // Harumasa
	{ID: 1211, Key: "rina", Name: "丽娜"},              // Rina
	{ID: 1221, Key: "yanagi", Name: "柳"},             // Yanagi
	{ID: 1241, Key: "zhuYuan", Name: "朱鸢"},           // Zhu Yuan
	{ID: 1251, Key: "qingyi", Name: "青衣"},            // Qingyi
	{ID: 1261, Key: "jane", Name: "简"},               // Jane
	{ID: 1271, Key: "seth", Name: "赛斯"},              // Seth
	{ID: 1281, Key: "piper", Name: "派派"},             // Piper
	{ID: 1291, Key: "hugo", Name: "雨果"},              // Hugo
	{ID: 1301, Key: "orphieMagus", Name: "奥菲丝&「鬼火」"}, // Orphie & Magus
	{ID: 1311, Key: "astraYao", Name: "耀嘉音"},         // Astra Yao
	{ID: 1321, Key: "evelyn", Name: "伊芙琳"},           // Evelyn
	{ID: 1331, Key: "vivian", Name: "薇薇安"},           // Vivian
	{ID: 1341, Key: "zhao", Name: "照"},               // Zhao
	{ID: 1351, Key: "pulchra", Name: "波可娜"},          // Pulchra
	{ID: 1361, Key: "trigger", Name: "「扳机」"},         // Trigger
	{ID: 1371, Key: "yixuan", Name: "仪玄"},            // Yixuan
	{ID: 1381, Key: "soldier0Anby", Name: "零号·安比"},   // Soldier 0 - Anby
	{ID: 1391, Key: "juFufu", Name: "橘福福"},           // Ju Fufu
	{ID: 1401, Key: "alice", Name: "爱丽丝"},            // Alice
	{ID: 1411, Key: "yuzuha", Name: "柚叶"},            // Yuzuha
	{ID: 1421, Key: "panYinhu", Name: "潘引壶"},         // Pan Yinhu
	{ID: 1431, Key: "yeShunguang", Name: "叶瞬光"},      // Ye Shunguang
	{ID: 1441, Key: "manato", Name: "真斗"},            // Manato
	{ID: 1451, Key: "lucia", Name: "卢西娅"},            // Lucia
	{ID: 1461, Key: "seed", Name: "「席德」"},            // Seed
	{ID: 1471, Key: "banyue", Name: "般岳"},            // Banyue
	{ID: 1481, Key: "dialyn", Name: "琉音"},            // Dialyn
	{ID: 1491, Key: "sunna", Name: "千夏"},             // Sunna
	{ID: 1501, Key: "aria", Name: "爱芮"},              // Aria
	{ID: 1511, Key: "nangongYu", Name: "南宫羽"},        // Nangong Yu
	{ID: 1521, Key: "cissia", Name: "希希芙"},           // Cissia
	{ID: 1531, Key: "starlightBilly", Name: "星徽·比利"}, // Starlight - Billy
	{ID: 1541, Key: "promeia", Name: "普罗米娅"},         // Promeia
	{ID: 1551, Key: "pyrois", Name: "佩洛伊斯"},          // Pyrois
	{ID: 1561, Key: "velina", Name: "维琳娜"},           // Velina
	{ID: 1571, Key: "norma", Name: "诺姆"},             // Norma
	{ID: 1581, Key: "remielle", Name: "蕾米埃尔"},        // Remielle
	{ID: 1591, Key: "sigrid", Name: "希格莉德"},          // Sigrid
	{ID: 1611, Key: "claret", Name: "克拉蕾"},           // Claret
	{ID: 1621, Key: "roxy", Name: "洛克茜"},             // Roxy
}

// setByName, setByID and setByKey index the set table; agentByName,
// agentByID and agentByKey index the agents. Lookups are exact — no
// trimming, no case folding. The numeric fairy id is the public identity;
// the key is an internal alias only.
var (
	setByName   = make(map[string]Set, len(sets))
	setByID     = make(map[int]Set, len(sets))
	setByKey    = make(map[string]Set, len(sets))
	agentByName = make(map[string]Agent, len(agents))
	agentByID   = make(map[int]Agent, len(agents))
	agentByKey  = make(map[string]Agent, len(agents))
)

func init() {
	for _, s := range sets {
		setByName[s.Name] = s
		setByID[s.ID] = s
		setByKey[s.Key] = s
	}
	for _, a := range agents {
		agentByName[a.Name] = a
		agentByID[a.ID] = a
		agentByKey[a.Key] = a
	}
}

// LookupSetByName resolves a zh-CN set display name to its catalog entry.
func LookupSetByName(name string) (Set, bool) {
	s, ok := setByName[name]
	return s, ok
}

// LookupSetByID resolves a fairy numeric set id to its catalog entry.
func LookupSetByID(id int) (Set, bool) {
	s, ok := setByID[id]
	return s, ok
}

// LookupSetByKey resolves the internal readable set alias to its catalog
// entry. The alias is not part of the public structured data.
func LookupSetByKey(key string) (Set, bool) {
	s, ok := setByKey[key]
	return s, ok
}

// LookupAgentByName resolves a zh-CN agent display name to its catalog
// entry.
func LookupAgentByName(name string) (Agent, bool) {
	a, ok := agentByName[name]
	return a, ok
}

// LookupAgentByID resolves a fairy numeric agent id to its catalog entry.
func LookupAgentByID(id int) (Agent, bool) {
	a, ok := agentByID[id]
	return a, ok
}

// LookupAgentByKey resolves the internal readable agent alias to its
// catalog entry. The alias is not part of the public structured data.
func LookupAgentByKey(key string) (Agent, bool) {
	a, ok := agentByKey[key]
	return a, ok
}
