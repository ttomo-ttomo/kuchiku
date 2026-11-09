#!/bin/sh
# 配布用の実行ファイルを skills/kuchiku/bin/ に作る（Go 1.22 以上）。コードを変えたら実行してコミットする
set -e
cd "$(dirname "$0")/skills/kuchiku/scripts"
out=../bin
rm -f $out/kuchiku-*
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${t%/*}; arch=${t#*/}; ext=""
  [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$out/kuchiku-$os-$arch$ext" .
done
ls -l $out
