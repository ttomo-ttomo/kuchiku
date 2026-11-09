package main

// Markdown を「削除できる単位（unit）」に分ける。
// - 段落は文ごと、リストは項目ごと、それ以外（コード・表・引用・:::ブロックなど）は塊ごと
// - 見出しごとに section を作る（s0 は最初の見出しより前）
// - frontmatter と <!-- kuchiku:keep --> を含む節はロックする

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	reFence     = regexp.MustCompile("^(\\s*)(`{3,}|~{3,})")
	reContainer = regexp.MustCompile(`^(:{3,})\S`)
	reHeading   = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	reListItem  = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+`)
	reEmbed     = regexp.MustCompile(`^\s*(https?://\S+|@\[[^\]]*\]\([^)]*\)|!\[[^\]]*\]\([^)]*\)(\s*\*[^*]*\*)?)\s*$`)
	reImage     = regexp.MustCompile(`^\s*!\[[^\]]*\]\([^)]*\)`)
	reCaption   = regexp.MustCompile(`^\s*[*_][^\s*_]`)
)

const keepMarker = "kuchiku:keep"

type Unit struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Block     string   `json:"block"`
	Section   string   `json:"section"`
	StartLine int      `json:"startLine"`
	EndLine   int      `json:"endLine"`
	Col       [2]int   `json:"col"`
	Text      string   `json:"text"`
	Locked    bool     `json:"locked"`
	Level     int      `json:"level,omitempty"`
	Indent    int      `json:"indent,omitempty"`
	Parent    string   `json:"parent,omitempty"`
	Children  []string `json:"children,omitempty"`
}

type Block struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Section   string `json:"section"`
	Level     int    `json:"level,omitempty"`
}

type Section struct {
	ID         string   `json:"id"`
	Heading    string   `json:"heading"`
	Level      int      `json:"level"`
	Parent     string   `json:"parent"`
	UnitIDs    []string `json:"unitIds"`
	ChildIDs   []string `json:"childIds"`
	Locked     bool     `json:"locked"`
	KeepMarker bool     `json:"keepMarker,omitempty"`
}

type Doc struct {
	Eol      string     `json:"eol"`
	Lines    []string   `json:"lines"`
	Blocks   []*Block   `json:"blocks"`
	Units    []*Unit    `json:"units"`
	Sections []*Section `json:"sections"`
}

func (d *Doc) unitByID() map[string]*Unit {
	m := make(map[string]*Unit, len(d.Units))
	for _, u := range d.Units {
		m[u.ID] = u
	}
	return m
}

func (d *Doc) sectionByID() map[string]*Section {
	m := make(map[string]*Section, len(d.Sections))
	for _, s := range d.Sections {
		m[s.ID] = s
	}
	return m
}

func startsBlock(line string) bool {
	t := trimStart(line)
	return reFence.MatchString(line) || reContainer.MatchString(line) || reHeading.MatchString(line) ||
		strings.HasPrefix(t, "|") || strings.HasPrefix(t, ">") || strings.TrimSpace(line) == "$$" ||
		strings.HasPrefix(t, "<!--") || reListItem.MatchString(line)
}

func closeFence(lines []string, i int, marker string, indentLen int) int {
	ch := regexp.QuoteMeta(marker[:1])
	re := regexp.MustCompile(fmt.Sprintf(`^\s{0,%d}%s{%d,}\s*$`, indentLen+3, ch, len(marker)))
	j := i + 1
	for j < len(lines) && !re.MatchString(lines[j]) {
		j++
	}
	return min(j, len(lines)-1)
}

