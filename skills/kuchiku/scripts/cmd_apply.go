package main

// 手順5・7: 主眼が未確認なら削除案を検査して見せるだけ。確認済みなら、accepted が true の案だけを文章に反映し、採否を記録する

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func cmdApply(args Args) {
	if len(args.Pos) == 0 {
		fail(2, "使い方: kuchiku apply <文章.md | ディレクトリ>")
	}
	runEach(args.Pos[0], func(a string) string {
		if !fileExists(filepath.Join(runDirPath(a), "plan.json")) {
			return "plan.json がない（plan のあと、削除案を作ってから）"
		}
		return ""
	}, applyOne)
}

// applyOne は plan.json を検査して見せる。主眼が確認済み（promise_confirmed）なら、accepted が true の案を文章に反映する
func applyOne(article string) (map[string]any, error) {
	run, err := loadRun(article) // plan の後に文章が変わっていないことの確認も兼ねる
	if err != nil {
		return nil, err
	}
	// plan.json は毎回ここで検査し直す。plan.json を直したあとでも、最新の内容で見せる・反映する
	pv, err := previewOne(article)
	if err != nil {
		return nil, err
	}
	var plan Plan
	pb, err := os.ReadFile(filepath.Join(run.Dir, "plan.json"))
	if err != nil || json.Unmarshal(pb, &plan) != nil {
		return nil, fmt.Errorf("plan.json を読めない")
	}
	if !plan.PromiseConfirmed {
		pv["applied"] = false // 主眼が未確認なら、見せるだけで文章は変えない
		return pv, nil
	}
	ab, err := os.ReadFile(filepath.Join(run.Dir, "preview.json"))
	if err != nil {
		return nil, fmt.Errorf("preview.json を読めない: %v", err)
	}
	var applied struct {
		Article string `json:"article"`
		Hash    string `json:"hash"`
		Cuts    []Cut  `json:"cuts"`
	}
	if err := json.Unmarshal(ab, &applied); err != nil {
		return nil, fmt.Errorf("preview.json を読めない: %v", err)
	}
	if applied.Hash != run.Meta.Hash {
		return nil, fmt.Errorf("plan の後に文章か run が変わっている。plan からやり直す")
	}

	// 全案に accepted（true / false）が書かれていること。書き忘れで全部「却下」と記録されるのを防ぐ
	var accept []int
	var missing []int
	for _, c := range plan.Cuts {
		switch {
		case c.Accepted == nil:
			missing = append(missing, c.ID)
		case *c.Accepted:
			accept = append(accept, c.ID)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("案 %v に accepted（true か false）がない。採否を決めて plan.json に書く", missing)
	}

	// 却下の理由は plan.json の各案の note から読む
	notes := map[int]string{}
	for _, c := range plan.Cuts {
		if c.Note != "" {
			notes[c.ID] = strings.TrimSpace(c.Note)
		}
	}

	var del []string
	for _, c := range applied.Cuts {
		if contains(accept, c.ID) {
			del = append(del, c.UnitIDs...)
		}
	}
	finalSource := render(run.Doc, del)
	if !verify(run.Source, finalSource).OK {
		return nil, fmt.Errorf("検証に失敗した（バグの可能性）。何も書き出していない")
	}
	os.WriteFile(filepath.Join(run.Dir, "final.md"), []byte(finalSource), 0o644)

	home := homeDir(run.Meta.Article)
	os.MkdirAll(home, 0o755)
	logPath := filepath.Join(home, "decisions.jsonl")
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	var lines []string
	for _, c := range applied.Cuts {
		d := "reject"
		if contains(accept, c.ID) {
			d = "accept"
		}
		rec := Decision{TS: ts, Article: applied.Article, Hash: applied.Hash, Cut: c.ID, Decision: d, Chars: c.Chars, Reason: c.Reason, Features: c.Features}
		if n, ok := notes[c.ID]; ok && d == "reject" {
			rec.Note = &n
		}
		b, _ := json.Marshal(rec)
		lines = append(lines, string(b))
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%v", err)
	}
	f.WriteString(strings.Join(lines, "\n") + "\n")
	f.Close()

	summary := summarize(latestDecisions(readDecisions(logPath)), loadConfig())
	learnedPath := filepath.Join(home, "learned.md")
	writeLearned(learnedPath, summary)

	// 採用された案があれば文章に反映する（loadRun が、plan の後に文章が変わっていないことを確かめ済み）
	var wrote any = false
	if len(accept) > 0 {
		if err := os.WriteFile(run.Meta.Article, []byte(finalSource), 0o644); err != nil {
			return nil, err
		}
		wrote = run.Meta.Article
	}

	rejected := []int{}
	for _, c := range applied.Cuts {
		if !contains(accept, c.ID) {
			rejected = append(rejected, c.ID)
		}
	}
	if accept == nil {
		accept = []int{}
	}
	return map[string]any{
		"verified": true, "accepted": accept, "rejected": rejected, "before": run.Meta.BodyChars, "after": bodyChars(finalSource),
		"final": filepath.Join(run.Dir, "final.md"), "wroteArticle": wrote, "log": logPath, "learned": learnedPath, "decisionsTotal": summary.Total,
	}, nil
}
