// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package unmanagedinfra

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/utils/kubernetes/health"
)

var _ = Describe("gardenadm unmanaged infrastructure disaster recovery tests", Label("gardenadm", "unmanaged-infra", "dr"), func() {
	Describe("Single-node control plane", Ordered, Label("single"), func() {
		var (
			shootClientSet                   kubernetes.Interface
			shootClusterKubeconfigPathOnHost = filepath.Join("..", "..", "..", "dev-setup", "kubeconfigs", "self-hosted-shoot", "kubeconfig")

			controlPlaneNamespace = "kube-system"
		)

		It("should create a client for the self-hosted shoot API server", func(ctx SpecContext) {
			Eventually(ctx, func() error {
				var err error
				shootClientSet, err = kubernetes.NewClientFromFile("", shootClusterKubeconfigPathOnHost,
					kubernetes.WithDisabledCachedClient(),
					kubernetes.WithClientOptions(client.Options{Scheme: kubernetes.SeedScheme}),
				)
				return err
			}).Should(Succeed())
		}, SpecTimeout(time.Minute))

		It("should trigger an etcd delta snapshot before the disaster", func(ctx SpecContext) {
			// This mirrors triggerEtcdDeltaSnapshot in hack/dr-unmanaged-same-node.sh: etcd-backup-restore only takes
			// deltas on a schedule, so we explicitly trigger a delta to flush the latest cluster state to the backup
			// store before destroying the node. A delta (not a full) is triggered so the recovery path exercises
			// full+delta replay, matching a real disaster. The /snapshot/delta request blocks until the delta is uploaded.
			By("Find the etcd-main pod")
			var etcdMainPod string
			Eventually(ctx, func(g Gomega) {
				podList := &corev1.PodList{}
				g.Expect(shootClientSet.Client().List(ctx, podList, client.InNamespace(controlPlaneNamespace),
					client.MatchingLabels{"app.kubernetes.io/name": "etcd-main"})).To(Succeed())
				g.Expect(podList.Items).NotTo(BeEmpty())
				etcdMainPod = podList.Items[0].Name
			}).Should(Succeed())
			GinkgoWriter.Printf("Triggering delta snapshot via etcd-main pod %q\n", etcdMainPod)

			By("Send an HTTP request for a delta snapshot")
			_, _, err := execute(ctx, 0, "curl", "-sk", "--fail", "https://localhost:8080/snapshot/delta")
			Expect(err).NotTo(HaveOccurred())
		}, SpecTimeout(time.Minute))

		It("should observe that all nodes are ready", func(ctx SpecContext) {
			Eventually(ctx, func(g Gomega) {
				nodeList := &corev1.NodeList{}
				g.Expect(shootClientSet.Client().List(ctx, nodeList)).To(Succeed())
				g.Expect(nodeList.Items).NotTo(BeEmpty())

				for _, node := range nodeList.Items {
					GinkgoWriter.Printf("Node %q: %v\n", node.Name, health.CheckNode(&node))
				}

				for _, node := range nodeList.Items {
					g.Expect(health.CheckNode(&node)).To(Succeed(), "node %q should be healthy", node.Name)
				}
			}).Should(Succeed())
		}, SpecTimeout(5*time.Minute))

		It("should observe that all pods are running", func(ctx SpecContext) {
			Eventually(ctx, func(g Gomega) {
				podList := &corev1.PodList{}
				g.Expect(shootClientSet.Client().List(ctx, podList)).To(Succeed())
				g.Expect(podList.Items).NotTo(BeEmpty())

				for _, pod := range podList.Items {
					GinkgoWriter.Printf("Pod %s/%s: phase=%s (%v)\n", pod.Namespace, pod.Name, pod.Status.Phase, health.CheckPod(&pod))
				}

				for _, pod := range podList.Items {
					g.Expect(health.CheckPod(&pod)).To(Succeed(), "pod %q should be healthy", client.ObjectKeyFromObject(&pod))
				}
			}).Should(Succeed())
		}, SpecTimeout(5*time.Minute))
	})
})

// dockerCommand runs a top-level `docker` command (e.g. stop/rm) on the host and returns its stdout/stderr buffers.
func dockerCommand(ctx context.Context, args ...string) (*gbytes.Buffer, *gbytes.Buffer, error) {
	var stdOutBuffer, stdErrBuffer = gbytes.NewBuffer(), gbytes.NewBuffer()

	cmd := exec.CommandContext(ctx, "docker", args...) // #nosec G204 -- Used for e2e tests only.
	cmd.Stdout = io.MultiWriter(stdOutBuffer, gexec.NewPrefixedWriter("[out] ", GinkgoWriter))
	cmd.Stderr = io.MultiWriter(stdErrBuffer, gexec.NewPrefixedWriter("[err] ", GinkgoWriter))

	return stdOutBuffer, stdErrBuffer, cmd.Run()
}
