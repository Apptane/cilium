// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package k8s

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/cilium/cilium/api/v1/models"
	"github.com/cilium/cilium/pkg/annotation"
	v2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	slim_corev1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/api/core/v1"
	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/cilium/cilium/pkg/k8s/types"
	"github.com/cilium/cilium/pkg/labels"
	"github.com/cilium/cilium/pkg/policy/api"
)

var (
	unknownObj    = 100
	errUnknownObj = fmt.Errorf("unknown object type %T", unknownObj)
)

func Test_EqualV2CNP(t *testing.T) {
	type args struct {
		o1 *types.SlimCNP
		o2 *types.SlimCNP
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "CNP with the same name",
			args: args{
				o1: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
						},
					},
				},
				o2: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "CNP with the different spec",
			args: args{
				o1: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
						},
						Spec: &api.Rule{
							EndpointSelector: api.NewESFromLabels(labels.NewLabel("foo", "bar", labels.LabelSourceK8s)),
						},
					},
				},
				o2: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
						},
						Spec: nil,
					},
				},
			},
			want: false,
		},
		{
			name: "CNP with the same spec",
			args: args{
				o1: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
						},
						Spec: &api.Rule{},
					},
				},
				o2: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
						},
						Spec: &api.Rule{},
					},
				},
			},
			want: true,
		},
		{
			name: "CNP with different last applied annotations. The are ignored so they should be equal",
			args: args{
				o1: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
							Annotations: map[string]string{
								core_v1.LastAppliedConfigAnnotation: "foo",
							},
						},
						Spec: &api.Rule{},
					},
				},
				o2: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rule1",
							Annotations: map[string]string{
								core_v1.LastAppliedConfigAnnotation: "bar",
							},
						},
						Spec: &api.Rule{},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		got := tt.args.o1.DeepEqual(tt.args.o2)
		require.Equal(t, tt.want, got, "Test Name: %s", tt.name)
	}
}

