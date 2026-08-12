// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package unmanagedinfra

import (
	"context"
	"fmt"
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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/gardener/gardener/pkg/utils/kubernetes/health"
)

var _ = Describe("gardenadm unmanaged infrastructure disaster recovery tests", Label("gardenadm", "unmanaged-infra", "dr"), func() {
	Describe("Single-node control plane", Ordered, Label("single"), func() {
		var (
			shootClientSet                   kubernetes.Interface
			gardenClientSet                  kubernetes.Interface
			shootClusterKubeconfigPathOnHost = filepath.Join("..", "..", "..", "dev-setup", "kubeconfigs", "self-hosted-shoot", "kubeconfig")

			shootNamespace        = "garden"
			shootName             = "root"
			controlPlaneNamespace = "kube-system"

			// gardenKubeconfigPathOnNode is where the virtual garden kubeconfig is copied on the recreated node, so that
			// 'gardenadm discover existing' can download the Gardener configuration resources from the garden.
			gardenKubeconfigPathOnNode = "/virtual-garden-kubeconfig"
			// gardenKubeconfigPathOnHost is the virtual garden kubeconfig on the host; it is copied onto the node for discover.
			gardenKubeconfigPathOnHost = filepath.Join("..", "..", "..", "dev-setup", "kubeconfigs", "virtual-garden", "kubeconfig")
			// gardenKubeconfigPathOnMachine is where the garden kubeconfig is placed on the node to run the connect command.
			gardenKubeconfigPathOnMachine = "/tmp/virtual-garden-kubeconfig"
			// configDirOnNode is the directory on the recreated node holding the discovered resources consumed by
			// 'gardenadm restore -d'.
			configDirOnNode = "/gardenadm/discover-output"

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

		It("should ensure the self-hosted shoot is connected and a ShootState exists", func(ctx SpecContext) {
			// 'gardenadm discover existing' (run after the disaster) needs a Shoot and ShootState in the garden. This
			// step is idempotent: if a ShootState already exists (e.g. from a prior run or a connected environment), it
			// does nothing; otherwise it connects the shoot to the garden and drives ShootState creation, mirroring
			// hack/dr-unmanaged-same-node.sh.
			By("Create a client for the garden cluster")
			Eventually(ctx, func() error {
				var err error
				gardenClientSet, err = kubernetes.NewClientFromFile("", gardenKubeconfigPathOnHost,
					kubernetes.WithDisabledCachedClient(),
					kubernetes.WithClientOptions(client.Options{Scheme: kubernetes.GardenScheme}),
				)
				return err
			}).Should(Succeed())

			shootState := &gardencorev1beta1.ShootState{ObjectMeta: metav1.ObjectMeta{Name: shootName, Namespace: shootNamespace}}
			if err := gardenClientSet.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState); err == nil {
				GinkgoWriter.Printf("ShootState %s/%s already exists, skipping connect\n", shootNamespace, shootName)
				return
			}

			By("Copy the garden cluster kubeconfig onto the node")
			gardenKubeconfig, err := os.ReadFile(gardenKubeconfigPathOnHost) // #nosec: G304 -- variable points to a static file path
			Expect(err).NotTo(HaveOccurred())
			Eventually(ctx, func() error {
				_, _, err := execute(ctx, 0, "sh", "-c", fmt.Sprintf("echo '%s' > %s", string(gardenKubeconfig), gardenKubeconfigPathOnMachine))
				return err
			}).Should(Succeed())

			By("Connect the self-hosted shoot to Gardener")
			stdOut, _, err := execute(ctx, 0, "sh", "-c", fmt.Sprintf("KUBECONFIG=%s gardenadm token create --print-connect-command --shoot-namespace=%s --shoot-name=%s", gardenKubeconfigPathOnMachine, shootNamespace, shootName))
			Expect(err).NotTo(HaveOccurred())
			connectCommand := strings.Split(strings.ReplaceAll(string(stdOut.Contents()), `"`, ``), " ")
			stdOut, _, err = execute(ctx, 0, append(connectCommand, "--log-level=debug")...)
			Expect(err).NotTo(HaveOccurred())
			Eventually(ctx, stdOut).Should(gbytes.Say("Your self-hosted shoot cluster has successfully been connected to Gardener!"))

			By("Patch the Shoot status with a successful create lastOperation")
			shoot := &gardencorev1beta1.Shoot{ObjectMeta: metav1.ObjectMeta{Name: shootName, Namespace: shootNamespace}}
			Eventually(ctx, func(g Gomega) {
				g.Expect(gardenClientSet.Client().Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
				patch := client.MergeFrom(shoot.DeepCopy())
				shoot.Status.LastOperation = &gardencorev1beta1.LastOperation{
					Type:  gardencorev1beta1.LastOperationTypeCreate,
					State: gardencorev1beta1.LastOperationStateSucceeded,
				}
				g.Expect(gardenClientSet.Client().Status().Patch(ctx, shoot, patch)).To(Succeed())
			}).Should(Succeed())

			By("Roll out the gardenlet Deployment to trigger ShootState creation")
			gardenletDeployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: controlPlaneNamespace, Name: "gardenlet"}}
			Eventually(ctx, func(g Gomega) {
				g.Expect(shootClientSet.Client().Get(ctx, client.ObjectKeyFromObject(gardenletDeployment), gardenletDeployment)).To(Succeed())
				patch := client.MergeFrom(gardenletDeployment.DeepCopy())
				metav1.SetMetaDataAnnotation(&gardenletDeployment.Spec.Template.ObjectMeta, "kubectl.kubernetes.io/restartedAt", time.Now().Format(time.RFC3339))
				g.Expect(shootClientSet.Client().Patch(ctx, gardenletDeployment, patch)).To(Succeed())
			}).Should(Succeed())
			Eventually(ctx, func(g Gomega) {
				done, err := kubernetesutils.HasDeploymentRolloutCompleted(ctx, shootClientSet.Client(), controlPlaneNamespace, "gardenlet")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(done).To(BeTrue())
			}).Should(Succeed())

			By("Wait until the ShootState is created")
			Eventually(ctx, func() error {
				return gardenClientSet.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState)
			}).Should(Succeed())
		}, SpecTimeout(5*time.Minute))

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

		It("should discover the Gardener configuration resources from the garden", func(ctx SpecContext) {
			// This mirrors hack/dr-unmanaged-same-node.sh: the virtual garden survives the node disaster, so we copy its
			// kubeconfig onto the recreated node and run 'gardenadm discover existing' to download the Gardener
			// configuration resources (Shoot, ShootState, BackupBucket, BackupEntry, CloudProfile, ...) that
			// 'gardenadm restore' consumes.
			By("Copy the virtual garden kubeconfig onto the recreated node")
			_, _, err := dockerCommand(ctx, "cp", gardenKubeconfigPathOnHost, machineContainerName(0)+":"+gardenKubeconfigPathOnNode)
			Expect(err).NotTo(HaveOccurred())

			By("Run gardenadm discover existing")
			_, _, err = execute(ctx, 0, "mkdir", "-p", configDirOnNode)
			Expect(err).NotTo(HaveOccurred())
			_, _, err = execute(ctx, 0, "gardenadm", "discover", "existing",
				"--name", shootName,
				"--namespace", shootNamespace,
				"--kubeconfig", gardenKubeconfigPathOnNode,
				"-d", configDirOnNode,
			)
			Expect(err).NotTo(HaveOccurred())

			By("Remove the self-hosted shoot lease that must not be restored")
			_, _, err = execute(ctx, 0, "rm", "-f", configDirOnNode+"/lease-self-hosted-shoot-"+shootName+".yaml")
			Expect(err).NotTo(HaveOccurred())

			By("List the discovered resources on the node")
			stdOut, _, err := execute(ctx, 0, "ls", "-la", configDirOnNode)
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Printf("Discovered resources in %s:\n%s", configDirOnNode, string(stdOut.Contents()))
		}, SpecTimeout(2*time.Minute))

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

		It("should restore the control plane node", func(ctx SpecContext) {
			// This mirrors the restore step in hack/dr-unmanaged-same-node.sh: 'gardenadm restore' reads the discovered
			// resources and the etcd backup to recover the control plane onto the recreated node.
			Expect(backupDataPathOnNode).NotTo(BeEmpty(), "backup data must have been copied onto the node")

			By("Run gardenadm restore")
			stdOut, _, err := execute(ctx, 0, "gardenadm", "restore",
				"-d", configDirOnNode,
				"--prior-node-name="+machineContainerName(0),
				"--backup-data-path="+backupDataPathOnNode,
				"--log-level=debug",
			)
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Printf("gardenadm restore output:\n%s", string(stdOut.Contents()))
		}, SpecTimeout(10*time.Minute))

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

		It("should verify the control plane restoration", func(ctx SpecContext) {
			// This mirrors hack/dr-verify-restore.sh: it asserts that the etcd data survived the recovery (the workload
			// ConfigMap is still present with its original content) and that the Shoot's identity (UID) was preserved
			// across the disaster (the garden's Shoot .status.uid matches the statusUID in the shoot's shoot-info ConfigMap).

			// The default/experimental-configmap is only seeded by hack/create-workload.sh, which is not run by this test.
			// Verify it only if present, so the spec mirrors dr-verify-restore.sh without failing when no workload was seeded.
			By("Verify the default/experimental-configmap survived recovery (if it was seeded)")
			experimentalConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "experimental-configmap"}}
			if err := shootClientSet.Client().Get(ctx, client.ObjectKeyFromObject(experimentalConfigMap), experimentalConfigMap); err != nil {
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "unexpected error getting default/experimental-configmap")
				GinkgoWriter.Println("default/experimental-configmap not present (workload was not seeded), skipping content check")
			} else {
				Expect(experimentalConfigMap.Data).To(HaveKeyWithValue("content", "experimenting with control plane disaster recovery"),
					"default/experimental-configmap should have survived recovery with its original content")
			}

			By("Read the Shoot UID from the garden cluster")
			shoot := &gardencorev1beta1.Shoot{ObjectMeta: metav1.ObjectMeta{Name: shootName, Namespace: shootNamespace}}
			Expect(gardenClientSet.Client().Get(ctx, client.ObjectKeyFromObject(shoot), shoot)).To(Succeed())
			gardenUID := string(shoot.Status.UID)
			Expect(gardenUID).NotTo(BeEmpty(), "Shoot .status.uid should not be empty in the garden cluster")

			By("Read the statusUID from the kube-system/shoot-info ConfigMap on the self-hosted shoot")
			shootInfo := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: controlPlaneNamespace, Name: "shoot-info"}}
			Expect(shootClientSet.Client().Get(ctx, client.ObjectKeyFromObject(shootInfo), shootInfo)).To(Succeed())

			By("Verify the Shoot UID was preserved across recovery")
			Expect(shootInfo.Data).To(HaveKeyWithValue("statusUID", gardenUID),
				"the shoot-info statusUID should match the garden Shoot .status.uid after recovery")

			GinkgoWriter.Printf("🎉 Success! The control plane Node was successfully restored (Shoot UID %s preserved)\n", gardenUID)
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
