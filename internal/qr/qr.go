// Package qr 实现了一个极简二维码编码器（无第三方依赖）。
// 支持：字节模式（UTF-8）、纠错级别 M、版本 1~6（最大 106 字节）、8 种掩码自动择优。
// 仅用于编码播放页 URL，足以覆盖局域网地址场景。
package qr

import (
	"errors"
	"image"
	"image/color"
)

// ---- 每个版本（M 级）的参数：总码字 / 每块纠错码字 / 数据块数 / 每块数据码字 ----
var versions = [...]struct {
	total, ecPerBlock, blocks, dataPerBlock int
}{
	{26, 10, 1, 16},  // v1 数据 16 码字
	{44, 16, 1, 28},  // v2 数据 28
	{70, 26, 1, 44},  // v3 数据 44
	{100, 18, 2, 32}, // v4 数据 64
	{134, 24, 2, 43}, // v5 数据 86
	{172, 16, 4, 27}, // v6 数据 108
}

// 各版本对齐图案中心坐标（v1 无）。
var alignmentPos = [...][]int{
	nil,     // v1
	{6, 18}, // v2
	{6, 22}, // v3
	{6, 26}, // v4
	{6, 30}, // v5
	{6, 34}, // v6
}

// ---- GF(256) 运算（本原多项式 0x11D，生成元 2） ----
var gfExp [512]byte
var gfLog [256]byte

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// rsEncode 计算消息的 n 个 RS 纠错码字（系统性编码余数）。
func rsEncode(data []byte, n int) []byte {
	// 生成多项式 g(x) = ∏(x - α^i), i=0..n-1
	gen := make([]byte, n+1)
	gen[0] = 1
	for i := 0; i < n; i++ {
		for j := i; j >= 0; j-- {
			gen[j+1] ^= gfMul(gen[j], gfExp[i])
		}
	}
	rem := make([]byte, n)
	for _, b := range data {
		factor := b ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for j := 0; j < n; j++ {
			rem[j] ^= gfMul(gen[j+1], factor)
		}
	}
	return rem
}

// Encode 编码文本为二维码矩阵（true = 黑色模块），自动从 8 种掩码中择优。
func Encode(text string) ([][]bool, error) {
	base, err := encodeBase(text)
	if err != nil {
		return nil, err
	}
	bestMask, bestPenalty := 0, -1
	for mask := 0; mask < 8; mask++ {
		pen := maskPenalty(base, mask)
		if bestPenalty < 0 || pen < bestPenalty {
			bestMask, bestPenalty = mask, pen
		}
	}
	return encodeWithMask(base, bestMask), nil
}

// EncodeWithMask 按指定掩码编码（测试/调试用）。
func EncodeWithMask(text string, mask int) ([][]bool, error) {
	base, err := encodeBase(text)
	if err != nil {
		return nil, err
	}
	if mask < 0 || mask > 7 {
		return nil, errors.New("掩码号须为 0~7")
	}
	return encodeWithMask(base, mask), nil
}

type qrBase struct {
	m     *matrix
	final []byte // 数据+纠错码字序列
}

func encodeWithMask(base *qrBase, mask int) [][]bool {
	m := base.m
	placeData(m, base.final)
	applyMask(m, mask)
	drawFormat(m, mask)
	return m.grid
}

func maskPenalty(base *qrBase, mask int) int {
	m := base.m
	placeData(m, base.final)
	applyMask(m, mask)
	drawFormat(m, mask)
	p := penalty(m)
	applyMask(m, mask)
	return p
}

// encodeBase 构造码字序列与基础矩阵（功能图案就位、数据未放置）。
func encodeBase(text string) (*qrBase, error) {
	data := []byte(text)
	// 选版本（容量 = 数据码字 - 模式/计数的 2 字节开销）
	ver := -1
	for i, v := range versions {
		if len(data) <= v.dataPerBlock*v.blocks-2 {
			ver = i
			break
		}
	}
	if ver < 0 {
		return nil, errors.New("文本过长（QR 编码器支持版本 1~6，约 106 字节）")
	}
	v := versions[ver]

	// ---- 位流：模式 0100 + 8 位长度 + 数据 + 终止符 + 填充 ----
	var bits []bool
	appendBit := func(b bool) { bits = append(bits, b) }
	appendBits := func(val, n int) {
		for i := n - 1; i >= 0; i-- {
			appendBit(val>>uint(i)&1 == 1)
		}
	}
	appendBits(4, 4)
	appendBits(len(data), 8)
	for _, b := range data {
		appendBits(int(b), 8)
	}
	dataBitsCap := v.dataPerBlock * v.blocks * 8
	// 终止符最多 4 个 0
	for i := 0; i < 4 && len(bits) < dataBitsCap; i++ {
		appendBit(false)
	}
	// 对齐到字节
	for len(bits)%8 != 0 {
		appendBit(false)
	}
	// 填充码字 0xEC 0x11
	pad := []byte{0xEC, 0x11}
	pi := 0
	for len(bits) < dataBitsCap {
		appendBits(int(pad[pi%2]), 8)
		pi++
	}

	// 位流转码字
	dataCW := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b {
			dataCW[i/8] |= 1 << uint(7-i%8)
		}
	}

	// ---- 分块 RS 纠错 + 交错 ----
	numBlocks := v.blocks
	blocks := make([][]byte, numBlocks)
	ecBlocks := make([][]byte, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blk := dataCW[i*v.dataPerBlock : (i+1)*v.dataPerBlock]
		blocks[i] = blk
		ecBlocks[i] = rsEncode(blk, v.ecPerBlock)
	}
	var final []byte
	for c := 0; c < v.dataPerBlock; c++ {
		for i := 0; i < numBlocks; i++ {
			final = append(final, blocks[i][c])
		}
	}
	for c := 0; c < v.ecPerBlock; c++ {
		for i := 0; i < numBlocks; i++ {
			final = append(final, ecBlocks[i][c])
		}
	}

	size := 17 + 4*(ver+1)
	m := newMatrix(size)
	placeFunctionPatterns(m, ver+1)
	return &qrBase{m: m, final: final}, nil
}