func Test_EqualV1Endpoints(t *testing.T) {
	type args struct {
		o1 *slim_corev1.Endpoints
		o2 *slim_corev1.Endpoints
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "EPs with the same name",
			args: args{
				o1: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
				},
				o2: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
				},
			},
			want: true,
		},
		{
			name: "EPs with the different spec",
			args: args{
				o1: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
					Subsets: []slim_corev1.EndpointSubset{
						{
							Addresses: []slim_corev1.EndpointAddress{
								{
									IP: "172.0.0.1",
								},
							},
						},
					},
				},
				o2: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
				},
			},
			want: false,
		},
		{
			name: "EPs with the same spec",
			args: args{
				o1: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
					Subsets: []slim_corev1.EndpointSubset{
						{
							Addresses: []slim_corev1.EndpointAddress{
								{
									IP: "172.0.0.1",
								},
							},
						},
					},
				},
				o2: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
					Subsets: []slim_corev1.EndpointSubset{
						{
							Addresses: []slim_corev1.EndpointAddress{
								{
									IP: "172.0.0.1",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "EPs with the same spec (multiple IPs)",
			args: args{
				o1: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
					Subsets: []slim_corev1.EndpointSubset{
						{
							Addresses: []slim_corev1.EndpointAddress{
								{
									IP: "172.0.0.1",
								},
								{
									IP: "172.0.0.2",
								},
							},
						},
					},
				},
				o2: &slim_corev1.Endpoints{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "rule1",
					},
					Subsets: []slim_corev1.EndpointSubset{
						{
							Addresses: []slim_corev1.EndpointAddress{
								{
									IP: "172.0.0.1",
								},
								{
									IP: "172.0.0.2",
								},
							},
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		got := tt.args.o1.DeepEqual(tt.args.o2)
		require.Equal(t, tt.want, got, "Test Name: %s", tt.name)
	}
}

func Test_EqualV1Pod(t *testing.T) {
	type args struct {
		o1 *slim_corev1.Pod
		o2 *slim_corev1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Pods with the same name",
			args: args{
				o1: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
					},
				},
				o2: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
					},
				},
			},
			want: true,
		},
		{
			name: "Pods with the different spec",
			args: args{
				o1: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
				o2: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.1",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Pods with the same spec",
			args: args{
				o1: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
				o2: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Pods with the same spec but different labels",
			args: args{
				o1: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							"foo": "bar",
						},
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
				o2: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Pods with the same spec and same labels",
			args: args{
				o1: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							"foo": "bar",
						},
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
				o2: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							"foo": "bar",
						},
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Pods with differing no-track-port annotations",
			args: args{
				o1: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							"foo": "bar",
						},
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
				o2: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							"foo": "bar",
						},
						Annotations: map[string]string{
							annotation.NoTrack: "53",
						},
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Pods with irrelevant annotations",
			args: args{
				o1: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							"foo": "bar",
						},
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
				o2: &slim_corev1.Pod{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "pod1",
						Labels: map[string]string{
							"foo": "bar",
						},
						Annotations: map[string]string{
							"useless": "80/HTTP",
						},
					},
					Status: slim_corev1.PodStatus{
						HostIP: "127.0.0.1",
						PodIPs: []slim_corev1.PodIP{
							{
								IP: "127.0.0.2",
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		got := tt.args.o1.DeepEqual(tt.args.o2)
		require.Equal(t, tt.want, got, "Test Name: %s", tt.name)
	}
}

func Test_EqualV1Node(t *testing.T) {
	type args struct {
		o1 *slim_corev1.Node
		o2 *slim_corev1.Node
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Nodes with the same name",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
				},
			},
			want: true,
		},
		{
			name: "Nodes with the different names",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node2",
					},
				},
			},
			want: false,
		},
		{
			name: "Nodes with the different spec should return false",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "192.168.0.0/10",
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "127.0.0.1/10",
					},
				},
			},
			want: false,
		},
		{
			name: "Nodes with the same annotations",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
						Annotations: map[string]string{
							annotation.CiliumHostIP: "127.0.0.1",
						},
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
						Annotations: map[string]string{
							annotation.CiliumHostIP: "127.0.0.1",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Nodes with the different annotations",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
						Annotations: map[string]string{
							annotation.CiliumHostIP: "127.0.0.1",
						},
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
						Annotations: map[string]string{
							annotation.CiliumHostIP: "127.0.0.2",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Nodes with the same annotations and different specs should return false",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
						Annotations: map[string]string{
							annotation.CiliumHostIP: "127.0.0.1",
						},
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "192.168.0.0/10",
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
						Annotations: map[string]string{
							annotation.CiliumHostIP: "127.0.0.1",
						},
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "127.0.0.1/10",
					},
				},
			},
			want: false,
		},
		{
			name: "Nodes with the same taints and different specs should return false",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "192.168.0.0/10",
						Taints: []slim_corev1.Taint{
							{
								Key:    "key",
								Value:  "value",
								Effect: "no-effect",
							},
						},
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "127.0.0.1/10",
						Taints: []slim_corev1.Taint{
							{
								Key:    "key",
								Value:  "value",
								Effect: "no-effect",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Nodes with the same taints and different specs should false",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "192.168.0.0/10",
						Taints: []slim_corev1.Taint{
							{
								Key:       "key",
								Value:     "value",
								Effect:    "no-effect",
								TimeAdded: func() *slim_metav1.Time { return &slim_metav1.Time{Time: time.Unix(1, 1)} }(),
							},
						},
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "127.0.0.1/10",
						Taints: []slim_corev1.Taint{
							{
								Key:       "key",
								Value:     "value",
								Effect:    "no-effect",
								TimeAdded: func() *slim_metav1.Time { return &slim_metav1.Time{Time: time.Unix(1, 1)} }(),
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Nodes with the different taints and different specs should return false",
			args: args{
				o1: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					},
					Spec: slim_corev1.NodeSpec{
						PodCIDR: "192.168.0.0/10",
						Taints: []slim_corev1.Taint{
							{
								Key:    "key",
								Value:  "value",
								Effect: "no-effect",
							},
						},
					},
				},
				o2: &slim_corev1.Node{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Node1",
					}, Spec: slim_corev1.NodeSpec{
						PodCIDR: "127.0.0.1/10",
						Taints: []slim_corev1.Taint{
							{
								Key:       "key",
								Value:     "value",
								Effect:    "no-effect",
								TimeAdded: func() *slim_metav1.Time { return &slim_metav1.Time{Time: time.Unix(1, 1)} }(),
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		got := tt.args.o1.DeepEqual(tt.args.o2)
		require.Equal(t, tt.want, got, "Test Name: %s", tt.name)
	}
}

func Test_EqualV1Namespace(t *testing.T) {
	type args struct {
		o1 *slim_corev1.Namespace
		o2 *slim_corev1.Namespace
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Namespaces with the same name",
			args: args{
				o1: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace1",
					},
				},
				o2: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace1",
					},
				},
			},
			want: true,
		},
		{
			name: "Namespaces with the different names",
			args: args{
				o1: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace1",
					},
				},
				o2: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace2",
					},
				},
			},
			want: false,
		},
		{
			name: "Namespaces with the same labels",
			args: args{
				o1: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace1",
						Labels: map[string]string{
							"prod": "true",
						},
					},
				},
				o2: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace1",
						Labels: map[string]string{
							"prod": "true",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Namespaces with the different labels",
			args: args{
				o1: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace1",
						Labels: map[string]string{
							"prod": "true",
						},
					},
				},
				o2: &slim_corev1.Namespace{
					ObjectMeta: slim_metav1.ObjectMeta{
						Name: "Namespace1",
						Labels: map[string]string{
							"prod": "false",
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		got := tt.args.o1.DeepEqual(tt.args.o2)
		require.Equal(t, tt.want, got, "Test Name: %s", tt.name)
	}
}

func Test_TransformToCNP(t *testing.T) {
	type args struct {
		obj any
	}
	tests := []struct {
		name     string
		args     args
		want     any
		expected bool
	}{
		{
			name: "normal transformation",
			args: args{
				obj: &v2.CiliumNetworkPolicy{},
			},
			want: &types.SlimCNP{
				CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{},
			},
			expected: true,
		},
		{
			name: "transformation unneeded",
			args: args{
				obj: &types.SlimCNP{},
			},
			want:     &types.SlimCNP{},
			expected: true,
		},
		{
			name: "delete final state unknown transformation",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: &v2.CiliumNetworkPolicy{},
				},
			},
			want: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{},
				},
			},
			expected: true,
		},
		{
			name: "delete final state unknown transformation with SlimCNP",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: &types.SlimCNP{},
				},
			},
			want: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: &types.SlimCNP{},
			},
			expected: true,
		},
		{
			name: "unknown object type in delete final state unknown transformation",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: unknownObj,
				},
			},
			want:     errUnknownObj,
			expected: false,
		},
		{
			name: "unknown object type in transformation",
			args: args{
				obj: unknownObj,
			},
			want:     errUnknownObj,
			expected: false,
		},
	}
	for _, tt := range tests {
		got, err := TransformToCNP(tt.args.obj)
		if tt.expected {
			require.NoError(t, err)
			require.Equalf(t, tt.want, got, "Test Name: %s", tt.name)
		} else {
			require.Equal(t, tt.want, err, "Test Name: %s", tt.name)
		}
	}
}

