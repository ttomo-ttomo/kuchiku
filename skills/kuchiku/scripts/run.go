package main

// run ディレクトリ（plan の出力）を読む

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Meta struct {
	Article    string `json:"article"`
	ArticleRel string `json:"articleRel"`
	Hash       string `json:"hash"`
	CreatedAt  string `json:"createdAt"`
	BodyChars  int    `json:"bodyChars"`
}

type Run struct {
	Dir    string
	Source string
	Meta   Meta   `json:"meta"`
	Doc    *Doc   `json:"doc"`
	Hints  *Hints `json:"hints"`
}

func loadRun(arg string) (*Run, error) {
	dir := arg
	if st, err := os.Stat(arg); err != nil || !st.IsDir() {
		dir = runDir(arg)
	}
	p := filepath.Join(dir, "units.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("%s がない。先に plan を実行する", p)
	}
	r := &Run{Dir: dir}
	if err := json.Unmarshal(b, r); err != nil {
		return nil, err
	}
	sb, err := os.ReadFile(r.Meta.Article)
	if err != nil {
		return nil, err
	}
	r.Source = string(sb)
	if sha(r.Source) != r.Meta.Hash {
		return nil, fmt.Errorf("plan の後に文章が変更されている。plan からやり直す（単位のIDがずれるため）")
	}
	return r, nil
}

func bodyChars(source string) int { return charCount(bodyText(segment(source))) }
