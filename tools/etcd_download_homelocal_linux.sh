#!/usr/bin/env zsh

ETCD_VER=v3.4.13

# choose either URL
GOOGLE_URL=https://storage.googleapis.com/etcd
GITHUB_URL=https://github.com/etcd-io/etcd/releases/download
DOWNLOAD_URL=${GOOGLE_URL}

rm -f $HOME/tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz
rm -rf $HOME/tmp/etcd-download-test && mkdir -p $HOME/tmp/etcd-download-test

curl -L ${DOWNLOAD_URL}/${ETCD_VER}/etcd-${ETCD_VER}-linux-amd64.tar.gz -o $HOME/tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz
tar xzvf $HOME/tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz -C $HOME/tmp/etcd-download-test --strip-components=1
rm -f $HOME/tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz

$HOME/tmp/etcd-download-test/etcd --version
$HOME/tmp/etcd-download-test/etcdctl version

cp $HOME/tmp/etcd-download-test/etcd $HOME/.local/bin/etcd
cp $HOME/tmp/etcd-download-test/etcdctl $HOME/.local/bin/etcdctl