func Test_TransformToCCNP(t *testing.T) {
	type args struct {
		obj any
	}
	tests := []struct {
		name     string
		args     args
		want     any
		expected bool
	}{
		{
			name: "normal transformation",
			args: args{
				obj: &v2.CiliumClusterwideNetworkPolicy{},
			},
			want: &types.SlimCNP{
				CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{},
			},
			expected: true,
		},
		{
			name: "transformation unneeded",
			args: args{
				obj: &types.SlimCNP{},
			},
			want:     &types.SlimCNP{},
			expected: true,
		},
		{
			name: "A CCNP where it doesn't contain neither a spec nor specs",
			args: args{
				obj: &v2.CiliumClusterwideNetworkPolicy{},
			},
			want: &types.SlimCNP{
				CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{},
			},
			expected: true,
		},
		{
			name: "delete final state unknown transformation",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: &v2.CiliumClusterwideNetworkPolicy{},
				},
			},
			want: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: &types.SlimCNP{
					CiliumNetworkPolicy: &v2.CiliumNetworkPolicy{},
				},
			},
			expected: true,
		},
		{
			name: "delete final state unknown transformation with SlimCNP",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: &types.SlimCNP{},
				},
			},
			want: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: &types.SlimCNP{},
			},
			expected: true,
		},
		{
			name: "unknown object type in delete final state unknown transformation",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: unknownObj,
				},
			},
			want:     errUnknownObj,
			expected: false,
		},
		{
			name: "unknown object type in transformation",
			args: args{
				obj: unknownObj,
			},
			want:     errUnknownObj,
			expected: false,
		},
	}
	for _, tt := range tests {
		got, err := TransformToCCNP(tt.args.obj)
		if tt.expected {
			require.NoError(t, err)
			require.Equalf(t, tt.want, got, "Test Name: %s", tt.name)
		} else {
			require.Equal(t, tt.want, err, "Test Name: %s", tt.name)
		}
	}
}

