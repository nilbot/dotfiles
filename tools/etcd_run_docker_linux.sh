#!/usr/bin/env zsh

docker stop etcd && docker rm etcd

HOST_IP=$(ifconfig | grep -A 1 '^eth0' | grep '\sinet' | awk '{print $2}')
export NODE1=$HOST_IP

docker volume create --name etcd-data
export DATA_DIR="etcd-data"

REGISTRY=quay.io/coreos/etcd
# available from v3.2.5
REGISTRY=gcr.io/etcd-development/etcd

docker run -d \
  -p 2379:2379 \
  -p 2380:2380 \
  --volume=${DATA_DIR}:/etcd-data \
  --name etcd ${REGISTRY}:latest \
  /usr/local/bin/etcd \
  --data-dir=/etcd-data --name node1 \
  --initial-advertise-peer-urls http://${NODE1}:2380 --listen-peer-urls http://0.0.0.0:2380 \
  --advertise-client-urls http://${NODE1}:2379 --listen-client-urls http://0.0.0.0:2379 \
  --initial-cluster node1=http://${NODE1}:2380

etcdctl --endpoints=http://${NODE1}:2379 member list