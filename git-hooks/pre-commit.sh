#!/usr/bin/env bash

set -exuo pipefail

HERE=$(realpath $(dirname ${BASH_SOURCE[0]}))
echo "Generating skeets for new posts..."
pushd ${HERE}/../../git-hooks >/dev/null
hugo list published -s .. |  go run . -u joshghiloni.me
git status --porcelain
stagedChangeCount=$(git status --porcelain | grep -v '^??' | grep -v '^A ' | wc -l || true)
if [[ $stagedChangeCount > 0 ]]; then
    git commit --amend --no-edit --gpg-sign --signoff --no-verify --all
fi
popd >/dev/null