func Test_TransformToCiliumEndpoint(t *testing.T) {
	type args struct {
		obj any
	}
	tests := []struct {
		name     string
		args     args
		want     any
		expected bool
	}{
		{
			name: "normal transformation",
			args: args{
				obj: &v2.CiliumEndpoint{},
			},
			want: &types.CiliumEndpoint{
				Encryption: &v2.EncryptionSpec{},
			},
			expected: true,
		},
		{
			name: "transformation unneeded",
			args: args{
				obj: &types.CiliumEndpoint{},
			},
			want:     &types.CiliumEndpoint{},
			expected: true,
		},
		{
			name: "delete final state unknown transformation",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: &v2.CiliumEndpoint{
						TypeMeta: metav1.TypeMeta{
							Kind:       "CiliumEndpoint",
							APIVersion: "v2",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:            "foo",
							GenerateName:    "generated-Foo",
							Namespace:       "bar",
							UID:             "fdadada-dada",
							ResourceVersion: "5454",
							Generation:      5,
							CreationTimestamp: metav1.Time{
								Time: time.Date(2018, 01, 01, 01, 01, 01, 01, time.UTC),
							},
							Labels: map[string]string{
								"foo": "bar",
							},
							Annotations: map[string]string{
								"foo": "bar",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									Kind:       "Pod",
									APIVersion: "v1",
									Name:       "foo",
									UID:        "65dasd54d45",
									Controller: nil,
								},
							},
						},
						Status: v2.EndpointStatus{
							ID:          0,
							Controllers: nil,
							ExternalIdentifiers: &models.EndpointIdentifiers{
								ContainerID:   "3290f4bc32129cb3e2f81074557ad9690240ea8fcce84bcc51a9921034875878",
								ContainerName: "foo",
								K8sNamespace:  "foo",
								K8sPodName:    "bar",
								PodName:       "foo/bar",
							},
							Health: &models.EndpointHealth{
								Bpf:           "good",
								Connected:     false,
								OverallHealth: "excellent",
								Policy:        "excellent",
							},
							Identity: &v2.EndpointIdentity{
								ID: 9654,
								Labels: []string{
									"k8s:io.cilium.namespace=bar",
								},
							},
							Networking: &v2.EndpointNetworking{
								Addressing: []*v2.AddressPair{
									{
										IPV4: "10.0.0.1",
										IPV6: "fd00::1",
									},
								},
								NodeIP: "192.168.0.1",
							},
							Encryption: v2.EncryptionSpec{
								Key: 250,
							},
							Policy: &v2.EndpointPolicy{
								Ingress: &v2.EndpointPolicyDirection{
									Enforcing: true,
								},
								Egress: &v2.EndpointPolicyDirection{
									Enforcing: true,
								},
							},
							State: "",
							NamedPorts: []*models.Port{
								{
									Name:     "foo-port",
									Port:     8181,
									Protocol: "TCP",
								},
							},
						},
					},
				},
			},
			want: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: &types.CiliumEndpoint{
					TypeMeta: slim_metav1.TypeMeta{
						Kind:       "CiliumEndpoint",
						APIVersion: "v2",
					},
					ObjectMeta: slim_metav1.ObjectMeta{
						Name:            "foo",
						Namespace:       "bar",
						UID:             "fdadada-dada",
						ResourceVersion: "5454",
						// We don't need to store labels nor annotations because
						// they are not used by the CEP handlers.
						Labels:      nil,
						Annotations: nil,
					},
					Identity: &v2.EndpointIdentity{
						ID: 9654,
						Labels: []string{
							"k8s:io.cilium.namespace=bar",
						},
					},
					Networking: &v2.EndpointNetworking{
						Addressing: []*v2.AddressPair{
							{
								IPV4: "10.0.0.1",
								IPV6: "fd00::1",
							},
						},
						NodeIP: "192.168.0.1",
					},
					Encryption: &v2.EncryptionSpec{
						Key: 250,
					},
					NamedPorts: []*models.Port{
						{
							Name:     "foo-port",
							Port:     8181,
							Protocol: "TCP",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "unknown object type in delete final state unknown transformation",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: unknownObj,
				},
			},
			want:     errUnknownObj,
			expected: false,
		},
		{
			name: "delete final state unknown transformation with a types.CiliumEndpoint",
			args: args{
				obj: cache.DeletedFinalStateUnknown{
					Key: "foo",
					Obj: &types.CiliumEndpoint{},
				},
			},
			want: cache.DeletedFinalStateUnknown{
				Key: "foo",
				Obj: &types.CiliumEndpoint{},
			},
			expected: true,
		},
		{
			name: "unknown object type in transformation",
			args: args{
				obj: unknownObj,
			},
			want:     errUnknownObj,
			expected: false,
		},
	}
	for _, tt := range tests {
		got, err := TransformToCiliumEndpoint(tt.args.obj)
		if tt.expected {
			require.NoError(t, err)
			require.Equalf(t, tt.want, got, "Test Name: %s", tt.name)
		} else {
			require.Equal(t, tt.want, err, "Test Name: %s", tt.name)
		}
	}
}

