package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/go-errors/errors"
	catalogv1 "github.com/rancher/rancher/pkg/generated/controllers/catalog.cattle.io/v1"
	ctlcorev1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/pkg/name"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	kubevirtv1 "kubevirt.io/api/core/v1"

	ctlbatchv1 "github.com/rancher/wrangler/pkg/generated/controllers/batch/v1"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/harvester/harvester/pkg/config"
	v1 "github.com/harvester/harvester/pkg/generated/controllers/kubevirt.io/v1"
	"github.com/harvester/harvester/pkg/util"
	"github.com/harvester/harvester/pkg/util/catalog"
	"github.com/harvester/harvester/pkg/util/virtualmachineinstance"
)

const (
	CPUManagerNodeControllerName = "cpu-manager-node-controller"
	// policy
	CPUManagerStaticPolicy CPUManagerPolicy = "static"
	CPUManagerNonePolicy   CPUManagerPolicy = "none"
	// status
	CPUManagerRunningStatus   CPUManagerStatus = "running"
	CPUManagerRequestedStatus CPUManagerStatus = "requested"
	CPUManagerSuccessStatus   CPUManagerStatus = "success"
	CPUManagerFailedStatus    CPUManagerStatus = "failed"
)

type CPUManagerPolicy string
type CPUManagerStatus string

type CPUManagerUpdateStatus struct {
	policy CPUManagerPolicy
	status CPUManagerStatus
	// err    error
}

// cpuManagerNodeHandler updates cpu manager status of a node in its annotations, so that
// we can tell whether the node is under modifing cpu manager policy or not and its current policy.
type cpuManagerNodeHandler struct {
	appCache   catalogv1.AppCache
	nodeCache  ctlcorev1.NodeCache
	nodeClient ctlcorev1.NodeClient
	jobClient  ctlbatchv1.JobClient
	vmiCache   v1.VirtualMachineInstanceCache
	namespace  string
}

// CPUManagerRegister registers the node controller
func CPUManagerRegister(ctx context.Context, management *config.Management, options config.Options) error {
	app := management.CatalogFactory.Catalog().V1().App()
	job := management.BatchFactory.Batch().V1().Job()
	node := management.CoreFactory.Core().V1().Node()
	vmi := management.VirtFactory.Kubevirt().V1().VirtualMachineInstance()

	cpuManagerNodeHandler := &cpuManagerNodeHandler{
		appCache:   app.Cache(),
		jobClient:  job,
		nodeCache:  node.Cache(),
		nodeClient: node,
		vmiCache:   vmi.Cache(),
		namespace:  options.Namespace,
	}

	node.OnChange(ctx, CPUManagerNodeControllerName, cpuManagerNodeHandler.OnNodeChanged)

	return nil
}

// TODO: how to deal with the error handling ?
// TODO: add log to each skip
// TODO: how do we check if there is kubelet on the node ?
func (h *cpuManagerNodeHandler) OnNodeChanged(_ string, node *corev1.Node) (*corev1.Node, error) {
	if node == nil || node.DeletionTimestamp != nil {
		return node, nil
	}

	if node.Annotations[util.AnnotationCPUManagerUpdateStatus] == "" {
		return node, nil
	}

	cpuManagerStatus, err := getCPUManagerUpdateStatus(node.Annotations[util.AnnotationCPUManagerUpdateStatus])
	if err != nil {
		logrus.WithField("node_name", node.Name).WithError(err).Error("Skip update cpu manager policy, failed to retreive cpu-manager-update-status from annotation")
		return node, nil
	}

	// the cpu manager policy is under prcoess
	if cpuManagerStatus.status != CPUManagerRequestedStatus {
		return h.nodeClient.Update(failed(cpuManagerStatus, node))
	}

	// means CPUManager feature gate noe enabled
	cpuManagerLabel, err := strconv.ParseBool(node.Labels[kubevirtv1.CPUManager])
	if err != nil {
		return h.nodeClient.Update(failed(cpuManagerStatus, node))
	}
	// the cpu manager policy is the same
	if ((cpuManagerStatus.policy == CPUManagerStaticPolicy) && cpuManagerLabel) || ((cpuManagerStatus.policy == CPUManagerNonePolicy) && !cpuManagerLabel) {
		return h.nodeClient.Update(failed(cpuManagerStatus, node))
	}

	// if there is any vm that enable cpu pinning and we want to disable cpu manager
	vmis, err := virtualmachineinstance.ListByNode(node, labels.NewSelector(), h.vmiCache)
	if err != nil {
		return h.nodeClient.Update(failed(cpuManagerStatus, node))
	}
	if isVMEnableCPUPinning(vmis) && cpuManagerStatus.policy == CPUManagerNonePolicy {
		logrus.WithField("node_name", node.Name).Info("Skip update since there shouldn't have any unstopped vm with cpu pinning when disable cpu manager")
		return h.nodeClient.Update(failed(cpuManagerStatus, node))
	}

	// if this node is master and there are other master still under update policy progress
	// only allow one master node update policy, since we will restart kubelet
	nodes, err := h.nodeCache.List(labels.Everything())
	if err != nil {
		return h.nodeClient.Update(failed(cpuManagerStatus, node))
	}
	if isMasterNodeUpdatingPolicy(nodes) {
		return h.nodeClient.Update(failed(cpuManagerStatus, node))
	}

	return h.nodeClient.Update(running(cpuManagerStatus, node))
}

