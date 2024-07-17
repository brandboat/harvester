package node

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-errors/errors"
	catalogv1 "github.com/rancher/rancher/pkg/generated/controllers/catalog.cattle.io/v1"
	"github.com/rancher/wrangler/pkg/condition"
	ctlcorev1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/pkg/name"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	ctlbatchv1 "github.com/rancher/wrangler/pkg/generated/controllers/batch/v1"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/harvester/harvester/pkg/config"
	"github.com/harvester/harvester/pkg/util"
	"github.com/harvester/harvester/pkg/util/catalog"
)

const (
	CPUManagerControllerName = "cpu-manager-controller"
	// policy
	CPUManagerStaticPolicy CPUManagerPolicy = "static"
	CPUManagerNonePolicy   CPUManagerPolicy = "none"
	// status
	CPUManagerRequestedStatus CPUManagerStatus = "requested"
	CPUManagerRunningStatus   CPUManagerStatus = "running"
	CPUManagerSuccessStatus   CPUManagerStatus = "success"
	CPUManagerFailedStatus    CPUManagerStatus = "failed"

	HostDir                     string = "/host"
	ScriptWaitLabelTimeoutInSec int64  = 300 // 5 min
	JobTimeoutInSec             int64  = ScriptWaitLabelTimeoutInSec * 2
	JobTTLInSec                 int32  = 86400 * 7 // 7 day
)

type CPUManagerPolicy string
type CPUManagerStatus string

type CPUManagerUpdateStatus struct {
	Policy  CPUManagerPolicy `json:"policy"`
	Status  CPUManagerStatus `json:"status"`
	JobName string           `json:"jobName,omitempty"`
}

// cpuManagerNodeHandler updates cpu manager status of a node in its annotations, so that
// we can tell whether the node is under modifing cpu manager policy or not and its current policy.
type cpuManagerNodeHandler struct {
	appCache   catalogv1.AppCache
	nodeCache  ctlcorev1.NodeCache
	nodeClient ctlcorev1.NodeClient
	jobClient  ctlbatchv1.JobClient
	// vmiCache   v1.VirtualMachineInstanceCache
	namespace string
}

// CPUManagerRegister registers the node controller
func CPUManagerRegister(ctx context.Context, management *config.Management, options config.Options) error {
	app := management.CatalogFactory.Catalog().V1().App()
	job := management.BatchFactory.Batch().V1().Job()
	node := management.CoreFactory.Core().V1().Node()
	// vmi := management.VirtFactory.Kubevirt().V1().VirtualMachineInstance()

	cpuManagerNodeHandler := &cpuManagerNodeHandler{
		appCache:   app.Cache(),
		jobClient:  job,
		nodeCache:  node.Cache(),
		nodeClient: node,
		// vmiCache:   vmi.Cache(),
		namespace: options.Namespace,
	}

	node.OnChange(ctx, CPUManagerControllerName, cpuManagerNodeHandler.OnNodeChanged)
	job.OnChange(ctx, CPUManagerControllerName, cpuManagerNodeHandler.OnJobChanged)

	return nil
}

// // the cpu manager policy is under prcoess
// if cpuManagerStatus.Status != CPUManagerRequestedStatus {
// 	logrus.WithField("node_name", node.Name).Error("Update cpu manager policy is in progress")
// 	return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
// }

// // means CPUManager feature gate noe enabled
// cpuManagerLabel, err := strconv.ParseBool(node.Labels[kubevirtv1.CPUManager])
// if err != nil {
// 	logrus.WithField("node_name", node.Name).WithError(err).Error("CPUManager label not found")
// 	return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
// }
// // the cpu manager policy is the same
// if ((cpuManagerStatus.Policy == CPUManagerStaticPolicy) && cpuManagerLabel) || ((cpuManagerStatus.Policy == CPUManagerNonePolicy) && !cpuManagerLabel) {
// 	logrus.WithField("node_name", node.Name).Error("Same cpu manager policy")
// 	return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
// }

// // if there is any vm that enable cpu pinning and we want to disable cpu manager
// vmis, err := virtualmachineinstance.ListByNode(node, labels.NewSelector(), h.vmiCache)
// if err != nil {
// 	logrus.WithField("node_name", node.Name).WithError(err).Error("Failed to list virtual machine instances")
// 	return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
// }
// if isVMEnableCPUPinning(vmis) && cpuManagerStatus.Policy == CPUManagerNonePolicy {
// 	logrus.WithField("node_name", node.Name).Error("Skip update since there shouldn't have any unstopped vm with cpu pinning when disable cpu manager")
// 	return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
// }

