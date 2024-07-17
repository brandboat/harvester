package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// func fakeNode() *NodeBuilder {
// 	return &NodeBuilder{
// 		node: &corev1.Node{
// 			ObjectMeta: metav1.ObjectMeta{
// 				Labels:      map[string]string{},
// 				Annotations: map[string]string{},
// 			},
// 		},
// 	}
// }

// func (n *NodeBuilder) CPUManagerUpdateStatus(policy CPUManagerPolicy, status CPUManagerStatus) *NodeBuilder {
// 	updateStatus := &CPUManagerUpdateStatus{
// 		Policy: policy,
// 		Status: status,
// 	}
// 	jsonStr, _ := json.Marshal(updateStatus)
// 	n.node.Annotations[util.AnnotationCPUManagerUpdateStatus] = string(jsonStr)
// 	return n
// }

// func (n *NodeBuilder) Annotation(key string, value string) *NodeBuilder {
// 	n.node.Annotations[key] = value
// 	return n
// }

// // TODO: remove this
//
//	func Test_GetJob(t *testing.T) {
//		node := &corev1.Node{
//			ObjectMeta: metav1.ObjectMeta{
//				Name: "harvester-node-0",
//				Annotations: map[string]string{
//					util.AnnotationCPUManagerUpdateStatus: `{"policy": "foobar", "status": "requested"}`},
//			},
//		}
//		k8sclientset := k8sfake.NewSimpleClientset(node)
//		// var clientset = fake.NewSimpleClientset()
//		var handler = &cpuManagerNodeHandler{
//			nodeCache:  fakeclients.NodeCache(k8sclientset.CoreV1().Nodes),
//			nodeClient: fakeclients.NodeClient(k8sclientset.CoreV1().Nodes),
//			// jobClient:  fakeclients.JobClient(k8sclientset.BatchV1().Jobs),
//			// vmiCache:  fakeclients.VirtualMachineInstanceCache(clientset.KubevirtV1().VirtualMachineInstances),
//			namespace: "harveseter-system",
//		}
//		res, _ := yaml.Marshal(handler.getJob(CPUManagerNonePolicy, node, "registry.suse.com/bci/bci-base:15.5"))
//		fmt.Println(string(res))
//	}
func Test_foo(t *testing.T) {
	assert.Equal(t, 1, 1)
}

// func Test_CPUManagerController(t *testing.T) {
// 	type input struct {
// 		key  string
// 		node *v1.Node
// 	}
// 	type output struct {
// 		node   *v1.Node
// 		errMsg string
// 	}
// 	var testCases = []struct {
// 		name     string
// 		given    input
// 		expected output
// 	}{
// 		{
// 			name: "incorrect json",
// 			given: input{
// 				key: "test",
// 				node: &corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node-1",
// 						Annotations: map[string]string{
// 							util.AnnotationCPUManagerUpdateStatus: `{"policy":"foobar","status":"requested"}`},
// 					},
// 				},
// 			},
// 			expected: output{
// 				node: &corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node-1",
// 						Annotations: map[string]string{
// 							util.AnnotationCPUManagerUpdateStatus: `{"policy":"foobar","status":"failed"}`},
// 					},
// 				},
// 				errMsg: "",
// 			},
// 		},
// 	}
// 	for _, tc := range testCases {
// 		k8sclientset := k8sfake.NewSimpleClientset(tc.given.node)
// 		var handler = &cpuManagerNodeHandler{
// 			nodeCache:  fakeclients.NodeCache(k8sclientset.CoreV1().Nodes),
// 			nodeClient: fakeclients.NodeClient(k8sclientset.CoreV1().Nodes),
// 		}

// 		changedNode, err := handler.OnNodeChanged(tc.given.key, tc.given.node)
// 		if tc.expected.errMsg != "" {
// 			assert.NotNil(t, err)
// 			assert.Equal(t, tc.expected.errMsg, err.Error())
// 		}
// 		assert.Equal(t, tc.expected.node, changedNode, "case %q", tc.name)
// 	}
// }
