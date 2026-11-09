// kuchiku: 削除だけで文章を縮める
//
//	kuchiku plan   <文章.md | ディレクトリ>   # 分析する。plan.json があれば検査して結果を見せる（文章は変えない）
//	kuchiku apply  <文章.md | ディレクトリ>   # plan.json を検査して見せる。promise_confirmed が true なら、accepted の案を反映する
//	kuchiku verify  <original.md> <shortened.md>
//	kuchiku stats   [文章.md | リポジトリのパス]
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fail(2, "使い方: kuchiku plan|apply|verify|stats ...")
	}
	args := parseArgs(os.Args[2:])
	for k := range args.Opt {
		fail(2, "不明なオプション: --%s（kuchiku にオプションはない）", k)
	}
	switch os.Args[1] {
	case "plan":
		cmdPlan(args)
	case "apply":
		cmdApply(args)
	case "verify":
		cmdVerify(args)
	case "stats":
		cmdStats(args)
	default:
		fail(2, "不明なコマンド: %s", os.Args[1])
	}
}

// fmtInt は 12345 → "12,345"
func fmtInt(n int) string {
	s := fmt.Sprint(n)
	neg := ""
	if n < 0 {
		neg, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return neg + s
}
