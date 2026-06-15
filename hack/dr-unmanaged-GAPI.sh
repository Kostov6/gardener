
echo "> Setting up Gardener control plane in the kind cluster..."
make kind-up
make gardenadm-up SCENARIO=connect-kind

echo "> Sanity checking that gardener-apiserver is running..."
VIRTUAL_GARDEN_KUBECONFIG="./dev-setup/kubeconfigs/virtual-garden/kubeconfig"
kubectl --kubeconfig "$VIRTUAL_GARDEN_KUBECONFIG" get namespaces

