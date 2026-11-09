package main

// plan → plan.json → plan（検査）→ apply を、ビルドした実行ファイルで通しで動かす

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "cc-bin")
	binPath = filepath.Join(dir, "kuchiku")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type cli struct {
	t       *testing.T
	repo    string
	article string
}

func setup(t *testing.T) *cli {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.MkdirAll(filepath.Join(repo, "articles"), 0o755)
	article := filepath.Join(repo, "articles", "ai-sample.md")
	src, _ := os.ReadFile(filepath.Join(fixDir, "ai-sample.md"))
	os.WriteFile(article, src, 0o644)
	t.Setenv("KUCHIKU_HOME", "")
	return &cli{t, repo, article}
}

func (c *cli) run(args ...string) (stdout, stderr string, status int) {
	cmd := exec.Command(binPath, args...)
	cmd.Dir = c.repo
	var o, e strings.Builder
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	if x, ok := err.(*exec.ExitError); ok {
		status = x.ExitCode()
	}
	return o.String(), e.String(), status
}

func (c *cli) must(args ...string) string {
	c.t.Helper()
	o, e, st := c.run(args...)
	if st != 0 {
		c.t.Fatalf("%v: status %d\n%s%s", args, st, o, e)
	}
	return o
}

func (c *cli) plan() (runDir string, promise string) {
	var p struct {
		RunDir  string `json:"runDir"`
		Promise struct{ Text string }
	}
	if err := json.Unmarshal([]byte(c.must("plan", c.article)), &p); err != nil {
		c.t.Fatal(err)
	}
	return p.RunDir, p.Promise.Text
}

