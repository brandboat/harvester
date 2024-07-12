package node

import (
	"encoding/json"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/assert"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/harvester/harvester/pkg/generated/clientset/versioned/fake"
	"github.com/harvester/harvester/pkg/util"
	"github.com/harvester/harvester/pkg/util/fakeclients"
)

func fakeNode() *NodeBuilder {
	return &NodeBuilder{
		node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
		},
	}
}

func (n *NodeBuilder) CPUManagerUpdateStatus(policy CPUManagerPolicy, status CPUManagerStatus) *NodeBuilder {
	updateStatus := &CPUManagerUpdateStatus{
		policy: policy,
		status: status,
	}
	jsonStr, _ := json.Marshal(updateStatus)
	n.node.Annotations[util.AnnotationCPUManagerUpdateStatus] = string(jsonStr)
	return n
}

func (n *NodeBuilder) Annotation(key string, value string) *NodeBuilder {
	n.node.Annotations[key] = value
	return n
}

func Test_GetJob(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Annotations: map[string]string{
				util.AnnotationCPUManagerUpdateStatus: `{"policy": "foobar", "status": "requested"}`},
		},
	}
	k8sclientset := k8sfake.NewSimpleClientset(node)
	var clientset = fake.NewSimpleClientset()
	var handler = &cpuManagerNodeHandler{
		nodeCache: fakeclients.NodeCache(k8sclientset.CoreV1().Nodes),
		vmiCache:  fakeclients.VirtualMachineInstanceCache(clientset.KubevirtV1().VirtualMachineInstances),
	}
	updateStatus := CPUManagerUpdateStatus{
		policy: CPUManagerStaticPolicy,
		status: CPUManagerRequestedStatus,
	}
	res, _ := yaml.Marshal(handler.getJob(&updateStatus, node, "registry.suse.com/bci/bci-base:15.5"))
	fmt.Println(string(res))
}

func Test_CPUManagerController(t *testing.T) {
	type input struct {
		key  string
		node *v1.Node
	}
	type output struct {
		node   *v1.Node
		errMsg string
	}
	var testCases = []struct {
		name     string
		given    input
		expected output
	}{
		{
			name: "incorrect json",
			given: input{
				key: "test",
				node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Annotations: map[string]string{
							util.AnnotationCPUManagerUpdateStatus: `{"policy": "foobar", "status": "requested"}`},
					},
				},
			},
			expected: output{
				node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Annotations: map[string]string{
							util.AnnotationCPUManagerUpdateStatus: `{"policy": "foobar", "status": "requested"}`},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		k8sclientset := k8sfake.NewSimpleClientset(tc.given.node)
		var clientset = fake.NewSimpleClientset()
		var handler = &cpuManagerNodeHandler{
			nodeCache: fakeclients.NodeCache(k8sclientset.CoreV1().Nodes),
			vmiCache:  fakeclients.VirtualMachineInstanceCache(clientset.KubevirtV1().VirtualMachineInstances),
		}

		changedNode, err := handler.OnNodeChanged(tc.given.key, tc.given.node)
		if tc.expected.errMsg != "" {
			assert.NotNil(t, err)
			assert.Equal(t, tc.expected.errMsg, err.Error())
		}
		assert.Equal(t, tc.expected.node, changedNode, "case %q", tc.name)
	}
}
