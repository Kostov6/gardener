// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package unmanagedinfra

import (
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
