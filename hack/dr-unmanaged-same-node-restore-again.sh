#!/usr/bin/env bash

set -euo pipefail

# make gardenadm-up

DATE=$(date '+%Y-%m-%dT%H:%M:%S%z' | sed 's/\([0-9][0-9]\)$$/:\1/g')
LD_FLAGS=$(hack/get-build-ld-flags.sh k8s.io/component-base  VERSION  Gardener $DATE) GOOS=linux GOARCH=arm64 make -B gardenadm
kubectl cp bin/gardenadm gardenadm-unmanaged-infra/machine-0:/gardenadm/gardenadm

# Nuke machine but retain IP address
machine_pod=$(docker exec -it gardener-operator-local-control-plane crictl pods | grep machine-0 | cut -d ' ' -f 1)
machine_container=$(docker exec -it gardener-operator-local-control-plane crictl ps | grep "$machine_pod" | cut -d ' ' -f 1)
docker exec -it gardener-operator-local-control-plane crictl stop "$machine_container"
sleep 100

# Recovery (bootstrap + prep + second phase)
kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- gardenadm init -d /gardenadm/resources --recover --use-bootstrap-etcd
