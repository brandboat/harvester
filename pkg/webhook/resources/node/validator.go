package node

import (
	"fmt"
	"strconv"

	ctlbatchv1 "github.com/rancher/wrangler/pkg/generated/controllers/batch/v1"
	v1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	kubevirtv1 "kubevirt.io/api/core/v1"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/selection"

	ctlnode "github.com/harvester/harvester/pkg/controller/master/node"
	ctlkubevirtv1 "github.com/harvester/harvester/pkg/generated/controllers/kubevirt.io/v1"
	"github.com/harvester/harvester/pkg/util"
	"github.com/harvester/harvester/pkg/util/virtualmachineinstance"
	werror "github.com/harvester/harvester/pkg/webhook/error"
	"github.com/harvester/harvester/pkg/webhook/types"

	"github.com/rancher/wrangler/pkg/condition"
)

func NewValidator(nodeCache v1.NodeCache, jobCache ctlbatchv1.JobCache, vmiCache ctlkubevirtv1.VirtualMachineInstanceCache) types.Validator {
	return &nodeValidator{
		nodeCache: nodeCache,
		jobCache:  jobCache,
		vmiCache:  vmiCache,
	}
}

type nodeValidator struct {
	types.DefaultValidator
	nodeCache v1.NodeCache
	jobCache  ctlbatchv1.JobCache
	vmiCache  ctlkubevirtv1.VirtualMachineInstanceCache
}

func (v *nodeValidator) Resource() types.Resource {
	return types.Resource{
		Names:      []string{"nodes"},
		Scope:      admissionregv1.ClusterScope,
		APIGroup:   corev1.SchemeGroupVersion.Group,
		APIVersion: corev1.SchemeGroupVersion.Version,
		ObjectType: &corev1.Node{},
		OperationTypes: []admissionregv1.OperationType{
			admissionregv1.Update,
		},
	}
}

func (v *nodeValidator) Update(_ *types.Request, oldObj runtime.Object, newObj runtime.Object) error {
	oldNode := oldObj.(*corev1.Node)
	newNode := newObj.(*corev1.Node)

	nodeList, err := v.nodeCache.List(labels.Everything())
	if err != nil {
		return err
	}

	if err := validateCordonAndMaintenanceMode(oldNode, newNode, nodeList); err != nil {
		return err
	}
	if err := v.validateCPUManagerOperation(newNode); err != nil {
		return err
	}
	return nil
}

func validateCordonAndMaintenanceMode(oldNode, newNode *corev1.Node, nodeList []*corev1.Node) error {
	// if old node already have "maintain-status" annotation or Unscheduleable=true,
	// it has already been enabled, so we skip it
	if _, ok := oldNode.Annotations[ctlnode.MaintainStatusAnnotationKey]; ok || oldNode.Spec.Unschedulable {
		return nil
	}
	// if new node doesn't have "maintain-status" annotation and Unscheduleable=false, we skip it
	if _, ok := newNode.Annotations[ctlnode.MaintainStatusAnnotationKey]; !ok && !newNode.Spec.Unschedulable {
		return nil
	}

	for _, node := range nodeList {
		if node.Name == oldNode.Name {
			continue
		}

		// Return when we find another available node
		if _, ok := node.Annotations[ctlnode.MaintainStatusAnnotationKey]; !ok && !node.Spec.Unschedulable {
			return nil
		}
	}
	return werror.NewBadRequest("can't enable maintenance mode or cordon on the last available node")
}

func (v *nodeValidator) validateCPUManagerOperation(node *corev1.Node) error {
	annot, ok := node.Annotations[util.AnnotationCPUManagerUpdateStatus]
	if !ok {
		return nil
	}

	cpuManagerStatus, err := ctlnode.GetCPUManagerUpdateStatus(annot)
	if err != nil {
		return werror.NewBadRequest("Failed to retreive cpu-manager-update-status from annotation")
	}
	// only validate when update status is requested
	if cpuManagerStatus.Status != ctlnode.CPUManagerRequestedStatus {
		return nil
	}

	policy := cpuManagerStatus.Policy

	// means CPUManager feature gate noe enabled
	cpuManagerLabel, err := strconv.ParseBool(node.Labels[kubevirtv1.CPUManager])
	if err != nil {
		return werror.NewBadRequest("CPUManager label not found")
	}
	// the cpu manager policy is the same
	if ((policy == ctlnode.CPUManagerStaticPolicy) && cpuManagerLabel) || ((policy == ctlnode.CPUManagerNonePolicy) && !cpuManagerLabel) {
		return werror.NewBadRequest("Same cpu manager policy")
	}

	// if this node is master and there are other master still under update policy progress
	// only allow one master node update policy, since we will restart kubelet
	nodes, err := v.nodeCache.List(labels.Everything())
	if err != nil {
		return werror.NewBadRequest("Failed to list nodes")
	}
	if isMasterNodeUpdatingPolicy(nodes) {
		return werror.NewBadRequest("There is other master nodes updating cpu manager policy")
	}

	// if there is other job that still updating cpu manager policy to the same node
	requirement, err := labels.NewRequirement(util.LabelCPUManagerUpdateNode, selection.In, []string{node.GetName()})
	if err != nil {
		return werror.NewBadRequest("Failed to create requirement")
	}
	labelSelector := labels.NewSelector()
	labelSelector.Add(*requirement)
	jobs, err := v.jobCache.List("", labelSelector)
	if err != nil {
		return werror.NewBadRequest("Failed to list jobs")
	}
	for _, job := range jobs {
		if !condition.Cond(batchv1.JobComplete).IsTrue(job) && !condition.Cond(batchv1.JobFailed).IsTrue(job) {
			return werror.NewBadRequest(fmt.Sprintf("There is another ongoing job %s updating cpu manager policy", job.Name))
		}
	}

	// if there is any vm that enable cpu pinning and we want to disable cpu manager
	vmis, err := virtualmachineinstance.ListByNode(node, labels.NewSelector(), v.vmiCache)
	if err != nil {
		return werror.NewBadRequest("Failed to list virtual machine instances")
	}
	if isVMEnableCPUPinning(vmis) && policy == ctlnode.CPUManagerNonePolicy {
		return werror.NewBadRequest("Skip update since there shouldn't have any unstopped vm with cpu pinning when disable cpu manager")
	}

	return nil
}

func isMasterNodeUpdatingPolicy(nodes []*corev1.Node) bool {
	for _, node := range nodes {
		updateStatus, _ := ctlnode.GetCPUManagerUpdateStatus(node.Annotations[util.AnnotationCPUManagerUpdateStatus])
		if ctlnode.IsManagementRole(node) && updateStatus.Status == ctlnode.CPUManagerRunningStatus {
			return true
		}
	}
	return false
}

func isVMEnableCPUPinning(vmis []*kubevirtv1.VirtualMachineInstance) bool {
	for _, vmi := range vmis {
		if vmi.Spec.Domain.CPU != nil && vmi.Spec.Domain.CPU.DedicatedCPUPlacement {
			return true
		}
	}
	return false
}
