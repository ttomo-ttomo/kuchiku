package main

// LLMが書いた削除案（plan.json）を検査して、消す unit の集合に展開する

import (
	"fmt"
	"regexp"
	"strings"
)

var categories = map[string]string{
	"promise_unrelated": "主眼と無関係",
	"repeat":            "再掲",
	"generic":           "一般論（差し替えても成立）",
	"preamble":          "前置き",
	"filler":            "中身のない文",
}

var categoryOrder = []string{"promise_unrelated", "repeat", "generic", "preamble", "filler"}

type StructItem struct {
	Section  string `json:"section"`
	Priority string `json:"priority"`
}

type PlanCut struct {
	ID                int      `json:"id"`
	Targets           []string `json:"targets"`
	Category          string   `json:"category"`
	Reason            string   `json:"reason"`
	BreaksRepro       *bool    `json:"breaks_repro"`
	ChangesConclusion *bool    `json:"changes_conclusion"`
	Accepted          *bool    `json:"accepted"` // 採用するか（主眼の確認後に、全案へ true / false を書く）
	Note              string   `json:"note"`     // 却下されたときの理由（apply が記録する）
}

type Plan struct {
	Promise          string       `json:"promise"`
	PromiseConfirmed bool         `json:"promise_confirmed"`
	Structure        []StructItem `json:"structure"`
	Cuts             []PlanCut    `json:"cuts"`
}

type Features struct {
	Category     string   `json:"category"`
	Kind         string   `json:"kind"`
	Heading      *string  `json:"heading"`
	SectionFlags []string `json:"sectionFlags"`
	Hints        []string `json:"hints"`
	UnitKinds    []string `json:"unitKinds"`
}

type Cut struct {
	ID       int      `json:"id"`
	Category string   `json:"category"`
	Reason   string   `json:"reason"`
	Targets  []string `json:"targets"`
	Kind     string   `json:"kind"`
	UnitIDs  []string `json:"unitIds"`
	Chars    int      `json:"chars"`
	Heading  string   `json:"heading"`
	Label    string   `json:"label"`
	Features Features `json:"features"`
}

var (
	reStockCat  = regexp.MustCompile(`^(stock:[a-z]+):.*`)
	reRepeatCat = regexp.MustCompile(`^(repeat_of):.*`)
)

