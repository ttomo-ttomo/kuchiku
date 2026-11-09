package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// verify: 2つのMarkdownを比べ、短縮版が「原文から削除しただけ」かを確かめる
func cmdVerify(args Args) {
	if len(args.Pos) < 2 {
		fail(2, "使い方: kuchiku verify <original.md> <shortened.md>")
	}
	res := verify(readFile(args.Pos[0]), readFile(args.Pos[1]))
	if res.OK {
		fmt.Printf("OK: 削除だけでできている（原文 %d 単位のうち %d 単位が残っている）\n", res.Total, res.Kept)
		return
	}
	fmt.Printf("NG: 削除以外の変更が %d 件ある\n", len(res.Problems))
	for _, p := range res.Problems {
		d := ""
		if p.Detail != "" {
			d = "（" + p.Detail + "）"
		}
		fmt.Printf("- [%s] %s%s\n", p.Type, p.Text, d)
	}
	os.Exit(1)
}

// stats: 判断ログの集計を表示する（learned.md も更新する）
func cmdStats(args Args) {
	target, _ := os.Getwd()
	if len(args.Pos) > 0 {
		target = args.Pos[0]
	}
	probe := target
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		abs, _ := filepath.Abs(target)
		probe = filepath.Join(abs, "x.md")
	}
	home := homeDir(probe)
	logPath := filepath.Join(home, "decisions.jsonl")
	ds := readDecisions(logPath)
	if len(ds) == 0 {
		fmt.Printf("判断ログがまだない（%s）\n", logPath)
		return
	}
	s := summarize(latestDecisions(ds), loadConfig())
	writeLearned(filepath.Join(home, "learned.md"), s)
	fmt.Printf("判断 %d件（採用 %d件）  ログ: %s\n\n", s.Total, s.Accepted, logPath)
	for _, r := range s.Rows {
		fmt.Printf("%3d件  採用%3d%%  [%s]  %s\n", r.N, int(math.Round(r.Rate*100)), r.Advice, r.Name)
	}
}
