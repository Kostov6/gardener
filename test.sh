kubectl apply -f example/provider-local/shoot.yaml
kubectl apply -f example/provider-local/shoot-unconfined.yaml

NAMESPACE=garden-local ./hack/usage/wait-for.sh shoot local APIServerAvailable ControlPlaneHealthy 
NAMESPACE=garden-local ./hack/usage/wait-for.sh shoot local-unconfined APIServerAvailable ControlPlaneHealthy

# Create update to the cluster in a ConfigMap (shoot-local-scheduled-update) to be used by the maintenance controller
kubectl apply -f example/provider-local/shoot-local-scheduled-update-configmap.yaml

# This patch should PASS — shoot has no confineSpecUpdateRollout
kubectl -n garden-local patch shoot local-unconfined --type=merge -p '{"spec":{"provider":{"workers":[{"name":"local","machine":{"type":"local"},"cri":{"name":"containerd"},"minimum":1,"maximum":10,"maxSurge":1,"maxUnavailable":0}]}}}'

# Attempt the same patch on the confined shoot (should be BLOCKED by StagedSpec admission plugin)
kubectl -n garden-local patch shoot local --type=merge -p '{"spec":{"provider":{"workers":[{"name":"local","machine":{"type":"local"},"cri":{"name":"containerd"},"minimum":1,"maximum":10,"maxSurge":1,"maxUnavailable":0}]}}}' && echo "ERROR: patch should have been blocked" || echo "Correctly blocked by StagedSpec admission plugin"

sleep 10

# Force maintenance on the confined shoot, which should use the shoot-local-scheduled-update ConfigMap as the base for maintenance and thus succeed
kubectl -n garden-local annotate shoot local gardener.cloud/operation=maintain