func expandPlan(plan *Plan, doc *Doc, hints *Hints) (errs, warns []string, cuts []*Cut) {
	errs, warns = []string{}, []string{}
	uByID, sByID := doc.unitByID(), doc.sectionByID()

	// ① 主眼が本人に確認されていること
	if plan.Promise == "" {
		errs = append(errs, "promise（主眼の一文）がない")
	}

	// 構造シートで優先度Aの節は削れない
	protected := map[string]bool{}
	if len(plan.Structure) == 0 {
		errs = append(errs, "structure（構造シートの節と優先度）がない。手順1の結果を書く")
	}
	for _, it := range plan.Structure {
		switch {
		case sByID[it.Section] == nil || it.Section == "s0":
			errs = append(errs, fmt.Sprintf("structure: %s は削除対象になる節ではない", it.Section))
		case it.Priority != "A" && it.Priority != "B" && it.Priority != "C":
			errs = append(errs, fmt.Sprintf("structure: %s の priority は A / B / C のどれか", it.Section))
		case it.Priority == "A":
			for _, id := range sectionUnitIDs(doc, it.Section) {
				protected[id] = true
			}
		}
	}

	if plan.Cuts == nil {
		return append(errs, "cuts が配列でない"), warns, nil
	}

	owner := map[string]int{}
	seen := map[int]bool{}
	for _, c := range plan.Cuts {
		tag := fmt.Sprintf("案%d", c.ID)
		if c.ID < 1 || seen[c.ID] {
			errs = append(errs, tag+": id は重複しない正の整数にする")
			continue
		}
		seen[c.ID] = true
		if categories[c.Category] == "" {
			errs = append(errs, tag+": category は "+strings.Join(categoryOrder, " / ")+" のどれか")
		}
		if n := len([]rune(c.Reason)); c.Reason == "" {
			errs = append(errs, tag+": reason がない")
		} else if n > 100 {
			errs = append(errs, fmt.Sprintf("%s: reason は100字以内（今 %d字）", tag, n))
		}
		// 削除テスト: 両方 false でなければ削ってはいけない
		if c.BreaksRepro == nil || *c.BreaksRepro {
			errs = append(errs, tag+": breaks_repro が false でない（消すと読者が再現できなくなるなら削らない）")
		}
		if c.ChangesConclusion == nil || *c.ChangesConclusion {
			errs = append(errs, tag+": changes_conclusion が false でない（消すと結論が変わるなら削らない）")
		}
		if len(c.Targets) == 0 {
			errs = append(errs, tag+": targets が空")
			continue
		}

		ids := map[string]bool{}
		kind := "units"
		headTarget := ""
		for _, t := range c.Targets {
			if sByID[t] != nil {
				if t == "s0" {
					errs = append(errs, tag+": s0（見出し前）は節ごとには消せない。単位で指定する")
					continue
				}
				kind = "section"
				if headTarget == "" {
					headTarget = t
				}
				for _, id := range sectionUnitIDs(doc, t) {
					ids[id] = true
				}
			} else if uByID[t] != nil {
				ids[t] = true
				// 親の項目を消すなら子の項目も消す
				var addChildren func(u *Unit)
				addChildren = func(u *Unit) {
					for _, cid := range u.Children {
						ids[cid] = true
						addChildren(uByID[cid])
					}
				}
				addChildren(uByID[t])
			} else {
				errs = append(errs, fmt.Sprintf("%s: %s は存在しない", tag, t))
			}
		}
		sorted := make([]string, 0, len(ids))
		for id := range ids {
			sorted = append(sorted, id)
		}
		sortIDs(sorted)
		for _, id := range sorted {
			u := uByID[id]
			if protected[id] {
				errs = append(errs, fmt.Sprintf("%s: %s は優先度Aの節に含まれるので削れない", tag, id))
			}
			if u.Kind == "frontmatter" {
				errs = append(errs, tag+": frontmatter は消せない")
			} else if u.Locked {
				errs = append(errs, fmt.Sprintf("%s: %s は keep マーカーでロックされている", tag, id))
			}
			if o, ok := owner[id]; ok {
				errs = append(errs, fmt.Sprintf("%s: %s は案%d と重複している（1つの単位は1つの案にだけ入れる）", tag, id, o))
			}
			owner[id] = c.ID
		}
		if len(sorted) == 0 {
			continue
		}
		units := make([]*Unit, len(sorted))
		for i, id := range sorted {
			units[i] = uByID[id]
		}
		var headSec *Section
		if kind == "section" {
			headSec = sByID[headTarget]
		} else {
			headSec = sByID[units[0].Section]
		}

		if len(units) == 1 && units[0].Kind == "heading" {
			warns = append(warns, tag+": 見出しだけを消すと、本文が前の節に合流する")
		}

		hintSet := map[string]bool{}
		var unitHints []string
		for _, u := range units {
			for _, h := range hints.Units[u.ID] {
				h = reRepeatCat.ReplaceAllString(reStockCat.ReplaceAllString(h, "$1"), "repeat")
				if !hintSet[h] {
					hintSet[h] = true
					unitHints = append(unitHints, h)
				}
			}
		}
		sectionFlags := []string{}
		for _, s := range hints.Sections {
			if s.ID == headSec.ID {
				sectionFlags = s.Flags
				break
			}
		}
		var texts []string
		for _, u := range units {
			if u.Kind != "comment" {
				texts = append(texts, u.Text)
			}
		}
		var heading *string
		headingStr := "(見出し前)"
		if headSec.Heading != "" {
			headingStr = cleanHeading(headSec.Heading)
			heading = &headingStr
		}
		var label string
		if kind == "section" {
			label = fmt.Sprintf("%s %s（節ごと）", headSec.ID, cleanHeading(headSec.Heading))
		} else {
			rng, more := units[0].ID, ""
			if len(units) > 1 {
				rng += "〜" + units[len(units)-1].ID
				more = fmt.Sprintf("ほか計%d", len(units))
			}
			label = fmt.Sprintf("%s「%s」%s", rng, truncate(strings.TrimSpace(units[0].Text), 24), more)
		}
		kinds := []string{}
		for _, u := range units {
			found := false
			for _, k := range kinds {
				found = found || k == u.Kind
			}
			if !found {
				kinds = append(kinds, u.Kind)
			}
		}
		if unitHints == nil {
			unitHints = []string{}
		}
		cuts = append(cuts, &Cut{
			ID: c.ID, Category: c.Category, Reason: c.Reason, Targets: c.Targets, Kind: kind, UnitIDs: sorted,
			Chars: charCount(strings.Join(texts, "\n")), Heading: headingStr, Label: label,
			Features: Features{Category: c.Category, Kind: kind, Heading: heading, SectionFlags: sectionFlags, Hints: unitHints, UnitKinds: kinds},
		})
	}

	// 節の中身を全部消して見出しだけ残る場合
	deleted := map[string]bool{}
	for _, c := range cuts {
		for _, id := range c.UnitIDs {
			deleted[id] = true
		}
	}
	for _, s := range doc.Sections {
		if s.ID == "s0" {
			continue
		}
		head := ""
		var rest []string
		for _, id := range sectionUnitIDs(doc, s.ID) {
			switch {
			case head == "" && uByID[id].Kind == "heading":
				head = id
			case uByID[id].Kind != "comment":
				rest = append(rest, id)
			}
		}
		if len(rest) == 0 || deleted[head] {
			continue
		}
		all := true
		for _, id := range rest {
			all = all && deleted[id]
		}
		if all {
			warns = append(warns, fmt.Sprintf("%s %s: 中身が全部消えて見出しだけ残る。節ごと消すなら targets に %s を指定する", s.ID, cleanHeading(s.Heading), s.ID))
		}
	}
	return errs, warns, cuts
}
