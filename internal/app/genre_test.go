package app

import "testing"

func TestLookupGenre(t *testing.T) {
	cases := []struct{ dir, wantGroup, wantName string }{
		{"华尔兹", "摩登", "华尔兹"},
		{"waltz", "摩登", "华尔兹"},
		{"My Waltz Collection", "摩登", "华尔兹"},
		{"探戈", "摩登", "探戈"},
		{"Tango", "摩登", "探戈"},
		{"维也纳华尔兹", "摩登", "维也纳华尔兹"},
		{"维也纳", "摩登", "维也纳华尔兹"},
		{"Viennese Waltz", "摩登", "维也纳华尔兹"},
		{"狐步", "摩登", "狐步"},
		{"Slow Foxtrot", "摩登", "狐步"},
		{"快步", "摩登", "快步"},
		{"Quickstep", "摩登", "快步"},
		{"桑巴", "拉丁", "桑巴"},
		{"森巴", "拉丁", "桑巴"},
		{"恰恰", "拉丁", "恰恰"},
		{"Cha Cha Cha", "拉丁", "恰恰"},
		{"伦巴", "拉丁", "伦巴"},
		{"Rumba", "拉丁", "伦巴"},
		{"斗牛", "拉丁", "斗牛"},
		{"Paso Doble", "拉丁", "斗牛"},
		{"牛仔", "拉丁", "牛仔"},
		{"Jive", "拉丁", "牛仔"},
		{"慢三", "交谊舞", "慢三"},
		{"慢三步", "交谊舞", "慢三"},
		{"平四", "交谊舞", "平四"},
		{"北京平四", "交谊舞", "平四"},
		{"交谊舞伦巴", "交谊舞", "交谊舞伦巴"},
		{"交谊舞探戈", "交谊舞", "交谊舞探戈"},
		{"吉特巴", "交谊舞", "吉特巴"},
		{"水兵舞", "交谊舞", "吉特巴"},
		{"Jitterbug", "交谊舞", "吉特巴"},
		{"special", "其它", "special"},
		{"我的音乐", "其它", "我的音乐"},
	}
	for _, c := range cases {
		g, n := LookupGenre(c.dir)
		if g != c.wantGroup || n != c.wantName {
			t.Errorf("LookupGenre(%q) = (%q,%q), 期望 (%q,%q)", c.dir, g, n, c.wantGroup, c.wantName)
		}
	}
}

func TestGenreLessA组顺序(t *testing.T) {
	// 摩登 A 组内顺序
	order := []string{"华尔兹", "探戈", "维也纳华尔兹", "狐步", "快步"}
	for i := 0; i < len(order)-1; i++ {
		if !genreLess(order[i], order[i+1]) {
			t.Errorf("摩登顺序错误: %s 应在 %s 前", order[i], order[i+1])
		}
	}
	// 拉丁 A 组内顺序
	order = []string{"桑巴", "恰恰", "伦巴", "斗牛", "牛仔"}
	for i := 0; i < len(order)-1; i++ {
		if !genreLess(order[i], order[i+1]) {
			t.Errorf("拉丁顺序错误: %s 应在 %s 前", order[i], order[i+1])
		}
	}
	// 交谊舞 A 组内顺序
	order = []string{"慢三", "平四", "交谊舞伦巴", "交谊舞探戈", "吉特巴"}
	for i := 0; i < len(order)-1; i++ {
		if !genreLess(order[i], order[i+1]) {
			t.Errorf("交谊舞顺序错误: %s 应在 %s 前", order[i], order[i+1])
		}
	}
	// 组间顺序：摩登 < 拉丁 < 交谊舞 < 其它
	if !genreLess("快步", "桑巴") || !genreLess("牛仔", "慢三") || !genreLess("吉特巴", "special") {
		t.Error("组间顺序错误")
	}
}
