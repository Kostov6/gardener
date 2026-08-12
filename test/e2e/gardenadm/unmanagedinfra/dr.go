// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package unmanagedinfra

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

			// backupDataPathOnNode is the path to the etcd backup data on the recreated node, passed to
			// 'gardenadm restore --backup-data-path'. It is computed when copying the local backup onto the node.
			backupDataPathOnNode string
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

		It("should simulate a disaster by destroying the control plane node", func(ctx SpecContext) {
			// This mirrors the disaster event in hack/dr-unmanaged-same-node.sh: the control plane node container is
			// stopped and removed together with its volumes, which destroys the etcd data. The container is then
			// recreated (without running gardenadm), leaving the cluster broken until 'gardenadm restore' recovers it.
			By("Stop and remove the control plane node container including its volumes")
			_, _, err := dockerCommand(ctx, "stop", machineContainerName(0))
			Expect(err).NotTo(HaveOccurred())
			_, _, err = dockerCommand(ctx, "rm", "--volumes", machineContainerName(0))
			Expect(err).NotTo(HaveOccurred())

			By("Recreate the control plane node container")
			cmd := exec.CommandContext(ctx, "make", "gind-up", "SCENARIO=machines") // #nosec G204 -- Used for e2e tests only.
			cmd.Dir = filepath.Join("..", "..", "..")
			cmd.Stdout = gexec.NewPrefixedWriter("[out] ", GinkgoWriter)
			cmd.Stderr = gexec.NewPrefixedWriter("[err] ", GinkgoWriter)
			Expect(cmd.Run()).To(Succeed())
		}, SpecTimeout(5*time.Minute))

		It("should copy the local etcd backup onto the recreated node", func(ctx SpecContext) {
			// This mirrors hack/dr-unmanaged-same-node.sh: the etcd backup lives on the host's local disk (the local
			// backup bucket). We locate the '.../etcd-main/v2' backup directory (excluding the garden bucket), copy the
			// whole local-backupbuckets directory onto the node, and compute the on-node --backup-data-path that
			// 'gardenadm restore' will read from.
			localBackupBucketsOnHost := filepath.Join("..", "..", "..", "dev", "local-backupbuckets")

			By("Locate the etcd-main v2 backup directory on the host")
			var backupDataPathOnHost string
			Expect(filepath.WalkDir(localBackupBucketsOnHost, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && filepath.Base(path) == "v2" && !strings.Contains(path, "garden") {
					backupDataPathOnHost = path
				}
				return nil
			})).To(Succeed())
			Expect(backupDataPathOnHost).NotTo(BeEmpty(), "expected to find an etcd-main v2 backup directory under %s", localBackupBucketsOnHost)
			GinkgoWriter.Printf("Found etcd backup data on host at %q\n", backupDataPathOnHost)

			By("Copy the local backup buckets onto the recreated node")
			_, _, err := dockerCommand(ctx, "cp", localBackupBucketsOnHost, machineContainerName(0)+":/local-backupbuckets")
			Expect(err).NotTo(HaveOccurred())

			// Translate the host path to the on-node path: strip the leading "<...>/dev/" and root it at "/", so
			// e.g. "dev/local-backupbuckets/<uid>/.../v2" becomes "/local-backupbuckets/<uid>/.../v2".
			relToBackupBuckets, err := filepath.Rel(localBackupBucketsOnHost, backupDataPathOnHost)
			Expect(err).NotTo(HaveOccurred())
			backupDataPathOnNode = filepath.Join("/local-backupbuckets", relToBackupBuckets)
			GinkgoWriter.Printf("Backup data path on node: %q\n", backupDataPathOnNode)
		}, SpecTimeout(2*time.Minute))

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
