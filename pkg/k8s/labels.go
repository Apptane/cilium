// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package k8s

import (
	"log/slog"

	slim_corev1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
	k8sUtils "github.com/cilium/cilium/pkg/k8s/utils"
	"github.com/cilium/cilium/pkg/logging/logfields"
	"github.com/cilium/cilium/pkg/option"
)

// UseOriginalSourceAddressLabel is the k8s label that can be added to a
// `CiliumEnvoyConfig`. This way the Cilium BPF Metadata listener filter is configured
// to use the original source address when extracting the metadata for a request.
//
// Deprecated: use the corresponding annotation
const UseOriginalSourceAddressLabel = "cilium.io/use-original-source-address"

type nameLabelsGetter interface {
	GetName() string
	GetLabels() map[string]string
}

// GetPodMetadata returns the labels and annotations of the pod with the given
// namespace / name.
func GetPodMetadata(logger *slog.Logger, k8sNs nameLabelsGetter, pod *slim_corev1.Pod) (containerPorts []slim_corev1.ContainerPort, lbls map[string]string) {
	namespace := pod.Namespace
	logger.Debug(
		"Connecting to k8s local stores to retrieve labels for pod",
		logfields.K8sNamespace, namespace,
		logfields.K8sPodName, pod.Name,
	)

	objMetaCpy := pod.ObjectMeta.DeepCopy()
	labels := k8sUtils.SanitizePodLabels(objMetaCpy.Labels, k8sNs, pod.Spec.ServiceAccountName, option.Config.ClusterName)

	for _, containers := range pod.Spec.Containers {
		containerPorts = append(containerPorts, containers.Ports...)
	}

	return containerPorts, labels
}
