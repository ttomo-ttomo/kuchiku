package main

// 削除だけでできているかの検査。
// 短縮版の単位が、原文の単位の「部分列」（順番を保ったまま一部を抜いたもの）になっているかを見る。

import (
	"fmt"
	"strings"
)

type Problem struct {
	Type   string
	Text   string
	Detail string
}

type VerifyResult struct {
	OK       bool
	Problems []Problem
	Kept     int
	Total    int
}

func nonFront(d *Doc) []*Unit {
	var out []*Unit
	for _, u := range d.Units {
		if u.Kind != "frontmatter" {
			out = append(out, u)
		}
	}
	return out
}

func frontText(d *Doc) string {
	for _, u := range d.Units {
		if u.Kind == "frontmatter" {
			return u.Text
		}
	}
	return ""
}

func verify(originalSource, cutSource string) VerifyResult {
	a, b := segment(originalSource), segment(cutSource)
	var problems []Problem
	if frontText(a) != frontText(b) {
		problems = append(problems, Problem{Type: "frontmatter", Text: "frontmatter が変わっている"})
	}
	A, B := nonFront(a), nonFront(b)
	matched := map[string]bool{}
	p := 0
	for _, u := range B {
		t := norm(u.Text)
		k := p
		for k < len(A) && norm(A[k].Text) != t {
			k++
		}
		if k == len(A) {
			// 原文のどこかにはあるなら「順番の入れ替え」、なければ「書き換え・追加」
			anywhere := -1
			for i, x := range A {
				if norm(x.Text) == t {
					anywhere = i
					break
				}
			}
			if anywhere >= 0 {
				problems = append(problems, Problem{"reordered", truncate(strings.TrimSpace(u.Text), 80), "原文では " + A[anywhere].ID})
			} else {
				problems = append(problems, Problem{"added_or_changed", truncate(strings.TrimSpace(u.Text), 80), nearest(t, A)})
			}
			continue
		}
		matched[A[k].ID] = true
		p = k + 1
	}
	for _, u := range A {
		if u.Locked && !matched[u.ID] {
			problems = append(problems, Problem{"locked_removed", truncate(strings.TrimSpace(u.Text), 80), u.ID + " は削除不可"})
		}
	}
	return VerifyResult{len(problems) == 0, problems, len(matched), len(A)}
}

// nearest はいちばん先頭が長く一致する原文の単位を、手がかりとして返す
func nearest(t string, units []*Unit) string {
	tr := []rune(t)
	bestN, bestU := 0, (*Unit)(nil)
	for _, u := range units {
		s := []rune(norm(u.Text))
		n := 0
		for n < len(s) && n < len(tr) && s[n] == tr[n] {
			n++
		}
		if bestU == nil || n > bestN {
			bestN, bestU = n, u
		}
	}
	if bestU != nil && bestN >= 4 {
		return fmt.Sprintf("近い原文: %s「%s」", bestU.ID, truncate(strings.TrimSpace(bestU.Text), 40))
	}
	return "原文に該当なし"
}