func segment(source string) *Doc {
	eol := "\n"
	text := source
	if strings.Contains(source, "\r\n") {
		eol = "\r\n"
		text = strings.ReplaceAll(source, "\r\n", "\n")
	}
	lines := strings.Split(text, "\n")
	doc := &Doc{Eol: eol, Lines: lines}
	s0 := &Section{ID: "s0", UnitIDs: []string{}, ChildIDs: []string{}}
	doc.Sections = []*Section{s0}
	stack := []*Section{s0}
	cur := func() *Section { return stack[len(stack)-1] }

	addUnit := func(u *Unit) *Unit {
		u.ID = fmt.Sprintf("u%d", len(doc.Units)+1)
		u.Section = cur().ID
		doc.Units = append(doc.Units, u)
		cur().UnitIDs = append(cur().UnitIDs, u.ID)
		return u
	}
	addBlock := func(kind string, s, e, level int) *Block {
		b := &Block{ID: fmt.Sprintf("b%d", len(doc.Blocks)+1), Kind: kind, StartLine: s, EndLine: e, Section: cur().ID, Level: level}
		doc.Blocks = append(doc.Blocks, b)
		return b
	}
	raw := func(s, e int) string { return strings.Join(lines[s:e+1], "\n") }
	block := func(kind string, s, e int, locked bool) {
		b := addBlock(kind, s, e, 0)
		addUnit(&Unit{Kind: kind, Block: b.ID, StartLine: s, EndLine: e, Text: raw(s, e), Locked: locked})
	}

	i := 0
	if lines[0] == "---" {
		j := 1
		for j < len(lines) && lines[j] != "---" {
			j++
		}
		if j < len(lines) {
			block("frontmatter", 0, j, true)
			i = j + 1
		}
	}

	for i < len(lines) {
		line := lines[i]
		if isBlank(line) {
			i++
			continue
		}
		if fm := reFence.FindStringSubmatch(line); fm != nil {
			end := closeFence(lines, i, fm[2], len(fm[1]))
			block("code", i, end, false)
			i = end + 1
			continue
		}
		if cm := reContainer.FindStringSubmatch(line); cm != nil {
			closeRe := regexp.MustCompile(fmt.Sprintf(`^:{%d}\s*$`, len(cm[1])))
			j := i + 1
			for j < len(lines) && !closeRe.MatchString(lines[j]) {
				j++
			}
			end := min(j, len(lines)-1)
			block("container", i, end, false)
			i = end + 1
			continue
		}
		if strings.TrimSpace(line) == "$$" {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) != "$$" {
				j++
			}
			end := min(j, len(lines)-1)
			block("math", i, end, false)
			i = end + 1
			continue
		}
		if strings.HasPrefix(trimStart(line), "<!--") {
			j := i
			for j < len(lines) && !strings.Contains(lines[j], "-->") {
				j++
			}
			end := min(j, len(lines)-1)
			body := raw(i, end)
			block("comment", i, end, false)
			if strings.Contains(body, keepMarker) {
				cur().KeepMarker = true
			}
			i = end + 1
			continue
		}
		if hm := reHeading.FindStringSubmatch(line); hm != nil {
			level := len(hm[1])
			for len(stack) > 1 && stack[len(stack)-1].Level >= level {
				stack = stack[:len(stack)-1]
			}
			parent := cur()
			sec := &Section{ID: fmt.Sprintf("s%d", len(doc.Sections)), Heading: hm[2], Level: level, Parent: parent.ID, UnitIDs: []string{}, ChildIDs: []string{}}
			doc.Sections = append(doc.Sections, sec)
			parent.ChildIDs = append(parent.ChildIDs, sec.ID)
			stack = append(stack, sec)
			b := addBlock("heading", i, i, level)
			addUnit(&Unit{Kind: "heading", Block: b.ID, StartLine: i, EndLine: i, Text: line, Level: level})
			i++
			continue
		}
		if t := trimStart(line); strings.HasPrefix(t, "|") || strings.HasPrefix(t, ">") {
			prefix := t[:1]
			kind := "quote"
			if prefix == "|" {
				kind = "table"
			}
			j := i
			for j+1 < len(lines) && strings.HasPrefix(trimStart(lines[j+1]), prefix) {
				j++
			}
			block(kind, i, j, false)
			i = j + 1
			continue
		}
		if reListItem.MatchString(line) {
			b := addBlock("list", i, i, 0)
			var items []*Unit
			var curItem *Unit
			j := i
			for j < len(lines) && !isBlank(lines[j]) {
				l := lines[j]
				if im := reListItem.FindStringSubmatch(l); im != nil {
					indent := len(im[1])
					var parent *Unit
					for k := len(items) - 1; k >= 0; k-- {
						if items[k].Indent < indent {
							parent = items[k]
							break
						}
					}
					curItem = addUnit(&Unit{Kind: "item", Block: b.ID, StartLine: j, EndLine: j, Indent: indent})
					if parent != nil {
						curItem.Parent = parent.ID
						parent.Children = append(parent.Children, curItem.ID)
					}
					items = append(items, curItem)
					j++
					continue
				}
				if ifm := reFence.FindStringSubmatch(l); ifm != nil && curItem != nil {
					end := closeFence(lines, j, ifm[2], len(ifm[1]))
					curItem.EndLine = end
					j = end + 1
					continue
				}
				if startsBlock(l) && !(len(l) > 0 && (l[0] == ' ' || l[0] == '\t' || l[0] == '\n' || l[0] == '\r' || l[0] == '\f' || l[0] == '\v')) {
					break
				}
				if curItem != nil {
					curItem.EndLine = j
				}
				j++
			}
			for _, it := range items {
				it.Text = raw(it.StartLine, it.EndLine)
			}
			b.EndLine = j - 1
			i = j
			continue
		}
		if reImage.MatchString(line) {
			j := i
			for j+1 < len(lines) && reCaption.MatchString(lines[j+1]) && !reListItem.MatchString(lines[j+1]) {
				j++
			}
			if j > i || reEmbed.MatchString(line) {
				block("figure", i, j, false)
				i = j + 1
				continue
			}
		}
		if reEmbed.MatchString(line) && (i+1 >= len(lines) || isBlank(lines[i+1]) || startsBlock(lines[i+1])) {
			block("embed", i, i, false)
			i++
			continue
		}
		// 段落: 空行か別ブロックの開始まで。1行ずつ文に分ける
		j := i
		for j+1 < len(lines) && !isBlank(lines[j+1]) && !startsBlock(lines[j+1]) {
			j++
		}
		b := addBlock("para", i, j, 0)
		for k := i; k <= j; k++ {
			rs := []rune(lines[k])
			for _, sp := range splitSentences(lines[k]) {
				addUnit(&Unit{Kind: "sentence", Block: b.ID, StartLine: k, EndLine: k, Col: sp, Text: strings.TrimRight(string(rs[sp[0]:sp[1]]), " \t")})
			}
		}
		i = j + 1
	}

	// keepマーカーのある節（とその子節）をロック
	byID := doc.sectionByID()
	var lockTree func(s *Section)
	lockTree = func(s *Section) {
		s.Locked = true
		for _, c := range s.ChildIDs {
			lockTree(byID[c])
		}
	}
	for _, s := range doc.Sections {
		if s.KeepMarker && s.ID != "s0" {
			lockTree(s)
		}
	}
	if s0.KeepMarker {
		s0.Locked = true
	}
	for _, u := range doc.Units {
		if byID[u.Section].Locked {
			u.Locked = true
		}
	}
	return doc
}

