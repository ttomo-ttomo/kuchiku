package main

// 設定・パス・引数まわり

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed config.json
var configJSON []byte

type Config struct {
	LowDensityRatio   float64             `json:"lowDensityRatio"`
	RepeatSimilarity  float64             `json:"repeatSimilarity"`
	SummaryRepeatRate float64             `json:"summaryRepeatRate"`
	GenericHeadings   []string            `json:"genericHeadings"`
	SummaryHeadings   []string            `json:"summaryHeadings"`
	StockPhrases      map[string][]string `json:"stockPhrases"`
	Learning          struct {
		MinSamples     int     `json:"minSamples"`
		SuppressBelow  float64 `json:"suppressBelow"`
		ConfidentAbove float64 `json:"confidentAbove"`
	} `json:"learning"`
	PromiseSources []string `json:"promiseSources"`
	StockOrder     []string `json:"-"` // stockPhrases のキーの記載順
}

func loadConfig() *Config {
	var c Config
	if err := json.Unmarshal(configJSON, &c); err != nil {
		panic(err)
	}
	var raw struct {
		S json.RawMessage `json:"stockPhrases"`
	}
	json.Unmarshal(configJSON, &raw)
	dec := json.NewDecoder(bytes.NewReader(raw.S))
	dec.Token()
	for dec.More() {
		k, _ := dec.Token()
		c.StockOrder = append(c.StockOrder, k.(string))
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	return &c
}

// patternRegexp は "/正規表現/" の形なら正規表現を返す（フラグは使わない）
func patternRegexp(p string) *regexp.Regexp {
	if strings.HasPrefix(p, "/") && strings.LastIndex(p, "/") > 0 {
		return regexp.MustCompile(p[1:strings.LastIndex(p, "/")])
	}
	return nil
}

// matchAny は "文字列" か "/正規表現/" の配列のうち、当たったものを返す
func matchAny(text string, patterns []string) []string {
	var hits []string
	for _, p := range patterns {
		if re := patternRegexp(p); re != nil {
			if m := re.FindString(text); m != "" || re.MatchString(text) {
				hits = append(hits, m)
			}
		} else if strings.Contains(text, p) {
			hits = append(hits, p)
		}
	}
	return hits
}

var (
	reDecor   = regexp.MustCompile("[*_`]")
	reLeading = regexp.MustCompile(`^[\s\x{1F000}-\x{1FFFF}\x{2190}-\x{2BFF}\x{FE0F}\x{200D}\x{00A9}\x{00AE}\x{203C}\x{2049}]+`)
	reNumber  = regexp.MustCompile(`^(\d+[.)．、]|第\d+章)\s*`)
)

// cleanHeading は見出しから番号・絵文字・装飾を落とす（「1. まとめ」「🎯 まとめ」→「まとめ」）
func cleanHeading(h string) string {
	h = reDecor.ReplaceAllString(h, "")
	h = reLeading.ReplaceAllString(h, "")
	h = reNumber.ReplaceAllString(h, "")
	return strings.TrimSpace(h)
}

func headingMatches(h string, patterns []string) bool {
	c := cleanHeading(h)
	for _, p := range patterns {
		if re := patternRegexp(p); re != nil {
			if re.MatchString(c) {
				return true
			}
		} else if c == p {
			return true
		}
	}
	return false
}

func findGitRoot(start string) string {
	d, _ := filepath.Abs(start)
	for {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// homeDir は判断ログなどの置き場所。KUCHIKU_HOME > 対象のリポジトリ直下の .kuchiku
func homeDir(articlePath string) string {
	if h := os.Getenv("KUCHIKU_HOME"); h != "" {
		a, _ := filepath.Abs(h)
		return a
	}
	abs, _ := filepath.Abs(articlePath)
	root := findGitRoot(filepath.Dir(abs))
	if root == "" {
		root, _ = os.Getwd()
	}
	return filepath.Join(root, ".kuchiku")
}

func slugOf(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// runSlug は run ディレクトリ名。別のフォルダの同名のファイルとぶつからないよう、リポジトリからの相対パスを使う
func runSlug(articlePath string) string {
	rel := relPath(articlePath)
	if filepath.IsAbs(rel) {
		return slugOf(articlePath)
	}
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	return strings.ReplaceAll(filepath.ToSlash(rel), "/", "__")
}

const homeGitignore = `# kuchiku の作業ファイル。git に入れない。
# decisions.jsonl（判断ログ）と learned.md（傾向）はコミットする。
runs/
overview.md
`

// runDirPath は run ディレクトリの場所だけを返す（作らない）
func runDirPath(articlePath string) string {
	return filepath.Join(homeDir(articlePath), "runs", runSlug(articlePath))
}

func runDir(articlePath string) string {
	home := homeDir(articlePath)
	d := runDirPath(articlePath)
	os.MkdirAll(d, 0o755)
	// 利用者が対象側の .gitignore を編集しなくて済むよう、作業フォルダ自身に置く
	if gi := filepath.Join(home, ".gitignore"); !fileExists(gi) {
		os.WriteFile(gi, []byte(homeGitignore), 0o644)
	}
	return d
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

func relPath(p string) string {
	abs, _ := filepath.Abs(p)
	if root := findGitRoot(filepath.Dir(abs)); root != "" {
		if r, err := filepath.Rel(root, abs); err == nil {
			return r
		}
	}
	return p
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		fail(1, "%v", err)
	}
	return string(b)
}

func fail(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(code)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeJSON(path string, v any, indent string) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	enc.Encode(v)
	os.WriteFile(path, []byte(strings.TrimRight(sb.String(), "\n")), 0o644)
}

// Args は --key value / --flag / 位置引数
type Args struct {
	Pos []string
	Opt map[string][]string
}

func parseArgs(argv []string) Args {
	a := Args{Opt: map[string][]string{}}
	for i := 0; i < len(argv); i++ {
		s := argv[i]
		if strings.HasPrefix(s, "--") {
			key := s[2:]
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
				a.Opt[key] = append(a.Opt[key], "")
			} else {
				a.Opt[key] = append(a.Opt[key], argv[i+1])
				i++
			}
		} else {
			a.Pos = append(a.Pos, s)
		}
	}
	return a
}

type Priority struct {
	No       int    `json:"no"`
	Item     string `json:"item"`
	Priority string `json:"priority"`
}

type Promise struct {
	Text       string     `json:"text"`
	Source     string     `json:"source"`
	Priorities []Priority `json:"priorities"`
}

var (
	reOwnPromise = regexp.MustCompile(`<!--\s*promise[:：]\s*([\s\S]*?)-->`)
	rePromise    = regexp.MustCompile(`(?m)主眼\s*\**\s*[:：]\s*(.+)$`)
	rePlaceholer = regexp.MustCompile(`^[（(].*[）)]$`)
	rePrio       = regexp.MustCompile(`^\**[ABC]\**$`)
	reDigits     = regexp.MustCompile(`^\d+$`)
)

// findPromise は構造シートなどから「主眼」を探す。見つからなければ nil
func findPromise(articlePath string, c *Config) *Promise {
	abs, _ := filepath.Abs(articlePath)
	dir := filepath.Dir(abs)
	root := findGitRoot(dir)
	if root == "" {
		root = dir
	}
	rel := func(p string) string { r, _ := filepath.Rel(root, p); return r }
	slug := slugOf(abs)

	if m := reOwnPromise.FindStringSubmatch(readFile(abs)); m != nil {
		return &Promise{Text: strings.TrimSpace(m[1]), Source: rel(abs)}
	}
	for _, tmpl := range c.PromiseSources {
		d := rel(dir)
		if d == "" {
			d = "."
		}
		pattern := strings.ReplaceAll(strings.ReplaceAll(tmpl, "{slug}", slug), "{dir}", d)
		for _, file := range globSimple(root, pattern) {
			text := readFile(file)
			if found := extractPromise(text); found != "" {
				return &Promise{Text: found, Source: rel(file), Priorities: extractPriorities(text)}
			}
		}
	}
	return nil
}

// extractPromise は「主眼: …」の最後の記入済みのもの（清書側）を取る
func extractPromise(text string) string {
	last := ""
	for _, m := range rePromise.FindAllStringSubmatch(text, -1) {
		t := strings.TrimSpace(strings.ReplaceAll(m[1], "**", ""))
		if t != "" && !rePlaceholer.MatchString(t) {
			last = t
		}
	}
	return last
}

// extractPriorities は構造シートの表から（番号, 章, 優先度）を拾う
func extractPriorities(text string) []Priority {
	rows := []Priority{}
	for _, line := range strings.Split(text, "\n") {
		cells := strings.Split(line, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) < 5 {
			continue
		}
		pr := ""
		for _, c := range cells {
			if rePrio.MatchString(c) {
				pr = strings.ReplaceAll(c, "*", "")
				break
			}
		}
		if pr == "" || !reDigits.MatchString(cells[1]) {
			continue
		}
		var n int
		fmt.Sscanf(cells[1], "%d", &n)
		rows = append(rows, Priority{No: n, Item: cells[2], Priority: pr})
	}
	return rows
}

// globSimple は "*" だけを扱う最小のglob（ディレクトリ部分にはワイルドカードを使わない）
func globSimple(root, pattern string) []string {
	dir := filepath.Join(root, filepath.Dir(pattern))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	base := filepath.Base(pattern)
	var out []string
	for _, e := range ents {
		if ok, _ := filepath.Match(base, e.Name()); ok {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// listArticles は配下の .md を並べる（`.` で始まるフォルダと node_modules は除く）
func listArticles(root string) []string {
	var files []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			files = append(files, p)
		}
		return nil
	})
	return files
}

// runEach は文章1本ならそのまま実行し、ディレクトリなら配下の文章すべてに実行して結果を並べる。
// skip が理由を返した文章は飛ばす。1本でも失敗したら終了コード1
func runEach(arg string, skip func(article string) string, one func(article string) (map[string]any, error)) {
	if !isDir(arg) {
		r, err := one(arg)
		if err != nil {
			fail(1, "%v", err)
		}
		printJSON(r)
		return
	}
	files := listArticles(arg)
	if len(files) == 0 {
		fail(1, "%s に .md がない", arg)
	}
	results := []map[string]any{}
	done, skipped, failed := 0, 0, 0
	for _, f := range files {
		if why := skip(f); why != "" {
			results = append(results, map[string]any{"article": relPath(f), "skipped": why})
			skipped++
			continue
		}
		r, err := one(f)
		if err != nil {
			results = append(results, map[string]any{"article": relPath(f), "ok": false, "error": err.Error()})
			failed++
			continue
		}
		r["article"] = relPath(f)
		results = append(results, r)
		done++
	}
	printJSON(map[string]any{"count": len(files), "done": done, "skipped": skipped, "failed": failed, "articles": results})
	if failed > 0 {
		os.Exit(1)
	}
}