func readS(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCLIFlow(t *testing.T) {
	c := setup(t)
	original := readS(t, c.article)
	runDir, promise := c.plan()
	if runDir != filepath.Join(c.repo, ".kuchiku", "runs", "articles__ai-sample") {
		t.Errorf("runDir: %s", runDir)
	}
	gi := readS(t, filepath.Join(c.repo, ".kuchiku", ".gitignore"))
	if !strings.Contains(gi, "runs/") || strings.Contains(gi, "decisions") && strings.Contains(gi, "\ndecisions") {
		t.Errorf(".kuchiku/.gitignore: %q", gi)
	}
	if !strings.Contains(promise, "git worktree") {
		t.Errorf("promise: %q", promise)
	}

	var units struct{ Doc Doc }
	json.Unmarshal([]byte(readS(t, filepath.Join(runDir, "units.json"))), &units)
	idOf := func(s string) string { return unitByText(&units.Doc, s).ID }
	var intro string
	for _, s := range units.Doc.Sections {
		if s.Heading == "はじめに" {
			intro = s.ID
		}
	}
	plan := map[string]any{
		"promise": promise, "promise_confirmed": false,
		"structure": []map[string]string{{"section": intro, "priority": "C"}},
		"cuts": []map[string]any{
			{"id": 1, "targets": []string{intro}, "category": "preamble", "reason": "予告だけ", "breaks_repro": false, "changes_conclusion": false},
			{"id": 2, "targets": []string{idOf("いかがでしたか"), idOf("参考になれば")}, "category": "filler", "reason": "締めの定型句", "breaks_repro": false, "changes_conclusion": false, "note": "締めは残したい"},
		},
	}
	pb, _ := json.Marshal(plan)
	os.WriteFile(filepath.Join(runDir, "plan.json"), pb, 0o644)

	var a struct {
		Before, After int
		Applied       bool
	}
	json.Unmarshal([]byte(c.must("apply", c.article)), &a) // 主眼が未確認なら、検査して見せるだけ
	if a.After == 0 || a.After >= a.Before || a.Applied {
		t.Errorf("縮んでいない: %+v", a)
	}
	if !strings.Contains(readS(t, filepath.Join(runDir, "report.md")), "前置き") {
		t.Error("report に分類がない")
	}
	if readS(t, c.article) != original {
		t.Error("主眼が未確認の apply は元記事を変えない")
	}

	// 主眼を確認し、案1を採用・案2を却下する
	plan["promise_confirmed"] = true
	cuts := plan["cuts"].([]map[string]any)
	cuts[0]["accepted"], cuts[1]["accepted"] = true, false
	pb, _ = json.Marshal(plan)
	os.WriteFile(filepath.Join(runDir, "plan.json"), pb, 0o644)
	c.must("apply", c.article)
	logb := readS(t, filepath.Join(c.repo, ".kuchiku", "decisions.jsonl"))
	var recs []Decision
	for _, l := range strings.Split(strings.TrimSpace(logb), "\n") {
		var d Decision
		json.Unmarshal([]byte(l), &d)
		recs = append(recs, d)
	}
	if len(recs) != 2 || recs[0].Decision != "accept" || recs[1].Decision != "reject" || recs[1].Note == nil || *recs[1].Note != "締めは残したい" {
		t.Errorf("decisions: %+v", recs)
	}
	if !strings.Contains(readS(t, filepath.Join(c.repo, ".kuchiku", "learned.md")), "締めは残したい") {
		t.Error("learned.md に却下理由がない")
	}
	final := readS(t, filepath.Join(runDir, "final.md"))
	if strings.Contains(final, "## はじめに") || !strings.Contains(final, "いかがでしたか") {
		t.Error("final.md の中身が違う")
	}
	if got := readS(t, c.article); got != final || got == original {
		t.Error("apply で記事が final.md になる")
	}
	fixAbs, _ := filepath.Abs(filepath.Join(fixDir, "ai-sample.md"))
	out := c.must("verify", fixAbs, c.article)
	if !strings.HasPrefix(out, "OK") {
		t.Error(out)
	}
}

func TestCLIPromiseUnconfirmed(t *testing.T) {
	c := setup(t)
	runDir, _ := c.plan()
	before := readS(t, c.article)
	plan := `{"promise":"x","promise_confirmed":false,"structure":[{"section":"s1","priority":"B"}],"cuts":[{"id":1,"targets":["u3"],"category":"filler","reason":"r","breaks_repro":false,"changes_conclusion":false,"accepted":true}]}`
	os.WriteFile(filepath.Join(runDir, "plan.json"), []byte(plan), 0o644)
	var r struct{ Applied bool }
	json.Unmarshal([]byte(c.must("apply", c.article)), &r) // 主眼が未確認なら、accepted が true でも見せるだけ
	if r.Applied || readS(t, c.article) != before {
		t.Error("主眼が未確認なら記事を変えない")
	}
	if _, err := os.Stat(filepath.Join(runDir, "final.md")); err == nil {
		t.Error("何も書き出さない")
	}
	if _, err := os.Stat(filepath.Join(c.repo, ".kuchiku", "decisions.jsonl")); err == nil {
		t.Error("採否を記録しない")
	}
}

func TestCLIAcceptedMissing(t *testing.T) {
	c := setup(t)
	runDir, _ := c.plan()
	before := readS(t, c.article)
	plan := `{"promise":"x","promise_confirmed":true,"structure":[{"section":"s1","priority":"B"}],"cuts":[{"id":1,"targets":["u3"],"category":"filler","reason":"r","breaks_repro":false,"changes_conclusion":false}]}`
	os.WriteFile(filepath.Join(runDir, "plan.json"), []byte(plan), 0o644)
	_, e, st := c.run("apply", c.article)
	if st != 1 || !strings.Contains(e, "accepted") || readS(t, c.article) != before {
		t.Errorf("採否の書き忘れで止まる: %d %s", st, e)
	}
}

func TestCLIUnknownOption(t *testing.T) {
	c := setup(t)
	for _, args := range [][]string{{"plan", c.article, "--promise", "x"}, {"apply", c.article, "--accept", "1"}, {"apply", c.article, "--write"}} {
		if _, e, st := c.run(args...); st != 2 || !strings.Contains(e, "不明なオプション") {
			t.Errorf("%v: %d %s", args, st, e)
		}
	}
}

func TestCLIArticleChanged(t *testing.T) {
	c := setup(t)
	runDir, _ := c.plan()
	os.WriteFile(filepath.Join(runDir, "plan.json"), []byte(`{"promise":"x","promise_confirmed":true,"structure":[{"section":"s1","priority":"B"}],"cuts":[]}`), 0o644)
	f, _ := os.OpenFile(c.article, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\n追記。\n")
	f.Close()
	_, e, st := c.run("apply", c.article)
	if st != 1 || !strings.Contains(e, "変更されている") {
		t.Errorf("%d %s", st, e)
	}
}

func TestCLIPrepareDir(t *testing.T) {
	c := setup(t)
	os.MkdirAll(filepath.Join(c.repo, "articles", "sub"), 0o755)
	os.WriteFile(filepath.Join(c.repo, "articles", "sub", "ai-sample.md"), []byte(readS(t, c.article)), 0o644) // 同名の別記事
	os.MkdirAll(filepath.Join(c.repo, "articles", ".hidden"), 0o755)
	os.WriteFile(filepath.Join(c.repo, "articles", ".hidden", "x.md"), []byte("無視される\n"), 0o644)

	var r struct {
		Overview string
		Count    int
		Articles []struct{ RunDir string }
	}
	if err := json.Unmarshal([]byte(c.must("plan", filepath.Join(c.repo, "articles"))), &r); err != nil {
		t.Fatal(err)
	}
	if r.Count != 2 || r.Articles[0].RunDir == r.Articles[1].RunDir {
		t.Errorf("同名の記事でも run が分かれる: %+v", r)
	}
	ov := readS(t, r.Overview)
	if !strings.Contains(ov, "articles/sub/ai-sample.md") || strings.Contains(ov, "hidden") {
		t.Error(ov)
	}
}

func TestCLIBatchDir(t *testing.T) {
	c := setup(t)
	os.MkdirAll(filepath.Join(c.repo, "articles", "sub"), 0o755)
	os.WriteFile(filepath.Join(c.repo, "articles", "sub", "ai-sample.md"), []byte(readS(t, c.article)), 0o644)
	os.WriteFile(filepath.Join(c.repo, "articles", "no-plan.md"), []byte("# 見出し\n\n本文です。\n"), 0o644)
	dir := filepath.Join(c.repo, "articles")

	var prep struct {
		Articles []struct{ Article, RunDir string }
	}
	json.Unmarshal([]byte(c.must("plan", dir)), &prep)
	planWith := func(confirmed bool) {
		plan := fmt.Sprintf(`{"promise":"x","promise_confirmed":%v,"structure":[{"section":"s1","priority":"B"}],"cuts":[{"id":1,"targets":["s1"],"category":"preamble","reason":"r","breaks_repro":false,"changes_conclusion":false,"accepted":true}]}`, confirmed)
		for _, a := range prep.Articles {
			if strings.HasSuffix(a.Article, "ai-sample.md") {
				os.WriteFile(filepath.Join(a.RunDir, "plan.json"), []byte(plan), 0o644)
			}
		}
	}
	planWith(false)

	var pv struct {
		Done, Skipped int
		Articles      []map[string]any
	}
	json.Unmarshal([]byte(c.must("apply", dir)), &pv) // 主眼が未確認なら、plan.json のある文章を検査するだけ
	if pv.Done != 2 || pv.Skipped != 1 || readS(t, c.article) != readS(t, filepath.Join(fixDir, "ai-sample.md")) {
		t.Fatalf("検査だけ: %+v", pv)
	}

	planWith(true)
	var ap struct {
		Done, Skipped, Failed int
		Articles              []map[string]any
	}
	json.Unmarshal([]byte(c.must("apply", dir)), &ap)
	if ap.Done != 2 || ap.Skipped != 1 || ap.Failed != 0 {
		t.Fatalf("apply: %+v", ap)
	}
	for _, a := range ap.Articles {
		if a["skipped"] == nil && a["verified"] != true {
			t.Errorf("verified が出ていない: %v", a)
		}
	}
	if readS(t, c.article) == readS(t, filepath.Join(fixDir, "ai-sample.md")) {
		t.Error("apply で記事が縮む")
	}
}

func TestCLIApplyInspectsEveryTime(t *testing.T) {
	plan := func(confirmed bool, target string) string {
		return fmt.Sprintf(`{"promise":"x","promise_confirmed":%v,"structure":[{"section":"s1","priority":"B"}],"cuts":[{"id":1,"targets":["%s"],"category":"preamble","reason":"r","breaks_repro":false,"changes_conclusion":false,"accepted":true}]}`, confirmed, target)
	}
	// 検査のための apply を省いても、確認済みの plan.json をそのまま反映できる
	c := setup(t)
	runDir, _ := c.plan()
	original := readS(t, c.article)
	os.WriteFile(filepath.Join(runDir, "plan.json"), []byte(plan(true, "s1")), 0o644)
	c.must("apply", c.article)
	if readS(t, c.article) == original {
		t.Error("記事に反映される")
	}

	// plan.json を直したあとは、直した内容で検査される（古い検査結果は使われない）
	c2 := setup(t)
	runDir2, _ := c2.plan()
	os.WriteFile(filepath.Join(runDir2, "plan.json"), []byte(plan(false, "s1")), 0o644)
	c2.must("apply", c2.article)
	os.WriteFile(filepath.Join(runDir2, "plan.json"), []byte(plan(true, "u1")), 0o644) // frontmatter は消せない
	if _, e, st := c2.run("apply", c2.article); st != 1 || !strings.Contains(e, "frontmatter") {
		t.Errorf("直した内容が検査される: %d %s", st, e)
	}
}