// sectionUnitIDs は節と子孫節に含まれる unit id（見出しを含む）
func sectionUnitIDs(doc *Doc, id string) []string {
	byID := doc.sectionByID()
	var out []string
	var walk func(string)
	walk = func(id string) {
		s := byID[id]
		out = append(out, s.UnitIDs...)
		for _, c := range s.ChildIDs {
			walk(c)
		}
	}
	walk(id)
	return out
}

func unitNum(id string) int { n, _ := strconv.Atoi(id[1:]); return n }

func sortIDs(ids []string) {
	sort.Slice(ids, func(a, b int) bool { return unitNum(ids[a]) < unitNum(ids[b]) })
}

// bodyText は本文（frontmatterとコメントを除く）の文字列
func bodyText(doc *Doc) string {
	var parts []string
	for _, u := range doc.Units {
		if u.Kind != "frontmatter" && u.Kind != "comment" {
			parts = append(parts, u.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// render は指定した unit を消した文字列を作る。消すのは削除だけで、残す部分の文字は一切変えない。
// - 文は行内の範囲を消し、行が空になったら行ごと消す
// - ブロックが丸ごと消えたら、直後の空行も1つ分消す
func render(doc *Doc, deleteIDs []string) string {
	del := map[string]bool{}
	for _, id := range deleteIDs {
		del[id] = true
	}
	lines := doc.Lines
	lineOut := append([]string(nil), lines...)
	drop := make([]bool, len(lines))
	byLine := map[int][][2]int{}
	var order []int
	for _, u := range doc.Units {
		if !del[u.ID] {
			continue
		}
		if u.Kind == "sentence" {
			if _, ok := byLine[u.StartLine]; !ok {
				order = append(order, u.StartLine)
			}
			byLine[u.StartLine] = append(byLine[u.StartLine], u.Col)
		} else {
			for k := u.StartLine; k <= u.EndLine; k++ {
				drop[k] = true
			}
		}
	}
	for _, ln := range order {
		cols := byLine[ln]
		sort.Slice(cols, func(a, b int) bool { return cols[a][0] > cols[b][0] })
		orig := lines[ln]
		rs := []rune(orig)
		for _, c := range cols {
			rs = append(append([]rune(nil), rs[:c[0]]...), rs[c[1]:]...)
		}
		s := string(rs)
		if strings.TrimSpace(s) == "" {
			drop[ln] = true
		} else {
			trail := orig[len(strings.TrimRight(orig, " \t")):]
			lineOut[ln] = strings.TrimRight(s, " \t") + trail
		}
	}

	blockEmpty := func(b *Block) bool {
		for k := b.StartLine; k <= b.EndLine; k++ {
			if !drop[k] && strings.TrimSpace(lines[k]) != "" {
				return false
			}
		}
		return true
	}
	for bi, b := range doc.Blocks {
		if !blockEmpty(b) {
			continue
		}
		for k := b.StartLine; k <= b.EndLine; k++ {
			drop[k] = true
		}
		var next *Block
		gapEnd := len(lines)
		if bi+1 < len(doc.Blocks) {
			next = doc.Blocks[bi+1]
			gapEnd = next.StartLine
		}
		dropped := false
		for k := b.EndLine + 1; k < gapEnd; k++ {
			if strings.TrimSpace(lines[k]) == "" && !dropped {
				drop[k] = true
				dropped = true
			}
		}
		// 最後のブロックなら、手前の空行を1つ詰める
		if next == nil && !dropped {
			if k := b.StartLine - 1; k >= 0 && strings.TrimSpace(lines[k]) == "" {
				drop[k] = true
			}
		}
	}
	var kept []string
	for k, l := range lineOut {
		if !drop[k] {
			kept = append(kept, l)
		}
	}
	out := strings.Join(kept, "\n")
	if doc.Eol == "\r\n" {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out
}
