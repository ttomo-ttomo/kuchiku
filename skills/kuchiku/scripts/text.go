package main

// 文字列まわりの小さな道具: 文分割、正規化、類似度、情報密度

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

var openers = map[rune]rune{'「': '」', '『': '』', '（': '）', '(': ')', '【': '】', '[': ']', '〔': '〕'}

func isHard(r rune) bool { return r == '。' || r == '！' || r == '？' }
func isCloserAfter(r rune) bool {
	return strings.ContainsRune("」』）)】〕*_~\"”’", r)
}

func trimStart(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) }
func isBlank(s string) bool     { return strings.TrimSpace(s) == "" }

var (
	reBold   = regexp.MustCompile(`\*\*`)
	reStrike = regexp.MustCompile(`~~`)
	reCode   = regexp.MustCompile("`[^`]*`")
	reStar   = regexp.MustCompile(`\*`)
)

// splitSentences は1行を文に分け、行内の [start, end)（rune位置）を返す。
// 括弧・インラインコード・リンクの中では区切らない。end には直後の空白も含める。
func splitSentences(line string) [][2]int {
	rs := []rune(line)
	var spans [][2]int
	var stack []rune
	inCode := false
	start := 0
	for start < len(rs) && (rs[start] == ' ' || rs[start] == '\t') {
		start++
	}
	i := start
	for i < len(rs) {
		ch := rs[i]
		if ch == '`' {
			inCode = !inCode
			i++
			continue
		}
		if inCode {
			i++
			continue
		}
		if c, ok := openers[ch]; ok {
			stack = append(stack, c)
			i++
			continue
		}
		if len(stack) > 0 && ch == stack[len(stack)-1] {
			stack = stack[:len(stack)-1]
			i++
			continue
		}
		hard := isHard(ch)
		// 半角の ! ? は、直前が日本語で、直後が空白・行末・閉じ記号のときだけ文末とみなす（URLや英文を切らない）
		soft := (ch == '!' || ch == '?') && i > 0 && rs[i-1] > 127 &&
			!(i+1 < len(rs) && rs[i+1] == '[') &&
			(i+1 >= len(rs) || unicode.IsSpace(rs[i+1]) || strings.ContainsRune("」』）)*_\"”", rs[i+1]))
		if len(stack) == 0 && (hard || soft) {
			end := i + 1
			for end < len(rs) && (isHard(rs[end]) || isCloserAfter(rs[end])) {
				end++
			}
			for end < len(rs) && (rs[end] == ' ' || rs[end] == '\t') {
				end++
			}
			spans = append(spans, [2]int{start, end})
			start, i = end, end
			continue
		}
		i++
	}
	if start < len(rs) && strings.TrimSpace(string(rs[start:])) != "" {
		spans = append(spans, [2]int{start, len(rs)})
	}
	// 強調（* / _ / ~~）が文をまたぐ行は分けない。分けると記号の対応が崩れるため
	unbalanced := false
	for _, sp := range spans {
		t := reCode.ReplaceAllString(string(rs[sp[0]:sp[1]]), "")
		bold := len(reBold.FindAllString(t, -1))
		italic := len(reStar.FindAllString(reBold.ReplaceAllString(t, ""), -1))
		strike := len(reStrike.FindAllString(t, -1))
		if bold%2 == 1 || italic%2 == 1 || strike%2 == 1 {
			unbalanced = true
		}
	}
	if unbalanced && len(spans) > 1 {
		return [][2]int{{spans[0][0], len(rs)}}
	}
	return spans
}

// norm は比較用の正規化: 前後の空白と行末の空白を落とす
func norm(s string) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(ls, "\n"))
}

var plainRes = []struct {
	re *regexp.Regexp
	to string
}{
	{regexp.MustCompile("`([^`]*)`"), "$1"},
	{regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`), "$1"},
	{regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`), "$1"},
	{regexp.MustCompile(`(?m)^\s*([-*+]|\d+[.)])\s+`), ""},
	{regexp.MustCompile(`(?m)^\s*#{1,6}\s+`), ""},
	{regexp.MustCompile(`(?m)^\s*>\s?`), ""},
	{regexp.MustCompile(`[*_~|]`), ""},
	{regexp.MustCompile(`<!--[\s\S]*?-->`), ""},
}

// plainText はMarkdown記号を落とした「読む文字」
func plainText(s string) string {
	for _, p := range plainRes {
		s = p.re.ReplaceAllString(s, p.to)
	}
	return s
}

// charCount は空白を除いた文字数
func charCount(s string) int {
	n := 0
	for _, r := range plainText(s) {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

var reBigramDrop = regexp.MustCompile(`[\s、。，．,.!?！？「」『』（）()【】:：]`)

func bigrams(s string) map[string]bool {
	t := []rune(reBigramDrop.ReplaceAllString(plainText(s), ""))
	set := map[string]bool{}
	for i := 0; i+1 < len(t); i++ {
		set[string(t[i:i+2])] = true
	}
	return set
}

// similarity は文字bigramのJaccard係数（0〜1）
func similarity(a, b string) float64 {
	A, B := bigrams(a), bigrams(b)
	if len(A) == 0 || len(B) == 0 {
		return 0
	}
	inter := 0
	for x := range A {
		if B[x] {
			inter++
		}
	}
	return float64(inter) / float64(len(A)+len(B)-inter)
}

var (
	reInfoCode = regexp.MustCompile("`[^`]+`")
	reInfoLink = regexp.MustCompile(`\]\([^)]*\)`)
	reNum      = regexp.MustCompile(`\d[\d,.]*`)
	reWord     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_+\-]*`)
	reKata     = regexp.MustCompile(`[ァ-ヴー]{3,}`)
)

// infoTokenList は情報になりやすい語（数字・英単語・インラインコード・3文字以上のカタカナ語）を取り出す。
func infoTokenList(s string) []string {
	var out []string
	for _, c := range reInfoCode.FindAllString(s, -1) {
		out = append(out, strings.ToLower(c))
	}
	rest := reInfoLink.ReplaceAllString(reInfoCode.ReplaceAllString(s, " "), "]")
	out = append(out, reNum.FindAllString(rest, -1)...)
	for _, w := range reWord.FindAllString(rest, -1) {
		out = append(out, strings.ToLower(w))
	}
	return append(out, reKata.FindAllString(rest, -1)...)
}

// density は情報語の数 / 文字数 × 100。ignore に入れた語（テーマ語）は数えない
func density(s string, ignore map[string]bool) float64 {
	n := charCount(s)
	if n == 0 {
		return 0
	}
	k := 0
	for _, t := range infoTokenList(s) {
		if !ignore[t] {
			k++
		}
	}
	return math.Round(float64(k)/float64(n)*1000) / 10
}

var reNewlines = regexp.MustCompile(`\s*\n\s*`)

func truncate(s string, n int) string {
	one := []rune(reNewlines.ReplaceAllString(s, " "))
	if len(one) > n {
		return string(one[:n-1]) + "…"
	}
	return string(one)
}
