package main

// 手順2: 文章を削除単位に分け、機械ヒントと主眼の候補を run ディレクトリに書き出す

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var kindLabel = map[string]string{
	"heading": "見出し", "sentence": "文", "item": "項目", "code": "コード", "table": "表", "quote": "引用",
	"container": "ブロック", "figure": "図", "embed": "埋め込み", "math": "数式", "comment": "コメント", "frontmatter": "front",
}

func padRight(s string, n int, pad string) string {
	for l := len([]rune(s)); l < n; l++ {
		s += pad
	}
	return s
}

func fnum(p *float64) string {
	if p == nil {
		return "-"
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

const planUsage = `使い方: kuchiku plan <文章.md | ディレクトリ>`

func cmdPlan(args Args) {
	if len(args.Pos) == 0 {
		fail(2, planUsage)
	}
	st, err := os.Stat(args.Pos[0])
	if err != nil {
		fail(2, planUsage)
	}
	if st.IsDir() {
		planDir(args.Pos[0])
		return
	}
	printJSON(planOne(args.Pos[0]))
}

// planDir は配下の .md をすべて分析し、文章ごとの一覧（overview.md）を作る
func planDir(root string) {
	files := listArticles(root)
	if len(files) == 0 {
		fail(1, "%s に .md がない", root)
	}
	rows := []string{"# kuchiku: 一括分析", "", "| 文章 | 本文 | 主眼の候補 | 目印のある節 | run |", "|---|--:|---|--:|---|"}
	articles := []map[string]any{}
	for _, f := range files {
		r := planOne(f)
		articles = append(articles, r)
		promise := "（なし）"
		if p, ok := r["promise"].(map[string]string); ok {
			promise = truncate(p["text"], 40)
		}
		rows = append(rows, fmt.Sprintf("| %s | %s字 | %s | %d | %s |", relPath(f), fmtInt(r["bodyChars"].(int)), strings.ReplaceAll(promise, "|", "｜"), len(r["flaggedSections"].([]string)), r["runDir"]))
	}
	overview := filepath.Join(homeDir(files[0]), "overview.md")
	os.MkdirAll(filepath.Dir(overview), 0o755)
	os.WriteFile(overview, []byte(strings.Join(rows, "\n")+"\n"), 0o644)
	printJSON(map[string]any{"overview": overview, "count": len(files), "articles": articles})
}

func planOne(article string) map[string]any {
	cfg := loadConfig()
	source := readFile(article)
	doc := segment(source)
	hints := analyze(doc, cfg)
	dir := runDir(article)

	found := findPromise(article, cfg)
	learnedPath := filepath.Join(homeDir(article), "learned.md")
	abs, _ := filepath.Abs(article)
	meta := Meta{Article: abs, ArticleRel: relPath(article), Hash: sha(source), CreatedAt: time.Now().UTC().Format(time.RFC3339), BodyChars: charCount(bodyText(doc))}

	writeJSON(filepath.Join(dir, "units.json"), map[string]any{"meta": meta, "doc": doc, "hints": hints}, " ")
	prom := map[string]any{"text": nil, "source": nil, "priorities": []Priority{}}
	if found != nil {
		prom["text"], prom["source"] = found.Text, found.Source
		if found.Priorities != nil {
			prom["priorities"] = found.Priorities
		}
	}
	writeJSON(filepath.Join(dir, "promise.json"), prom, "  ")

	out := []string{fmt.Sprintf("# kuchiku: %s", meta.ArticleRel), ""}
	out = append(out, fmt.Sprintf("- 本文: %s字 / 節 %d / 単位 %d", fmtInt(meta.BodyChars), len(hints.Sections), len(doc.Units)))
	if found != nil {
		out = append(out, fmt.Sprintf("- 主眼の候補: 「%s」（出典: %s）", found.Text, found.Source))
	} else {
		out = append(out, "- 主眼の候補: 見つからない（文章から一文で書き出し、本人に確認する）")
	}
	_, learnedErr := os.Stat(learnedPath)
	if learnedErr == nil {
		out = append(out, fmt.Sprintf("- 過去の採否の傾向: %s を先に読むこと", learnedPath))
	}
	out = append(out, "- 🔒 は削除できない単位。⚑ は機械ヒント（判断材料であって、削る理由ではない）", "")

	if found != nil && len(found.Priorities) > 0 {
		out = append(out, "## 構造シートの優先度（A は削らない）", "", "| 番号 | 項目 | 優先度 |", "|:-:|---|:-:|")
		for _, r := range found.Priorities {
			out = append(out, fmt.Sprintf("| %d | %s | %s |", r.No, r.Item, r.Priority))
		}
		out = append(out, "")
	}

	out = append(out, "## 節の一覧", "", "| 節 | 見出し | 字数(子節含む) | 密度 | 目印 |", "|---|---|--:|--:|---|")
	secHint := map[string]*SectionHint{}
	for _, s := range hints.Sections {
		secHint[s.ID] = s
		flags := append([]string{}, s.Flags...)
		if s.RepeatRate != nil {
			flags = append(flags, fmt.Sprintf("再掲率%d%%", int(*s.RepeatRate*100+0.5)))
		}
		if s.Locked {
			flags = append(flags, "🔒")
		}
		out = append(out, fmt.Sprintf("| %s | %s %s | %d | %s | %s |", s.ID, strings.Repeat("#", s.Level), s.Heading, s.Chars, fnum(s.Density), strings.Join(flags, ", ")))
	}
	out = append(out, "", "## 本文", "")

	curSec := ""
	for _, u := range doc.Units {
		if u.Kind == "frontmatter" {
			continue
		}
		if u.Section != curSec {
			curSec = u.Section
			if s := secHint[curSec]; s != nil && u.Kind != "heading" {
				out = append(out, fmt.Sprintf("### %s %s", s.ID, s.Heading))
			}
		}
		if u.Kind == "heading" {
			info := ""
			if s := secHint[u.Section]; s != nil {
				info = fmt.Sprintf("%d字 / 密度%s", s.Chars, fnum(s.Density))
				if len(s.Flags) > 0 {
					info += " / " + strings.Join(s.Flags, ", ")
				}
			}
			if out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, fmt.Sprintf("### %s %s  [%s]", u.Section, strings.TrimSpace(u.Text), info))
		}
		lock := "  "
		if u.Locked {
			lock = "🔒"
		}
		lbl := kindLabel[u.Kind]
		if lbl == "" {
			lbl = u.Kind
		}
		lbl = padRight(lbl, 4, "　")
		var body string
		switch u.Kind {
		case "code", "table", "container", "math", "figure":
			extra := ""
			if u.Kind == "table" {
				extra = fmt.Sprintf("、%d字", charCount(u.Text))
			}
			body = fmt.Sprintf("%s（%d行%s）", truncate(strings.SplitN(u.Text, "\n", 2)[0], 60), u.EndLine-u.StartLine+1, extra)
		default:
			body = truncate(strings.TrimSpace(u.Text), 220)
		}
		indent := ""
		if u.Kind == "item" && u.Indent > 0 {
			indent = strings.Repeat("  ", (u.Indent+1)/2)
		}
		flags := ""
		if hs := hints.Units[u.ID]; len(hs) > 0 {
			flags = "  ⚑ " + strings.Join(hs, " ")
		}
		out = append(out, fmt.Sprintf("%s%s %s %s%s%s", lock, padRight(u.ID, 5, " "), lbl, indent, body, flags))
	}
	unitsMd := filepath.Join(dir, "units.md")
	os.WriteFile(unitsMd, []byte(strings.Join(out, "\n")+"\n"), 0o644)

	flagged := []string{}
	for _, s := range hints.Sections {
		if len(s.Flags) > 0 {
			flagged = append(flagged, fmt.Sprintf("%s %s: %s", s.ID, s.Heading, strings.Join(s.Flags, ",")))
		}
	}
	res := map[string]any{"article": relPath(article), "runDir": dir, "unitsMd": unitsMd, "bodyChars": meta.BodyChars, "units": len(doc.Units), "promise": nil, "learned": nil, "flaggedSections": flagged}
	if found != nil {
		res["promise"] = map[string]string{"text": found.Text, "source": found.Source}
	}
	if learnedErr == nil {
		res["learned"] = learnedPath
	}
	return res
}
