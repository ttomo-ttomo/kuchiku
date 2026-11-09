package main

// 判断ログ（decisions.jsonl）の集計。提案パターンごとの採用率から、次回の提案方針を作る

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

type Decision struct {
	TS       string   `json:"ts"`
	Article  string   `json:"article"`
	Hash     string   `json:"hash"`
	Cut      int      `json:"cut"`
	Decision string   `json:"decision"`
	Note     *string  `json:"note"`
	Chars    int      `json:"chars"`
	Reason   string   `json:"reason"`
	Features Features `json:"features"`
}

func readDecisions(path string) []Decision {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Decision
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var d Decision
		if json.Unmarshal(sc.Bytes(), &d) == nil {
			out = append(out, d)
		}
	}
	return out
}

// latestDecisions は同じ文章・同じ案の再判断を後勝ちにまとめる
func latestDecisions(ds []Decision) []Decision {
	idx := map[string]int{}
	var out []Decision
	for _, d := range ds {
		k := fmt.Sprintf("%s#%s#%d", d.Article, d.Hash, d.Cut)
		if i, ok := idx[k]; ok {
			out[i] = d
		} else {
			idx[k] = len(out)
			out = append(out, d)
		}
	}
	return out
}

var hintLabel = map[string]string{
	"stock:preamble":       "前置きの定型句を含む",
	"stock:closing":        "締めの定型句を含む",
	"stock:filler":         "水増し語を含む",
	"repeat":               "本文の再掲（類似度が高い）",
	"low_density":          "情報密度が低い節",
	"generic_heading":      "汎用的な見出しの節",
	"summary_like":         "まとめ系の節",
	"summary_repeats_body": "再掲率の高いまとめ",
}

func label(h string) string {
	if l, ok := hintLabel[h]; ok {
		return l
	}
	return h
}

func groupsOf(d Decision) []string {
	f := d.Features
	cat := categories[f.Category]
	if cat == "" {
		cat = f.Category
	}
	g := []string{"分類: " + cat}
	if f.Kind == "section" && f.Heading != nil && *f.Heading != "" {
		g = append(g, fmt.Sprintf("見出し「%s」を節ごと削る", *f.Heading))
	}
	for _, h := range f.Hints {
		g = append(g, "目印: "+label(h))
	}
	for _, h := range f.SectionFlags {
		g = append(g, "目印: "+label(h))
	}
	seen := map[string]bool{}
	var out []string
	for _, x := range g {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

type Row struct {
	Name     string
	N        int
	Accepted int
	Notes    []string
	Rate     float64
	Advice   string
}

type Summary struct {
	Total    int
	Accepted int
	Rows     []*Row
}

func summarize(ds []Decision, cfg *Config) Summary {
	groups := map[string]*Row{}
	var order []*Row
	s := Summary{Total: len(ds)}
	for _, d := range ds {
		if d.Decision == "accept" {
			s.Accepted++
		}
		for _, g := range groupsOf(d) {
			x := groups[g]
			if x == nil {
				x = &Row{Name: g}
				groups[g] = x
				order = append(order, x)
			}
			x.N++
			if d.Decision == "accept" {
				x.Accepted++
			}
			if d.Decision == "reject" && d.Note != nil && *d.Note != "" {
				x.Notes = append(x.Notes, *d.Note)
			}
		}
	}
	l := cfg.Learning
	for _, x := range order {
		if x.N > 0 {
			x.Rate = float64(x.Accepted) / float64(x.N)
		}
		x.Advice = "データ不足"
		if x.N >= l.MinSamples {
			switch {
			case x.Rate <= l.SuppressBelow:
				x.Advice = "控える"
			case x.Rate >= l.ConfidentAbove:
				x.Advice = "積極的に提案してよい"
			default:
				x.Advice = "従来どおり"
			}
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return order[a].N > order[b].N })
	s.Rows = order
	return s
}

func lastN(xs []string, n int) []string {
	if len(xs) > n {
		return xs[len(xs)-n:]
	}
	return xs
}

// writeLearned は SKILL が提案前に読む learned.md を書く
func writeLearned(path string, s Summary) {
	out := []string{"# 過去の採否から分かった傾向", ""}
	out = append(out, fmt.Sprintf("判断 %d件（採用 %d件）。kuchiku の apply が自動で更新する。手で編集しない。", s.Total, s.Accepted), "")
	var strong []*Row
	for _, r := range s.Rows {
		if r.Advice == "控える" || r.Advice == "積極的に提案してよい" {
			strong = append(strong, r)
		}
	}
	if len(strong) > 0 {
		out = append(out, "## 次回の提案方針", "")
		for _, r := range strong {
			why := ""
			if len(r.Notes) > 0 {
				var q []string
				for _, n := range lastN(r.Notes, 3) {
					q = append(q, "「"+n+"」")
				}
				why = " 却下の理由: " + strings.Join(q, "、")
			}
			out = append(out, fmt.Sprintf("- **%s**: %s（採用 %d/%d）。%s", r.Advice, r.Name, r.Accepted, r.N, why))
		}
		out = append(out, "")
	}
	out = append(out, "## 全体", "", "| パターン | 件数 | 採用率 | 方針 |", "|---|--:|--:|---|")
	var notes []string
	for _, r := range s.Rows {
		out = append(out, fmt.Sprintf("| %s | %d | %d%% | %s |", r.Name, r.N, int(math.Round(r.Rate*100)), r.Advice))
		notes = append(notes, r.Notes...)
	}
	if len(notes) > 0 {
		out = append(out, "", "## 最近の却下理由", "")
		seen := map[string]bool{}
		var uniq []string
		for _, n := range notes {
			if !seen[n] {
				seen[n] = true
				uniq = append(uniq, n)
			}
		}
		for _, n := range lastN(uniq, 10) {
			out = append(out, "- "+n)
		}
	}
	os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