func getCPUManagerUpdateStatus(jsonString string) (*CPUManagerUpdateStatus, error) {
	cpuManagerStatus := &CPUManagerUpdateStatus{}
	if err := json.Unmarshal([]byte(jsonString), cpuManagerStatus); err != nil {
		return nil, err
	}
	if cpuManagerStatus.policy == "" {
		return nil, errors.New("invalid policy")
	}
	if cpuManagerStatus.status == "" {
		return nil, errors.New("invalid status")
	}
	return cpuManagerStatus, nil
}

func isVMEnableCPUPinning(vmis []*kubevirtv1.VirtualMachineInstance) bool {
	for _, vmi := range vmis {
		if vmi.Spec.Domain.CPU != nil && vmi.Spec.Domain.CPU.DedicatedCPUPlacement {
			return true
		}
	}
	return false
}

func isMasterNodeUpdatingPolicy(nodes []*corev1.Node) bool {
	for _, node := range nodes {
		updateStatus, _ := getCPUManagerUpdateStatus(node.Annotations[util.AnnotationCPUManagerUpdateStatus])
		if isManagementRole(node) && updateStatus.status == CPUManagerRunningStatus {
			return true
		}
	}
	return false
}

func failed(updateStatus *CPUManagerUpdateStatus, node *corev1.Node) *corev1.Node {
	updateStatus.status = CPUManagerFailedStatus
	jsonStr, err := json.Marshal(updateStatus)
	// this shouldn't happen
	if err != nil {
		logrus.WithField("node_name", node.Name).Errorf("Failed to marshal cpu manager update status to json string %v", err)
	}

	toUpdate := node.DeepCopy()
	toUpdate.Annotations[util.AnnotationCPUManagerUpdateStatus] = string(jsonStr)
	return toUpdate
}

func running(updateStatus *CPUManagerUpdateStatus, node *corev1.Node) *corev1.Node {
	updateStatus.status = CPUManagerRunningStatus
	jsonStr, err := json.Marshal(updateStatus)
	// this shouldn't happen
	if err != nil {
		logrus.WithField("node_name", node.Name).Errorf("Failed to marshal cpu manager update status to json string %v", err)
	}

	toUpdate := node.DeepCopy()
	toUpdate.Annotations[util.AnnotationCPUManagerUpdateStatus] = string(jsonStr)
	return toUpdate
}

// TODO: wait until cpumanager is true ?
func getScript(nodeName string, policy CPUManagerPolicy) string {
	return fmt.Sprintf(`
set -e;
echo "Start update cpu-manager-policy option...";
KUBELET_CONFIG_FILE="/host/etc/rancher/rke2/config.yaml.d/99-z01-harvester-cpu-manager.yaml";
CPU_MANAGER_STATE_FILE="/host/var/lib/kubelet/cpu_manager_state";
NODE_NAME="%s";
NODE_POLICY="%s";
sed -i "s/cpu-manager-policy=$CURRENT_POLICY/cpu-manager-policy=$NODE_POLICY/" "$KUBELET_CONFIG_FILE";
echo "Updated CPU manager policy for $NODE_NAME to $NODE_POLICY in $KUBELET_CONFIG_FILE.";
rm -f "$CPU_MANAGER_STATE_FILE";
echo "Removed $CPU_MANAGER_STATE_FILE.";
if chroot /host systemctl is-active --quiet rke2-server; then
	echo "Restarting rke2-server."
	chroot /host systemctl restart rke2-server
	echo "Restarted rke2-server."
elif chroot /host systemctl is-active --quiet rke2-agent; then
	echo "Restarting rke2-agent."
	chroot /host systemctl restart rke2-agent
	echo "Restarted rke2-agent."
else
	echo "Neither rke2-server nor rke2-agent are running. No services restarted."
fi`, nodeName, policy)
}

// TODO: /host not found
func (h *cpuManagerNodeHandler) getJob(updateStatus *CPUManagerUpdateStatus, node *corev1.Node, image string) *batchv1.Job {
	hostPathDirectory := corev1.HostPathDirectory
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.SafeConcatName(node.Name, "update-cpu-manager"),
			Namespace: h.namespace,
			Labels: map[string]string{
				util.LabelCPUManagerUpdate: node.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: node.APIVersion,
					Kind:       node.Kind,
					Name:       node.Name,
					UID:        node.UID,
				},
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To[int32](86400), // TODO ???
			ActiveDeadlineSeconds:   ptr.To[int64](300),   // TODO ???
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						util.LabelCPUManagerUpdate: node.Name,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "update-cpu-manager",
							Image:   image,
							Command: []string{"bash", "-c"},
							Args:    []string{getScript(node.Name, updateStatus.policy)},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "host-root", MountPath: "/host"},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: ptr.To[bool](true),
							},
						},
					},
					ServiceAccountName: "harvester",
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{{
									MatchExpressions: []corev1.NodeSelectorRequirement{{
										Key:      corev1.LabelHostname,
										Operator: corev1.NodeSelectorOpIn,
										Values: []string{
											node.Name,
										},
									}},
								}},
							},
						},
					},
					Volumes: []corev1.Volume{{
						Name: `host-root`,
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/",
								Type: &hostPathDirectory,
							},
						},
					}},
				},
			},
		},
	}
}

func (h *cpuManagerNodeHandler) submitJob(updateStatus *CPUManagerUpdateStatus, node *corev1.Node) error {
	image, err := catalog.FetchAppChartImage(h.appCache, h.namespace, releaseAppHarvesterName, []string{"generalJob", "image"})
	if err != nil {
		return fmt.Errorf("failed to get harvester image (%s): %v", image.ImageName(), err)
	}

	_, err = h.jobClient.Create(h.getJob(updateStatus, node, image.ImageName()))

	if err != nil {
		return err
	}

	return nil
}