// placeData 以之字形放置数据码字（可重复调用，覆盖全部非功能模块）。
func placeData(m *matrix, final []byte) {
	size := m.size
	bitIdx := 0
	totalBits := len(final) * 8
	getBit := func(i int) bool { return final[i/8]>>(uint(7-i%8))&1 == 1 }
	right := size - 1
	for right > 0 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := (right+1)&2 == 0
				var y int
				if upward {
					y = size - 1 - vert
				} else {
					y = vert
				}
				if !m.function[y][x] {
					var dark bool
					if bitIdx < totalBits {
						dark = getBit(bitIdx)
					}
					m.grid[y][x] = dark
					bitIdx++
				}
			}
		}
		right -= 2
	}
}

// ---- 矩阵与功能图案 ----

type matrix struct {
	size     int
	grid     [][]bool // 模块颜色
	function [][]bool // 是否功能模块（数据/掩码不可触碰）
}

func newMatrix(size int) *matrix {
	m := &matrix{size: size}
	m.grid = make([][]bool, size)
	m.function = make([][]bool, size)
	for i := range m.grid {
		m.grid[i] = make([]bool, size)
		m.function[i] = make([]bool, size)
	}
	return m
}

func (m *matrix) set(y, x int, dark bool) {
	m.grid[y][x] = dark
	m.function[y][x] = true
}

func (m *matrix) setFinder(y, x int) {
	// 7x7 定位图案：黑框内白、中心 3x3 黑
	for dy := -1; dy <= 7; dy++ {
		for dx := -1; dx <= 7; dx++ {
			ry, rx := y+dy, x+dx
			if ry < 0 || ry >= m.size || rx < 0 || rx >= m.size {
				continue
			}
			dark := (0 <= dy && dy <= 6 && (dx == 0 || dx == 6)) ||
				(0 <= dx && dx <= 6 && (dy == 0 || dy == 6)) ||
				(2 <= dy && dy <= 4 && 2 <= dx && dx <= 4)
			m.set(ry, rx, dark)
		}
	}
}

func (m *matrix) setAlignment(y, x int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			dark := dy == -2 || dy == 2 || dx == -2 || dx == 2 || (dy == 0 && dx == 0)
			m.set(y+dy, x+dx, dark)
		}
	}
}

func placeFunctionPatterns(m *matrix, ver int) {
	size := m.size
	// 定位图案（含分隔留白由 setFinder 的 -1..7 覆盖）
	m.setFinder(0, 0)
	m.setFinder(0, size-7)
	m.setFinder(size-7, 0)
	// 时序线
	for i := 8; i < size-8; i++ {
		dark := i%2 == 0
		if !m.function[6][i] {
			m.set(6, i, dark)
		}
		if !m.function[i][6] {
			m.set(i, 6, dark)
		}
	}
	// 对齐图案
	if pos := alignmentPos[ver-1]; pos != nil {
		for _, r := range pos {
			for _, c := range pos {
				tl := r <= 8 && c <= 8
				tr := r <= 8 && c >= size-9
				bl := r >= size-9 && c <= 8
				if tl || tr || bl {
					continue
				}
				m.setAlignment(r, c)
			}
		}
	}
	// 格式信息预留区（内容最后写入）
	for i := 0; i <= 8; i++ {
		if i != 6 {
			m.set(8, i, false)
			m.set(i, 8, false)
		}
	}
	for i := 0; i < 8; i++ {
		m.set(8, size-1-i, false)
		m.set(size-1-i, 8, false)
	}
	// 固定黑模块
	m.set(size-8, 8, true)
}

