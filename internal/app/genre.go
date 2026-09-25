// 舞种归一化：把音乐库的目录名映射到标准舞种（分组 + 标准名）。
// 分组与 A 组顺序遵循赛事惯例：摩登、拉丁、交谊舞；无法识别的目录归入「其它」。
package app

import (
	"sort"
	"strings"
)

// GenreGroupOrder 分组展示顺序（前端分栏与此一致）。
var GenreGroupOrder = []string{"摩登", "拉丁", "交谊舞", "其它"}

// genreDefs 标准舞种定义：组内顺序即 A 组出场顺序。
// aliases 已含中文常用名/俗称与英文写法，匹配时做归一化（小写、去空白与连接符）。
var genreDefs = []struct {
	Group   string
	Name    string
	Aliases []string
}{
	// 摩登（Standard）
	{"摩登", "华尔兹", []string{"华尔兹", "华尔滋", "圆舞曲", "waltz", "slowwaltz", "slowwalzer"}},
	{"摩登", "探戈", []string{"探戈", "tango"}},
	{"摩登", "维也纳华尔兹", []string{"维也纳华尔兹", "维也纳", "维也那", "快三", "快三步", "viennesewaltz", "viennese", "vwalz", "vwaltz"}},
	{"摩登", "狐步", []string{"狐步", "狐步舞", "福克斯", "foxtrot", "foxtrott", "slowfoxtrot", "slowfox", "fox"}},
	{"摩登", "快步", []string{"快步", "快步舞", "quickstep", "quickstep"}},
	// 拉丁（Latin）
	{"拉丁", "桑巴", []string{"桑巴", "森巴", "samba"}},
	{"拉丁", "恰恰", []string{"恰恰", "恰恰恰", "恰查查", "chacha", "chachacha"}},
	{"拉丁", "伦巴", []string{"伦巴", "仑巴", "rumba", "rhumba"}},
	{"拉丁", "斗牛", []string{"斗牛", "斗牛舞", "西班牙斗牛", "pasodoble", "paso"}},
	{"拉丁", "牛仔", []string{"牛仔", "牛仔舞", "jive"}},
	// 交谊舞（大众交谊舞）
	{"交谊舞", "慢三", []string{"慢三", "慢三步", "交谊舞慢三", "slowthree"}},
	{"交谊舞", "平四", []string{"平四", "平四步", "北京平四", "pingsi"}},
	{"交谊舞", "交谊舞伦巴", []string{"交谊舞伦巴", "社交伦巴", "socialrumba"}},
	{"交谊舞", "交谊舞探戈", []string{"交谊舞探戈", "社交探戈", "socialtango"}},
	{"交谊舞", "吉特巴", []string{"吉特巴", "吉特帕", "水兵舞", "jitterbug"}},
}

var (
	genreByAlias  map[string]genreDef // 归一化别名 -> 定义
	genreByName   map[string]genreDef
	genreGroupIdx map[string]int
	genreOrderIdx map[string]int // 标准名 -> 组内顺序
)

type genreDef struct {
	Group string
	Name  string
}

func init() {
	genreByAlias = map[string]genreDef{}
	genreByName = map[string]genreDef{}
	genreGroupIdx = map[string]int{}
	for i, g := range GenreGroupOrder {
		genreGroupIdx[g] = i
	}
	genreOrderIdx = map[string]int{}
	for i, d := range genreDefs {
		genreOrderIdx[d.Name] = i
		genreByName[d.Name] = genreDef{d.Group, d.Name}
		for _, a := range d.Aliases {
			genreByAlias[normGenre(a)] = genreDef{d.Group, d.Name}
		}
		// 标准名本身也是可用别名
		genreByAlias[normGenre(d.Name)] = genreDef{d.Group, d.Name}
	}
}

// normGenre 归一化：小写、去除空白与常见连接符，便于匹配目录名。
func normGenre(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '-', '_', '.', '/', '\\', '(', ')', '（', '）', '·':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// LookupGenre 目录名 -> 标准舞种。先精确匹配归一化名；
// 再做「最长别名优先」的包含匹配（可识别"我的华尔兹精选"这类目录名，
// 且"交谊舞伦巴"优先于"伦巴"、"维也纳华尔兹"优先于"华尔兹"）。
// 匹配不到时归入「其它」组，目录名即舞种名。
func LookupGenre(dirName string) (group, name string) {
	n := normGenre(dirName)
	if n == "" {
		return "其它", dirName
	}
	if d, ok := genreByAlias[n]; ok {
		return d.Group, d.Name
	}
	// 包含匹配：按别名长度降序，避免短别名抢先
	type al struct {
		norm string
		def  genreDef
	}
	var cands []al
	for a, d := range genreByAlias {
		if strings.Contains(n, a) {
			cands = append(cands, al{a, d})
		}
	}
	if len(cands) > 0 {
		sort.Slice(cands, func(i, j int) bool {
			if len(cands[i].norm) != len(cands[j].norm) {
				return len(cands[i].norm) > len(cands[j].norm)
			}
			return cands[i].def.Name < cands[j].def.Name
		})
		return cands[0].def.Group, cands[0].def.Name
	}
	return "其它", dirName
}

// genreLess 组间按 GenreGroupOrder、组内按 A 组顺序、其它按名称排序。
func genreLess(a, b string) bool {
	ga, gb := genreByName[a], genreByName[b]
	gia, okA := genreGroupIdx[ga.Group]
	if !okA {
		gia = len(GenreGroupOrder) - 1
	}
	gib, okB := genreGroupIdx[gb.Group]
	if !okB {
		gib = len(GenreGroupOrder) - 1
	}
	if gia != gib {
		return gia < gib
	}
	if ga.Group != "其它" && ga.Group == gb.Group {
		ia, okA2 := genreOrderIdx[a]
		ib, okB2 := genreOrderIdx[b]
		if okA2 && okB2 && ia != ib {
			return ia < ib
		}
	}
	return a < b
}
