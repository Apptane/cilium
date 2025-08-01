// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package ciliumendpointslice

import (
	"context"
	"fmt"

	k8s "github.com/cilium/cilium/pkg/annotation"
	"github.com/cilium/cilium/pkg/k8s/resource"
	slimcorev1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
	"github.com/cilium/cilium/pkg/logging/logfields"
)

const (
	priorityNamespaceAnnotation = "cilium.io/ces-namespace"
)

func (c *Controller) processNamespaceEvents(ctx context.Context) error {
	for event := range c.namespace.Events(ctx) {
		switch event.Kind {
		case resource.Upsert:
			c.logger.DebugContext(ctx, "Got Upsert Namespace event ", logfields.K8sNamespace, event.Key)

			c.onNamespaceUpsert(event.Object)
		case resource.Delete:
			c.logger.DebugContext(ctx, "Got Delete Namespace event ", logfields.K8sNamespace, event.Key)

			c.onNamespaceDelete(event.Object)
		}
		event.Done(nil)
	}
	return nil
}

// onNamespaceUpsert modifies the Controller's list of priority namespaces if the namespace is modified.
func (c *Controller) onNamespaceUpsert(ns *slimcorev1.Namespace) {
	value, _ := k8s.Get(ns, priorityNamespaceAnnotation)
	c.priorityNamespacesLock.Lock()
	defer c.priorityNamespacesLock.Unlock()
	if value == "priority" {
		c.logger.Debug(fmt.Sprintf("Namespace has a priority annotation %s", ns.Name))
		c.priorityNamespaces[ns.Name] = struct{}{}
	} else {
		c.logger.Debug(fmt.Sprintf("Namespace does not have priority: %s", ns.Name))
		_, ok := c.priorityNamespaces[ns.Name]
		if ok {
			c.logger.Debug(fmt.Sprintf("Namespace %s removed from priority list.", ns.Name))
		}
		delete(c.priorityNamespaces, ns.Name)
	}
}

// onNamespaceDelete deletes the namespace from the Controller's list of priority namespaces
// if the namespace is deleted.
func (c *Controller) onNamespaceDelete(ns *slimcorev1.Namespace) {
	c.logger.Debug(fmt.Sprintf("Namespace deleted: %s", ns.Name))
	c.priorityNamespacesLock.Lock()
	defer c.priorityNamespacesLock.Unlock()
	delete(c.priorityNamespaces, ns.Name)

}