func Test_AnnotationsEqual(t *testing.T) {
	irrelevantAnnoKey := "foo"
	irrelevantAnnoVal := "bar"

	relevantAnnoKey := annotation.NoTrack
	relevantAnnoVal1 := ""
	relevantAnnoVal2 := "53"

	// Empty returns true.
	require.True(t, AnnotationsEqual(nil, map[string]string{}, map[string]string{}))

	require.True(t, AnnotationsEqual(nil,
		map[string]string{
			irrelevantAnnoKey: irrelevantAnnoVal,
			relevantAnnoKey:   relevantAnnoVal1,
		}, map[string]string{
			irrelevantAnnoKey: irrelevantAnnoVal,
			relevantAnnoKey:   relevantAnnoVal2,
		}))

	// If the relevant annotation isn't in either map, return true.
	require.True(t, AnnotationsEqual([]string{relevantAnnoKey},
		map[string]string{
			irrelevantAnnoKey: irrelevantAnnoVal,
		}, map[string]string{
			irrelevantAnnoKey: irrelevantAnnoVal,
		}))

	require.False(t, AnnotationsEqual([]string{relevantAnnoKey},
		map[string]string{
			relevantAnnoKey: relevantAnnoVal1,
		}, map[string]string{
			relevantAnnoKey: relevantAnnoVal2,
		}))
}