// // if this node is master and there are other master still under update policy progress
// // only allow one master node update policy, since we will restart kubelet
// nodes, err := h.nodeCache.List(labels.Everything())
//
//	if err != nil {
//		logrus.WithField("node_name", node.Name).WithError(err).Error("Failed to list nodes")
//		return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
//	}
//
//	if isMasterNodeUpdatingPolicy(nodes) {
//		logrus.WithField("node_name", node.Name).Error("There is other master nodes updating cpu manager policy")
//		return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
//	}
func (h *cpuManagerNodeHandler) OnNodeChanged(_ string, node *corev1.Node) (*corev1.Node, error) {
	if node == nil || node.DeletionTimestamp != nil {
		return node, nil
	}

	annot, ok := node.Annotations[util.AnnotationCPUManagerUpdateStatus]
	if !ok {
		return node, nil
	}

	cpuManagerStatus, err := GetCPUManagerUpdateStatus(annot)
	if err != nil {
		logrus.WithField("node_name", node.Name).WithError(err).Error("Skip update cpu manager policy, failed to retreive cpu-manager-update-status from annotation")
		return node, nil
	}
	// do nothing if status is not requested
	if cpuManagerStatus.Status != CPUManagerRequestedStatus {
		return node, nil
	}

	job, err := h.submitJob(cpuManagerStatus, node)
	if err != nil {
		logrus.WithField("node_name", node.Name).WithError(err).Error("Submit cpu manager job failed")
		return h.nodeClient.Update(updateNode(node, toFailedStatus(cpuManagerStatus)))
	}

	return h.nodeClient.Update(updateNode(node, toRunningStatus(cpuManagerStatus, job)))
}

func (h *cpuManagerNodeHandler) OnJobChanged(_ string, job *batchv1.Job) (*batchv1.Job, error) {
	if job == nil || job.DeletionTimestamp != nil {
		return job, nil
	}

	nodeName, ok := job.Labels[util.LabelCPUManagerUpdateNode]
	if !ok {
		return job, nil
	}

	node, err := h.nodeCache.Get(nodeName)
	if err != nil {
		return job, err
	}

	if condition.Cond(batchv1.JobComplete).IsTrue(job) {
		updateStatus := &CPUManagerUpdateStatus{
			Status:  CPUManagerSuccessStatus,
			Policy:  CPUManagerPolicy(job.Labels[util.LabelCPUManagerUpdatePolicy]),
			JobName: job.Name,
		}
		if _, err := h.nodeClient.Update(updateNode(node, updateStatus)); err != nil {
			return nil, err
		}
	}

	if condition.Cond(batchv1.JobFailed).IsTrue(job) {
		updateStatus := &CPUManagerUpdateStatus{
			Status:  CPUManagerFailedStatus,
			Policy:  CPUManagerPolicy(job.Labels[util.LabelCPUManagerUpdatePolicy]),
			JobName: job.Name,
		}
		if _, err := h.nodeClient.Update(updateNode(node, updateStatus)); err != nil {
			return nil, err
		}
	}

	return job, nil
}

func GetCPUManagerUpdateStatus(jsonString string) (*CPUManagerUpdateStatus, error) {
	cpuManagerStatus := &CPUManagerUpdateStatus{}
	if err := json.Unmarshal([]byte(jsonString), cpuManagerStatus); err != nil {
		return nil, err
	}
	if cpuManagerStatus.Policy == "" {
		return nil, errors.New("invalid policy")
	}
	if cpuManagerStatus.Status == "" {
		return nil, errors.New("invalid status")
	}
	return cpuManagerStatus, nil
}

func copy(updateStatus *CPUManagerUpdateStatus) *CPUManagerUpdateStatus {
	newUpdateStatus := &CPUManagerUpdateStatus{}
	newUpdateStatus.Status = updateStatus.Status
	newUpdateStatus.Policy = updateStatus.Policy
	newUpdateStatus.JobName = updateStatus.JobName
	return newUpdateStatus
}

func toFailedStatus(updateStatus *CPUManagerUpdateStatus) *CPUManagerUpdateStatus {
	newUpdateStatus := copy(updateStatus)
	newUpdateStatus.Status = CPUManagerFailedStatus
	return newUpdateStatus
}

func toRunningStatus(updateStatus *CPUManagerUpdateStatus, job *batchv1.Job) *CPUManagerUpdateStatus {
	newUpdateStatus := copy(updateStatus)
	newUpdateStatus.Status = CPUManagerFailedStatus
	newUpdateStatus.JobName = job.Name
	return newUpdateStatus
}

func updateNode(node *corev1.Node, updateStatus *CPUManagerUpdateStatus) *corev1.Node {
	jsonStr, _ := json.Marshal(updateStatus)
	toUpdate := node.DeepCopy()
	toUpdate.Annotations[util.AnnotationCPUManagerUpdateStatus] = string(jsonStr)
	return toUpdate
}

