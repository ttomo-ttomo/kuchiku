package main

// 削除案（plan.json）を検査し、削除だけを適用した短縮版とレポートを作る。元の文章は変更しない

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// previewOne は plan.json を検査し、削除だけを適用した短縮版（cut.md）とレポートを作る。元の文章は変えない
func previewOne(article string) (map[string]any, error) {
	run, err := loadRun(article)
	if err != nil {
		return nil, err
	}
	// 検査に失敗したとき、前回の結果が残って apply に使われないようにする
	os.Remove(filepath.Join(run.Dir, "preview.json"))
	os.Remove(filepath.Join(run.Dir, "cut.md"))
	planPath := filepath.Join(run.Dir, "plan.json")
	pb, err := os.ReadFile(planPath)
	if err != nil {
		return nil, fmt.Errorf("%s がない", planPath)
	}
	var plan Plan
	if err := json.Unmarshal(pb, &plan); err != nil {
		return nil, fmt.Errorf("plan.json を読めない: %v", err)
	}
	errs, warns, cuts := expandPlan(&plan, run.Doc, run.Hints)
	if len(errs) > 0 {
		return nil, fmt.Errorf("削除案に問題がある（何も書き出していない）:\n- %s", strings.Join(errs, "\n- "))
	}

	var deleteIDs []string
	for _, c := range cuts {
		deleteIDs = append(deleteIDs, c.UnitIDs...)
	}
	cutSource := render(run.Doc, deleteIDs)
	if chk := verify(run.Source, cutSource); !chk.OK {
		// render は削除しかしないので、ここに来たらこのツールのバグ
		var ps []string
		for _, p := range chk.Problems {
			ps = append(ps, fmt.Sprintf("- %s: %s（%s）", p.Type, p.Text, p.Detail))
		}
		return nil, fmt.Errorf("検証に失敗した（バグの可能性）:\n%s", strings.Join(ps, "\n"))
	}

	before, after := run.Meta.BodyChars, bodyChars(cutSource)
	uByID := run.Doc.unitByID()
	os.WriteFile(filepath.Join(run.Dir, "cut.md"), []byte(cutSource), 0o644)
	writeJSON(filepath.Join(run.Dir, "preview.json"), map[string]any{
		"article": run.Meta.ArticleRel, "hash": run.Meta.Hash, "promise": plan.Promise, "before": before, "after": after, "cuts": cuts,
	}, "  ")

	pct := func(n int) string { return fmt.Sprintf("%v%%", math.Round(float64(n)/float64(before)*1000)/10) }
	r := []string{fmt.Sprintf("# 削除案: %s", run.Meta.ArticleRel), "", fmt.Sprintf("**主眼**: %s", plan.Promise), ""}
	r = append(r, fmt.Sprintf("本文 %s字 → %s字（−%s字、−%s）／ 削除案 %d件・%d単位", fmtInt(before), fmtInt(after), fmtInt(before-after), pct(before-after), len(cuts), len(deleteIDs)), "")
	r = append(r, "元の文章は変更していません。短縮版は cut.md です。反映するには、plan.json の各案に accepted を書き、promise_confirmed を true にして apply を実行します。", "")
	if len(warns) > 0 {
		r = append(r, "**注意**", "")
		for _, w := range warns {
			r = append(r, "- "+w)
		}
		r = append(r, "")
	}
	r = append(r, "| # | 対象 | 分類 | 削減 | 理由 |", "|--:|---|---|--:|---|")
	for _, c := range cuts {
		r = append(r, fmt.Sprintf("| %d | %s | %s | %d字 | %s |", c.ID, c.Label, categories[c.Category], c.Chars, strings.ReplaceAll(c.Reason, "|", "｜")))
	}
	r = append(r, "", "## 各案で消える文", "")
	for _, c := range cuts {
		r = append(r, fmt.Sprintf("### %d. %s（%d字）", c.ID, c.Label, c.Chars), "")
		for _, id := range c.UnitIDs {
			u := uByID[id]
			first := strings.SplitN(u.Text, "\n", 2)[0]
			more := ""
			if u.EndLine > u.StartLine {
				k := ""
				if u.Kind != "item" {
					k = u.Kind + "、"
				}
				more = fmt.Sprintf("（%s%d行）", k, u.EndLine-u.StartLine+1)
			}
			r = append(r, fmt.Sprintf("> ~~%s~~%s", strings.ReplaceAll(truncate(strings.TrimSpace(first), 90), "~", "～"), more))
		}
		r = append(r, "")
	}
	os.WriteFile(filepath.Join(run.Dir, "report.md"), []byte(strings.Join(r, "\n")), 0o644)

	type summ struct {
		ID       int    `json:"id"`
		Label    string `json:"label"`
		Category string `json:"category"`
		Chars    int    `json:"chars"`
	}
	ss := []summ{}
	for _, c := range cuts {
		ss = append(ss, summ{c.ID, c.Label, c.Category, c.Chars})
	}
	return map[string]any{
		"ok": true, "verified": true, "report": filepath.Join(run.Dir, "report.md"), "cut": filepath.Join(run.Dir, "cut.md"),
		"before": before, "after": after, "removed": before - after, "cuts": ss, "warnings": warns,
	}, nil
}
