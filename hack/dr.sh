#!/usr/bin/env bash

set -e

function copy_data() {
    # Retry copy until tar doesn't warn "file changed as we read it"
    max_attempts=20
    success=0
    for i in $(seq 1 "$max_attempts"); do
        rm -rf data
        output=$(kubectl cp gardenadm-unmanaged-infra/machine-0:/var/lib/etcd-main/data data 2>&1 || true)
        echo "$output"
        if echo "$output" | grep -qi 'file changed as we read it'; then
            echo "Attempt $i/$max_attempts: tar reported 'file changed as we read it', retrying..."
            sleep 2
            continue
        fi
        success=1
        break
    done
    if [ "$success" -ne 1 ]; then
        echo "Failed to copy data after $max_attempts attempts due to 'file changed as we read it' error."
        exit 1
    fi
}

# Unmanaged infra 2 worker nodes setup
make kind-single-node-up
export KUBECONFIG=$PWD/example/gardener-local/kind/multi-zone/kubeconfig
make gardenadm-up
kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- gardenadm init -d /gardenadm/resources

JOIN_COMMAND_1=$(kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- gardenadm token create --print-join-command | tr -d '"')
kubectl -n gardenadm-unmanaged-infra exec -it machine-1 -- $JOIN_COMMAND_1

JOIN_COMMAND_2=$(kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- gardenadm token create --print-join-command | tr -d '"')
kubectl -n gardenadm-unmanaged-infra exec -it machine-2 -- $JOIN_COMMAND_2

kubectl -n gardenadm-unmanaged-infra port-forward pod/machine-0 6443:443 >/dev/null 2>&1 &
PF_PID=$!
trap 'kill "$PF_PID" 2>/dev/null || true' EXIT
sleep 1

# Creating workload
kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- cat /etc/kubernetes/admin.conf | sed 's/api.root.garden.local.gardener.cloud/localhost:6443/' > /tmp/shoot--garden--root.conf
export KUBECONFIG=/tmp/shoot--garden--root.conf
./hack/creating-workload.sh
export KUBECONFIG=$PWD/example/gardener-local/kind/multi-zone/kubeconfig

# Get etcd data
rm -rf data
copy_data

# Nuke machine
kill "$PF_PID" 2>/dev/null || true
kubectl -n gardenadm-unmanaged-infra delete pod machine-0 --force
sleep 3

# Prepare for recovery
kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- mkdir -p /var/lib/etcd-main
kubectl cp data/ gardenadm-unmanaged-infra/machine-0:/var/lib/etcd-main/data
kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- gardenadm init -d /gardenadm/resources --bootstrap

kubectl -n gardenadm-unmanaged-infra port-forward pod/machine-0 6443:443 >/dev/null 2>&1 &
PF_PID=$!
trap 'kill "$PF_PID" 2>/dev/null || true' EXIT
sleep 1
kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- cat /etc/kubernetes/admin.conf | sed 's/api.root.garden.local.gardener.cloud/localhost:6443/' > /tmp/shoot--garden--root.conf
export KUBECONFIG=/tmp/shoot--garden--root.conf
rm -rf secrets.yaml
./hack/prep-cluster.sh
export KUBECONFIG=$PWD/example/gardener-local/kind/multi-zone/kubeconfig


rm -rf data
copy_data

# Cleanup gardenadm os artefacts
kill "$PF_PID" 2>/dev/null || true
kubectl -n gardenadm-unmanaged-infra delete pod machine-0 --force
sleep 3

# Recover
kubectl -n gardenadm-unmanaged-infra exec -it machine-0 -- mkdir -p /var/lib/etcd-main

kubectl cp data/ gardenadm-unmanaged-infra/machine-0:/var/lib/etcd-main/data
kubectl cp secrets.yaml gardenadm-unmanaged-infra/machine-0:/secrets.yaml

kubectl -n gardenadm-unmanaged-infra exec -it machine-0 --  gardenadm init -d /gardenadm/resources  --secret-file=/secrets.yaml --use-bootstrap-etcd || true

kubectl -n gardenadm-unmanaged-infra exec -it machine-0 --  gardenadm init -d /gardenadm/resources  --secret-file=/secrets.yaml --use-bootstrap-etcd