// drawFormat 写入 15 位格式信息（级别 M + 掩码号，BCH(15,5) + 掩码 0x5412）。
// 布局与主流实现一致（位 0 = LSB）。
func drawFormat(m *matrix, mask int) {
	const bchGen = 0x537  // BCH(15,5) 生成多项式
	fmtVal := 0<<3 | mask // 级别 M = 00
	// (fmtVal << 10) 除以生成多项式取 10 位余数
	data := fmtVal << 10
	for i := 14; i >= 10; i-- {
		if data&(1<<uint(i)) != 0 {
			data ^= bchGen << uint(i-10)
		}
	}
	bits := ((fmtVal << 10) | (data & 0x3FF)) ^ 0x5412
	// 放置顺序按 MSB 在前（位 0 = 整数最高位）
	get := func(i int) bool { return bits>>(uint(14-i))&1 == 1 }

	// 第一份
	m.grid[8][0] = get(0)
	m.grid[8][1] = get(1)
	m.grid[8][2] = get(2)
	m.grid[8][3] = get(3)
	m.grid[8][4] = get(4)
	m.grid[8][5] = get(5)
	m.grid[8][7] = get(6)
	m.grid[8][8] = get(7)
	m.grid[7][8] = get(8)
	m.grid[5][8] = get(9)
	m.grid[4][8] = get(10)
	m.grid[3][8] = get(11)
	m.grid[2][8] = get(12)
	m.grid[1][8] = get(13)
	m.grid[0][8] = get(14)
	// 第二份：列 8 底部 7 格 + 固定黑模块 + 行 8 右侧 8 格
	for i := 0; i < 7; i++ {
		m.grid[m.size-1-i][8] = get(i)
	}
	for i := 7; i < 15; i++ {
		m.grid[8][m.size-15+i] = get(i)
	}
	m.grid[m.size-8][8] = true
}

// ---- 掩码与惩罚 ----

func applyMask(m *matrix, mask int) {
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size; x++ {
			if m.function[y][x] {
				continue
			}
			if maskBit(mask, x, y) {
				m.grid[y][x] = !m.grid[y][x]
			}
		}
	}
}

func maskBit(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	default:
		return ((x+y)%2+(x*y)%3)%2 == 0
	}
}

func penalty(m *matrix) int {
	s := m.size
	score := 0
	// 规则 1：行/列连续同色 ≥5
	for y := 0; y < s; y++ {
		runX, runY := 1, 1
		for x := 1; x < s; x++ {
			if m.grid[y][x] == m.grid[y][x-1] {
				runX++
			} else {
				if runX >= 5 {
					score += runX - 2
				}
				runX = 1
			}
			if m.grid[x][y] == m.grid[x-1][y] {
				runY++
			} else {
				if runY >= 5 {
					score += runY - 2
				}
				runY = 1
			}
		}
		if runX >= 5 {
			score += runX - 2
		}
		if runY >= 5 {
			score += runY - 2
		}
	}
	// 规则 2：2x2 同色块
	for y := 0; y < s-1; y++ {
		for x := 0; x < s-1; x++ {
			c := m.grid[y][x]
			if c == m.grid[y][x+1] && c == m.grid[y+1][x] && c == m.grid[y+1][x+1] {
				score += 3
			}
		}
	}
	// 规则 3：1011101 两侧带 ≥4 个 0 的定位样式（越界按浅色处理）
	pat := []bool{true, false, true, true, true, false, true}
	at := func(y, x int) bool {
		if y < 0 || y >= s || x < 0 || x >= s {
			return false
		}
		return m.grid[y][x]
	}
	check := func(get func(int, int) bool) {
		for y := 0; y < s; y++ {
			for x := 0; x < s; x++ {
				// 尝试以 x 为图案起点，检查 [x, x+6] 及两侧 4 格
				ok := true
				for k := 0; k < 7; k++ {
					if get(y, x+k) != pat[k] {
						ok = false
						break
					}
				}
				if !ok {
					continue
				}
				left := true
				for k := 1; k <= 4; k++ {
					if get(y, x-k) {
						left = false
						break
					}
				}
				right := true
				for k := 0; k < 4; k++ {
					if get(y, x+7+k) {
						right = false
						break
					}
				}
				if left || right {
					score += 40
				}
			}
		}
	}
	check(func(y, x int) bool { return at(y, x) })
	check(func(y, x int) bool { return at(x, y) })
	// 规则 4：黑白比例
	dark := 0
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			if m.grid[y][x] {
				dark++
			}
		}
	}
	total := s * s
	percent := dark * 100 / total
	d := percent%10*10 - 50
	if d < 0 {
		d = -d
	}
	score += d / 5 * 10
	return score
}

// PNG 把二维码渲染为图像（含 4 模块静区）。
func PNG(text string, scale int) (image.Image, error) {
	grid, err := Encode(text)
	if err != nil {
		return nil, err
	}
	if scale <= 0 {
		scale = 8
	}
	const quiet = 4
	n := len(grid)
	dim := (n + quiet*2) * scale
	img := image.NewRGBA(image.Rect(0, 0, dim, dim))
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	for y := 0; y < dim; y++ {
		for x := 0; x < dim; x++ {
			img.Set(x, y, white)
		}
	}
	for gy := 0; gy < n; gy++ {
		for gx := 0; gx < n; gx++ {
			if !grid[gy][gx] {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set((gx+quiet)*scale+dx, (gy+quiet)*scale+dy, black)
				}
			}
		}
	}
	return img, nil
}
