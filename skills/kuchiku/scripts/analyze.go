package main

// 機械ヒント: 削る判断そのものはしない。LLMと人が見るための目印だけ出す

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

var (
	prose   = map[string]bool{"sentence": true, "item": true, "quote": true}
	content = map[string]bool{"sentence": true, "item": true, "quote": true, "table": true, "container": true, "figure": true}
)

type SectionHint struct {
	ID         string   `json:"id"`
	Heading    string   `json:"heading"`
	Level      int      `json:"level"`
	Chars      int      `json:"chars"`
	OwnChars   int      `json:"ownChars"`
	Density    *float64 `json:"density"`
	RepeatRate *float64 `json:"repeatRate"`
	Locked     bool     `json:"locked"`
	Flags      []string `json:"flags"`
}

type Hints struct {
	Sections      []*SectionHint      `json:"sections"`
	Units         map[string][]string `json:"units"`
	TopicTokens   []string            `json:"topicTokens"`
	MedianDensity float64             `json:"medianDensity"`
}

type repeatHit struct {
	id  string
	sim float64
}

var reTitle = regexp.MustCompile(`(?m)^title:\s*(.*)$`)

func analyze(doc *Doc, cfg *Config) *Hints {
	uByID := doc.unitByID()
	h := &Hints{Units: map[string][]string{}}
	addHint := func(id, s string) { h.Units[id] = append(h.Units[id], s) }

	// 定型句
	cats := cfg.StockOrder
	for _, u := range doc.Units {
		if !prose[u.Kind] {
			continue
		}
		for _, cat := range cats {
			for _, hit := range matchAny(u.Text, cfg.StockPhrases[cat]) {
				addHint(u.ID, fmt.Sprintf("stock:%s:%s", cat, hit))
			}
		}
	}

	// 別の節にほぼ同じ内容がある（再掲）
	var pu []*Unit
	for _, u := range doc.Units {
		if prose[u.Kind] && charCount(u.Text) >= 12 {
			pu = append(pu, u)
		}
	}
	repeatOf := map[string]repeatHit{}
	for _, a := range pu {
		var best *repeatHit
		for _, b := range pu {
			if a == b || a.Section == b.Section {
				continue
			}
			if sim := similarity(a.Text, b.Text); best == nil || sim > best.sim {
				best = &repeatHit{b.ID, sim}
			}
		}
		if best != nil && best.sim >= cfg.RepeatSimilarity {
			repeatOf[a.ID] = *best
		}
	}

	// テーマ語: タイトルにある語と、内容のある節の半分以上に出る語
	contentOf := func(s *Section) []*Unit {
		var out []*Unit
		for _, id := range s.UnitIDs {
			if content[uByID[id].Kind] {
				out = append(out, uByID[id])
			}
		}
		return out
	}
	joinText := func(us []*Unit) string {
		var p []string
		for _, u := range us {
			p = append(p, u.Text)
		}
		return strings.Join(p, "\n")
	}
	var withContent []*Section
	for _, s := range doc.Sections {
		if len(contentOf(s)) > 0 {
			withContent = append(withContent, s)
		}
	}
	df := map[string]int{}
	for _, s := range withContent {
		seen := map[string]bool{}
		for _, t := range infoTokenList(joinText(contentOf(s))) {
			if !seen[t] {
				seen[t] = true
				df[t]++
			}
		}
	}
	topic := map[string]bool{}
	for t, n := range df {
		if t != "" && (t[0] < '0' || t[0] > '9') && len(withContent) >= 4 && float64(n) >= float64(len(withContent))/2 {
			topic[t] = true
		}
	}
	for _, u := range doc.Units {
		if u.Kind == "frontmatter" {
			if m := reTitle.FindStringSubmatch(u.Text); m != nil {
				for _, t := range infoTokenList(strings.NewReplacer(`"`, "", `'`, "").Replace(m[1])) {
					topic[t] = true
				}
			}
			break
		}
	}

	for _, s := range doc.Sections {
		var own []*Unit
		for _, id := range s.UnitIDs {
			own = append(own, uByID[id])
		}
		var all []*Unit
		for _, id := range sectionUnitIDs(doc, s.ID) {
			if k := uByID[id].Kind; k != "frontmatter" && k != "comment" {
				all = append(all, uByID[id])
			}
		}
		ct := contentOf(s)
		if s.ID == "s0" && len(ct) == 0 {
			continue
		}
		text := joinText(ct)
		flags := []string{}
		isSummary := s.Heading != "" && headingMatches(s.Heading, cfg.SummaryHeadings)
		if s.Heading != "" && headingMatches(s.Heading, cfg.GenericHeadings) {
			flags = append(flags, "generic_heading")
		}
		if isSummary {
			flags = append(flags, "summary_like")
		}
		var d *float64
		if len(ct) > 0 {
			v := density(text, topic)
			d = &v
		}
		var proseOwn []*Unit
		for _, u := range own {
			if prose[u.Kind] && charCount(u.Text) >= 12 {
				proseOwn = append(proseOwn, u)
			}
		}
		var rr *float64
		if isSummary && len(proseOwn) > 0 {
			var rep []*Unit
			for _, u := range proseOwn {
				if _, ok := repeatOf[u.ID]; ok {
					rep = append(rep, u)
				}
			}
			v := math.Round(float64(len(rep))/float64(len(proseOwn))*100) / 100
			rr = &v
			if v >= cfg.SummaryRepeatRate {
				flags = append(flags, "summary_repeats_body")
			}
			for _, u := range rep {
				addHint(u.ID, fmt.Sprintf("repeat_of:%s(%.2f)", repeatOf[u.ID].id, repeatOf[u.ID].sim))
			}
		} else {
			for _, u := range proseOwn {
				if r, ok := repeatOf[u.ID]; ok && unitNum(r.id) < unitNum(u.ID) {
					addHint(u.ID, fmt.Sprintf("repeat_of:%s(%.2f)", r.id, r.sim))
				}
			}
		}
		heading := "(見出し前)"
		if s.Heading != "" {
			heading = cleanHeading(s.Heading)
		}
		h.Sections = append(h.Sections, &SectionHint{
			ID: s.ID, Heading: heading, Level: s.Level, Chars: charCount(joinText(all)), OwnChars: charCount(text),
			Density: d, RepeatRate: rr, Locked: s.Locked, Flags: flags,
		})
	}

	// 密度が低い節: 文章内の中央値に対する比で判定
	var ds []float64
	for _, s := range h.Sections {
		if s.Density != nil && s.OwnChars >= 60 {
			ds = append(ds, *s.Density)
		}
	}
	sort.Float64s(ds)
	if len(ds) > 0 {
		h.MedianDensity = ds[len(ds)/2]
	}
	for _, s := range h.Sections {
		if s.Density != nil && s.OwnChars >= 60 && *s.Density < h.MedianDensity*cfg.LowDensityRatio {
			s.Flags = append(s.Flags, "low_density")
		}
	}
	h.TopicTokens = []string{}
	for t := range topic {
		h.TopicTokens = append(h.TopicTokens, t)
	}
	sort.Strings(h.TopicTokens)
	return h
}
