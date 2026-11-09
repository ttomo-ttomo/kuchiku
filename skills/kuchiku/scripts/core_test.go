package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const fixDir = "testdata"

func read(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixDir, f))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func texts(line string) []string {
	rs := []rune(line)
	out := []string{}
	for _, sp := range splitSentences(line) {
		out = append(out, strings.TrimSpace(string(rs[sp[0]:sp[1]])))
	}
	return out
}

func unitByText(doc *Doc, s string) *Unit {
	for _, u := range doc.Units {
		if strings.Contains(u.Text, s) {
			return u
		}
	}
	return nil
}

func TestSplitSentences(t *testing.T) {
	cases := map[string][]string{
		"一文目。二文目！三文目？":                 {"一文目。", "二文目！", "三文目？"},
		"彼は「はい。わかった」と言った。次。":           {"彼は「はい。わかった」と言った。", "次。"},
		"`a。b` を使う。次。":                 {"`a。b` を使う。", "次。"},
		"**強調。強調の続き。**":                {"**強調。強調の続き。**"},
		"URL は https://x.com/?q=1 です。": {"URL は https://x.com/?q=1 です。"},
		"本当に! 次へ。":                     {"本当に!", "次へ。"},
		"文末なしの文":                       {"文末なしの文"},
	}
	for in, want := range cases {
		if got := texts(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestRenderRoundTrip(t *testing.T) {
	for _, f := range []string{"ai-sample.md", "edge.md"} {
		src := read(t, f)
		if got := render(segment(src), nil); got != src {
			t.Errorf("%s: 削除なしの描き直しが原文と違う", f)
		}
	}
	crlf := strings.ReplaceAll(read(t, "ai-sample.md"), "\n", "\r\n")
	if render(segment(crlf), nil) != crlf {
		t.Error("CRLF")
	}
}

func TestRenderPartial(t *testing.T) {
	doc := segment("A です。B です。C です。\n")
	if got := render(doc, []string{unitByText(doc, "B です").ID}); got != "A です。C です。\n" {
		t.Errorf("got %q", got)
	}
	src := "# 見出し\n\n一段落目。\n\n二段落目。\n\n三段落目。\n"
	doc = segment(src)
	if got := render(doc, []string{unitByText(doc, "二段落目").ID}); got != "# 見出し\n\n一段落目。\n\n三段落目。\n" {
		t.Errorf("got %q", got)
	}
	if got := render(doc, []string{unitByText(doc, "三段落目").ID}); got != "# 見出し\n\n一段落目。\n\n二段落目。\n" {
		t.Errorf("got %q", got)
	}
}

func TestSegmentBlocks(t *testing.T) {
	doc := segment(read(t, "edge.md"))
	count := func(k string) (n int) {
		for _, u := range doc.Units {
			if u.Kind == k {
				n++
			}
		}
		return
	}
	for k, want := range map[string]int{"container": 2, "figure": 1, "table": 1, "math": 1} {
		if count(k) != want {
			t.Errorf("%s: %d", k, count(k))
		}
	}
	if !strings.HasPrefix(unitByText(doc, "npm i").Text, "1. インストールする") {
		t.Error("リスト内のコードは項目に含める")
	}
	if !doc.Units[0].Locked || doc.Units[0].Kind != "frontmatter" {
		t.Error("frontmatter")
	}
	if !unitByText(doc, "この節はロックされる").Locked || !unitByText(doc, "親がロックされているので").Locked {
		t.Error("keep の節と子の節はロック")
	}
	if unitByText(doc, "URL の中の").Locked {
		t.Error("ロックされないはず")
	}
}

func TestVerify(t *testing.T) {
	src := read(t, "ai-sample.md")
	doc := segment(src)
	var ids []string
	re := regexp.MustCompile(`いかがでしたか|参考になれば`)
	for _, u := range doc.Units {
		if re.MatchString(u.Text) {
			ids = append(ids, u.ID)
		}
	}
	if !verify(src, render(doc, ids)).OK {
		t.Error("削除だけなら OK")
	}
	first := func(changed string) string {
		r := verify(src, changed)
		if r.OK {
			return "ok"
		}
		return r.Problems[0].Type
	}
	if got := first(strings.Replace(src, "約 40 秒", "約 30 秒", 1)); got != "added_or_changed" {
		t.Error(got)
	}
	if got := first(strings.Replace(src, "参考になれば幸いです。", "参考になれば幸いです。ぜひ試してください。", 1)); got == "ok" {
		t.Error("追加は NG")
	}
	swapped := strings.Replace(strings.Replace(src, "Git 2.5 で追加されました。\n", "", 1), "## git worktree のメリット", "Git 2.5 で追加されました。\n\n## git worktree のメリット", 1)
	if got := first(swapped); got != "reordered" {
		t.Error(got)
	}
	if got := first(strings.Replace(src, "published: false", "published: true", 1)); got != "frontmatter" {
		t.Error(got)
	}
	edge := read(t, "edge.md")
	r := verify(edge, strings.Replace(edge, "この節はロックされる。", "", 1))
	found := false
	for _, p := range r.Problems {
		found = found || p.Type == "locked_removed"
	}
	if !found {
		t.Error("ロックされた単位の削除は NG")
	}
}

func planFor(doc *Doc, cuts []PlanCut) *Plan {
	return &Plan{Promise: "主眼", PromiseConfirmed: true, Structure: []StructItem{{doc.Sections[1].ID, "B"}}, Cuts: cuts}
}

func cut(id int, targets ...string) PlanCut {
	f := false
	return PlanCut{ID: id, Targets: targets, Category: "filler", Reason: "理由", BreaksRepro: &f, ChangesConclusion: &f}
}

func errsOf(p *Plan, doc *Doc, h *Hints) string {
	e, _, _ := expandPlan(p, doc, h)
	return strings.Join(e, ",")
}

func TestPlanRejects(t *testing.T) {
	doc := segment(read(t, "ai-sample.md"))
	h := analyze(doc, loadConfig())
	u := unitByText(doc, "いかがでしたか").ID
	tr := true

	p := planFor(doc, []PlanCut{cut(1, u)})
	p.Promise = ""
	check := func(p *Plan, want string) {
		t.Helper()
		if got := errsOf(p, doc, h); !strings.Contains(got, want) {
			t.Errorf("want %q in %q", want, got)
		}
	}
	check(p, "promise（主眼の一文）")
	c := cut(1, u)
	c.BreaksRepro = &tr
	check(planFor(doc, []PlanCut{c}), "breaks_repro")
	c = cut(1, u)
	c.ChangesConclusion = nil
	check(planFor(doc, []PlanCut{c}), "changes_conclusion")
	check(planFor(doc, []PlanCut{cut(1, u), cut(2, u)}), "重複")
	check(planFor(doc, []PlanCut{cut(1, "u999")}), "存在しない")
	c = cut(1, u)
	c.Category = "other"
	check(planFor(doc, []PlanCut{c}), "category")
	check(planFor(doc, []PlanCut{cut(1, "u1")}), "frontmatter")
	if got := errsOf(planFor(doc, []PlanCut{cut(1, u)}), doc, h); got != "" {
		t.Errorf("通るはずの案: %s", got)
	}
}

func TestPlanPriorityA(t *testing.T) {
	doc := segment(read(t, "ai-sample.md"))
	h := analyze(doc, loadConfig())
	s := doc.Sections[1]
	a := func(cuts ...PlanCut) *Plan {
		p := planFor(doc, cuts)
		p.Structure = []StructItem{{s.ID, "A"}}
		return p
	}
	for _, p := range []*Plan{a(cut(1, s.ID)), a(cut(1, s.UnitIDs[len(s.UnitIDs)-1]))} {
		if got := errsOf(p, doc, h); !strings.Contains(got, "優先度A") {
			t.Errorf("Aの節を消す案は弾く: %s", got)
		}
	}
	p := planFor(doc, nil)
	p.Structure = nil
	p.Cuts = []PlanCut{}
	if got := errsOf(p, doc, h); !strings.Contains(got, "structure") {
		t.Errorf("structure がない: %s", got)
	}
}

func TestPlanExpand(t *testing.T) {
	src := read(t, "edge.md")
	doc := segment(src)
	h := analyze(doc, loadConfig())
	parent := unitByText(doc, "2. 設定する").ID
	e, _, cuts := expandPlan(planFor(doc, []PlanCut{cut(1, parent)}), doc, h)
	if len(e) > 0 || len(cuts[0].UnitIDs) != 3 {
		t.Fatalf("親の項目は子の項目まで展開: %v %v", e, cuts)
	}
	if !verify(src, render(doc, cuts[0].UnitIDs)).OK {
		t.Error("展開後も部分列")
	}
	if got := errsOf(planFor(doc, []PlanCut{cut(1, "s2")}), doc, h); !strings.Contains(got, "ロック") {
		t.Error(got)
	}
	var s1 *Section
	for _, s := range doc.Sections {
		if s.Heading == "リストとコード" {
			s1 = s
		}
	}
	var body []string
	for _, id := range s1.UnitIDs {
		if doc.unitByID()[id].Kind != "heading" {
			body = append(body, id)
		}
	}
	_, w, _ := expandPlan(planFor(doc, []PlanCut{cut(1, body...)}), doc, h)
	if !strings.Contains(strings.Join(w, ","), "見出しだけ残る") {
		t.Error("見出しだけ残る警告")
	}
}

func TestAnalyze(t *testing.T) {
	doc := segment(read(t, "ai-sample.md"))
	h := analyze(doc, loadConfig())
	has := func(text, prefix string) bool {
		for _, f := range h.Units[unitByText(doc, text).ID] {
			if strings.HasPrefix(f, prefix) {
				return true
			}
		}
		return false
	}
	if !has("本記事では、git worktree を活用", "stock:preamble") || !has("いかがでしたか", "stock:closing") || !has("- 同じブランチを 2 つ", "repeat_of") {
		t.Errorf("目印が付かない: %v", h.Units)
	}
	for _, s := range h.Sections {
		if s.Heading == "まとめ" {
			for _, f := range s.Flags {
				if f == "summary_like" {
					return
				}
			}
		}
	}
	t.Error("まとめの節に summary_like が付かない")
}

func TestStructureSheet(t *testing.T) {
	sheet := "主眼: （項目を出し終えてから書く）\n\n**主眼: 清書した一文。**\n\n| 番号 | 章 | 優先度 |\n|:-:|---|:-:|\n| 1 | まとめ | B |\n| 2 | 結果 | **A** |\n"
	if got := extractPromise(sheet); got != "清書した一文。" {
		t.Error(got)
	}
	want := []Priority{{1, "まとめ", "B"}, {2, "結果", "A"}}
	if got := extractPriorities(sheet); !reflect.DeepEqual(got, want) {
		t.Error(got)
	}
}
