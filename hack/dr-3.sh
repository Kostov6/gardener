#!/usr/bin/env bash

set -e


function copy_data() {
    # Retry copy until tar doesn't warn "file changed as we read it"
    ns=$1
    pod=$2
    max_attempts=20
    success=0
    for i in $(seq 1 "$max_attempts"); do
        rm -rf data
        output=$(kubectl cp "$ns/$pod":/var/lib/etcd-main/data data 2>&1 || true)
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

# Managed infra setup
rm -rf secrets.yaml
rm -rf data
make kind-single-node-up
export KUBECONFIG=$PWD/example/gardener-local/kind/multi-zone/kubeconfig
make gardenadm-up SCENARIO=managed-infra
make gardenadm-up
kubectl label node gardener-operator-local-control-plane "worker.gardener.cloud/pool=control-plane"
export IMAGEVECTOR_OVERWRITE=$PWD/dev-setup/gardenadm/resources/generated/.imagevector-overwrite.yaml
go run ./cmd/gardenadm bootstrap -d ./dev-setup/gardenadm/resources/generated/managed-infra


machine="$(kubectl -n shoot--garden--root get po -l app=machine -oname | head -1 | cut -d/ -f2)"
kubectl -n shoot--garden--root port-forward "pod/${machine}" 6443:443 >/dev/null 2>&1 &
PF_PID=$!
trap 'kill "$PF_PID" 2>/dev/null || true' EXIT
sleep 1

# Creating workload
kubectl -n shoot--garden--root exec -it "$machine" -- cat /etc/kubernetes/admin.conf | sed 's/api.root.garden.local.gardener.cloud/localhost:6443/' > /tmp/shoot--garden--root.conf
export KUBECONFIG=/tmp/shoot--garden--root.conf
./hack/creating-workload.sh
kill "$PF_PID" 2>/dev/null || true
export KUBECONFIG=$PWD/example/gardener-local/kind/multi-zone/kubeconfig

# Get etcd data
rm -rf data
copy_data shoot--garden--root "$machine"

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
./hack/prep-cluster-2.sh "$machine"
rm -rf machine.yaml
kubectl get machine -l name=shoot--garden--root-worker-z1 -o yaml > machine.yaml
sed -i 's/namespace: kube-system/namespace: shoot--garden--root/' machine.yaml


export KUBECONFIG=$PWD/example/gardener-local/kind/multi-zone/kubeconfig

kubectl apply -f machine.yaml

rm -rf data
copy_data gardenadm-unmanaged-infra machine-0


kubectl -n shoot--garden--root scale deploy/machine-controller-manager --replicas=1
kubectl delete machine -l name=shoot--garden--root-control-plane-z1 -A
kubectl -n shoot--garden--root scale deploy/machine-controller-manager --replicas=0

kubectl -n gardener-extension-provider-local-coredns get configmap coredns-custom -o yaml > dns.yaml
IP=$(kubectl get pods -A -o wide | grep machine-shoot--garden--root-control-plane  | grep -o -E "10.0.212.\S+")
sed -i "s/10.0.212../$IP/" dns.yaml
kubectl apply -f dns.yaml


go run ./cmd/gardenadm bootstrap -d ./dev-setup/gardenadm/resources/generated/managed-infra --recover