// TODO: should I rollback if anything goes wrong ?
func getScript(nodeName string, policy CPUManagerPolicy) string {
	var label string
	if policy == CPUManagerStaticPolicy {
		label = "true"
	} else {
		label = "false"
	}
	return fmt.Sprintf(`
set -e

echo "Start update cpu-manager-policy option..."
HOST_DIR="%s"
KUBECTL="$HOST_DIR/$(readlink $HOST_DIR/var/lib/rancher/rke2/bin)/kubectl"
KUBELET_CONFIG_FILE="$HOST_DIR/etc/rancher/rke2/config.yaml.d/99-z01-harvester-cpu-manager.yaml"
CPU_MANAGER_STATE_FILE="$HOST_DIR/var/lib/kubelet/cpu_manager_state"
NODE_NAME="%s"
NODE_POLICY="%s"
EXIT_CODE=0

if ! $KUBECTL get node "$NODE_NAME" --show-labels | grep -q "cpumanager="; then
	echo "Error: There is no label cpumanager in node $NODE_NAME."
	exit 1
fi

if ! [ -f "$KUBELET_CONFIG_FILE" ]; then
	echo "Error: $KUBELET_CONFIG_FILE does not exist."
	exit 1
fi

CURRENT_POLICY=$(grep -oP '(?<=cpu-manager-policy=)\w+' "$KUBELET_CONFIG_FILE")

if [ "$CURRENT_POLICY" != "%s" ] && [ "$CURRENT_POLICY" != "%s"; then
	echo "Error: invalid cpu-manager-policy in $KUBELET_CONFIG_FILE"
	exit 1
fi

sed -i "s/cpu-manager-policy=$CURRENT_POLICY/cpu-manager-policy=$NODE_POLICY/" "$KUBELET_CONFIG_FILE"
echo "Updated CPU manager policy for $NODE_NAME to $NODE_POLICY in $KUBELET_CONFIG_FILE."

if [ -f "$CPU_MANAGER_STATE_FILE" ]; then
	mv "$CPU_MANAGER_STATE_FILE" "${CPU_MANAGER_STATE_FILE}.old"
	echo "File $CPU_MANAGER_STATE_FILE has been renamed to ${CPU_MANAGER_STATE_FILE}.old"
else
	echo "File $CPU_MANAGER_STATE_FILE does not exist."
fi

if chroot $HOST_DIR systemctl is-active --quiet rke2-server; then
	echo "Restarting rke2-server."
	if ! chroot $HOST_DIR systemctl restart rke2-server; then
		echo "Error: failed to restart rke2-server."
		exit 1
	fi
	echo "Restarted rke2-server."
elif chroot $HOST_DIR systemctl is-active --quiet rke2-agent; then
	echo "Restarting rke2-agent."
	if ! chroot $HOST_DIR systemctl restart rke2-agent; then
		echo "Error: failed to restart rke2-agent."
		exit 1
	fi
	echo "Restarted rke2-agent."
else
	echo "Error: Neither rke2-server nor rke2-agent are running. No services restarted."
	exit 1
fi

TIMEOUT=%d
INTERVAL=5
ELAPSED=0
LABEL_VALUE=%s
LABELS=""
while [ $ELAPSED -lt $TIMEOUT ]; do
	if ! LABELS=$($KUBECTL get node "$NODE_NAME" --show-labels); then
		echo "Error: failed to get labels in $NODE_NAME"
		exit 1
	fi
	if grep -q "cpumanager=$LABEL_VALUE" <<< $LABELS; then
		echo "End update cpu-manager-policy"
		exit 0
	fi
	echo "Value in label cpumanager is not $LABEL_VALUE, wait ${INTERVAL}s..."
	sleep $INTERVAL
	ELAPSED=$((ELAPSED + INTERVAL))
done
echo "Error: timeout, elapsed ${ELAPSED}s"
exit 1
`, HostDir, nodeName, policy, CPUManagerNonePolicy, CPUManagerStaticPolicy, ScriptWaitLabelTimeoutInSec, label)
}

func (h *cpuManagerNodeHandler) getJob(policy CPUManagerPolicy, node *corev1.Node, image string) *batchv1.Job {
	hostPathDirectory := corev1.HostPathDirectory
	labels := map[string]string{
		util.LabelCPUManagerUpdateNode:   node.Name,
		util.LabelCPUManagerUpdatePolicy: string(policy),
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: name.SafeConcatName(node.Name, "update-cpu-manager-"),
			Namespace:    h.namespace,
			Labels:       labels,
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
			BackoffLimit:            ptr.To(int32(0)), // do not retry
			TTLSecondsAfterFinished: ptr.To(JobTTLInSec),
			ActiveDeadlineSeconds:   ptr.To(JobTimeoutInSec),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					HostPID:       true,
					Containers: []corev1.Container{
						{
							Name:    "update-cpu-manager",
							Image:   image,
							Command: []string{"bash", "-c", getScript(node.Name, policy)},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "host-root", MountPath: HostDir},
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
						Name: "host-root",
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

func (h *cpuManagerNodeHandler) submitJob(updateStatus *CPUManagerUpdateStatus, node *corev1.Node) (*batchv1.Job, error) {
	image, err := catalog.FetchAppChartImage(h.appCache, h.namespace, releaseAppHarvesterName, []string{"generalJob", "image"})
	if err != nil {
		return nil, fmt.Errorf("failed to get harvester image (%s): %v", image.ImageName(), err)
	}

	job, err := h.jobClient.Create(h.getJob(updateStatus.Policy, node, image.ImageName()))

	if err != nil {
		return nil, err
	}

	return job, nil
}
