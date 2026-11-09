---
title: "git worktree でレビューと実装を同時に進める"
emoji: "🌲"
type: "tech"
topics: ["git"]
published: false
---
<!-- promise: git worktree を使うと、作業中のブランチを切り替えずに別ブランチの PR をレビューできる -->

## はじめに

本記事では、git worktree を活用することで、開発効率を大幅に向上させる方法について解説します。
現代のソフトウェア開発において、複数のタスクを並行して進めることは非常に重要です。
さまざまな場面で役立つテクニックなので、ぜひ最後までご覧ください。

## git worktree とは？

git worktree とは、1 つのリポジトリに対して複数の作業ディレクトリを持てる機能です。
Git 2.5 で追加されました。
この機能を活用することで、柔軟な開発フローを実現することができます。

## git worktree のメリット

git worktree には、次のようなメリットがあります。

- 生産性の向上: 作業を中断せずに済むため、生産性が大幅に向上します
- 品質の向上: レビューに集中できるため、コードの品質が向上します
- 柔軟性: さまざまな開発フローに柔軟に対応できます

これらのメリットにより、チーム全体の開発効率が向上すると言えるでしょう。

## 実装中に PR をレビューする手順

実装中のブランチ `feature/login` はそのままにして、レビュー対象の `fix/timeout` を別のディレクトリに取り出します。

```bash
git fetch origin fix/timeout
git worktree add ../review-timeout origin/fix/timeout
cd ../review-timeout && npm ci && npm test
```

`npm ci` が必要なのは、`node_modules` が作業ディレクトリごとに別になるためです。
手元で 1,200 個のパッケージを入れ直すのに約 40 秒かかりました。

レビューが終わったら、作業ディレクトリを消します。

```bash
cd - && git worktree remove ../review-timeout
```

`remove` は未コミットの変更があると止まるので、レビュー中に書いたメモを消してしまう心配はありません。

## 注意点

同じブランチを 2 つの作業ディレクトリで同時に開くことはできません。
`fatal: 'fix/timeout' is already checked out` と出たら、`git worktree list` でどこで開いているかを確認します。
また、git worktree を使う際には、チームのルールに従って適切に運用することが大切です。

## まとめ

本記事では、git worktree を使って、作業中のブランチを切り替えずに PR をレビューする方法を解説しました。

- git worktree を使えば、作業中のブランチを切り替えずに PR をレビューできます
- レビューが終わったら git worktree remove で作業ディレクトリを消します
- 同じブランチを 2 つの作業ディレクトリで同時に開くことはできません

git worktree は、今後ますます重要になっていく技術だと言っても過言ではありません。
いかがでしたか？参考になれば幸いです。
