# CPU Pinning

## Summary
Support virtual machine CPU pinning to pin guest's vCPUs to the host's pCPUs to provide predictable latency and
enhanced performance.

### Related Issues
https://github.com/harvester/harvester/issues/2305

## Motivation
Enabling CPU pinning could bring the following benefits:
- Performance Isolation: CPU pinning allows you to dedicate CPU cores or threads to particular virtual machines.
This isolation can prevent performance interference between different VMs running on the same physical hardware.
- Predictable Performance: By assigning dedicated CPU resources to VMs, you can achieve more predictable performance levels.
This is particularly important for applications with stringent performance requirements, such as real-time applications
or high-performance computing workloads.
- Reduced Latency: CPU pinning can help reduce latency by ensuring that critical tasks consistently run on the same set
of CPU cores or threads. This can be beneficial for applications where low latency is crucial.

### Goals
- Allowing virtual machines to exclusively use the CPU resources.
- After live migration, the virtual machine retains CPU pinning settings.

### Non-goals
This HEP does not cover:
- Allow user to specify specific CPUs in virtual machine.
- Enable CPU pinning without restarting the VM.

## Proposal
Enabling CPU pinning for KubeVirt requires setting the kubelet argument `--cpu-manager-policy` to [static](https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/#static-policy).
However, this change affects the way CPU resources are utilized by all pods that meet the condition as follows, not just VMs. 
- Both requests and limits must be configured in pods (Guaranteed QoS) and their values must be the same integer.

So we cannot set `--cpu-manager-policy` to static on all nodes initially. Instead, we allow users to start Harvester
and then decide which nodes they want to set `--cpu-manager-policy=static`. Subsequently, when creating VMs,
they can deploy the VM desired CPU pinning to the corresponding nodes with `--cpu-manager-policy=static` enabled.

### User Stories

#### Story 1: Set cpu-manager-policy from none to static
Assuming we have 3 Harvester nodes (node1, node2, node3), each containing CPU sets ranging from 0 to 47, total 48 CPUs,
and there is a VM named `test` with 1 CPU deployed on node1. Initially, the CPU set in test is `0-47` since CPU pinning is not yet enabled.
We can examine VM CPU set by running
```sh
kubectl exec -n default -it virt-launcher-test-9nmwp -- taskset -cp 1
```
the output is
```txt
pid 1's current affinity list: 0-47
```

After setting up `cpu-manager-policy` to `static` on node1 and node2, we create another VM named test2 with 16 CPUs and enable CPU pinning.
The CPU set in `test2` is `1-8,25-32` (16 CPUs). To examine the CPUs are pinned in `test2`, we can run cmd as below. 
```sh
kubectl exec virt-launcher-test2-7wgvk -- virsh dumpxml default_test2 | awk "/<cputune>/,/<\/cputune>/"`
```
the output is
```xml
  <cputune>
    <vcpupin vcpu='0' cpuset='1'/>
    <vcpupin vcpu='1' cpuset='25'/>
    <vcpupin vcpu='2' cpuset='2'/>
    <vcpupin vcpu='3' cpuset='26'/>
    <vcpupin vcpu='4' cpuset='3'/>
    <vcpupin vcpu='5' cpuset='27'/>
    <vcpupin vcpu='6' cpuset='4'/>
    <vcpupin vcpu='7' cpuset='28'/>
    <vcpupin vcpu='8' cpuset='5'/>
    <vcpupin vcpu='9' cpuset='29'/>
    <vcpupin vcpu='10' cpuset='6'/>
    <vcpupin vcpu='11' cpuset='30'/>
    <vcpupin vcpu='12' cpuset='7'/>
    <vcpupin vcpu='13' cpuset='31'/>
    <vcpupin vcpu='14' cpuset='8'/>
    <vcpupin vcpu='15' cpuset='32'/>
  </cputune>
```

Subsequently, when we check VM `test`, we observe that the CPU set in it changes to `0,9-24,33-47`. The CPUs occupied by `test2`
are excluded from the CPU shared pool.

Now, let's examine other pre-existing pod, such as the `harvester-node-manager` pod deployed on node1. The CPU set in it is also `0,9-24,33-47`.

#### Story 2: Set cpu-manager-policy from static to none
Assume that we have 3 nodes (node1, node2, node3), each node contains cpu set `0-47`, and the `cpu-manager-policy` is set to `static` in node1, node2.
Initially, we have a VM named `test` with CPU pinning enabled, utilizing 16 CPUs(`1-8,25-32`), deployed to node1.

Now, let's change the `cpu-manager-policy` to `none` in node1, and observe the CPU set in VM `test`, which remains `1-8,25-32`.
Next, we create another VM named `test2` with 2 CPUs without enabling CPU pinning and deploy it to node1,
the CPU set in VM `test2` is `0-47`. Although CPU pinning in `test` still work, CPUs inside `test` is no longer isolated
and could be shared with other VMs. 

Upon checking other pre-existing pod, such as the `harvester-node-manager` pod which deployed in node1, we find that
the cpu set in it remains `0,9-24,33-47`, despite the `cpu-manager-policy` being set to none in node1.

Now, if we restart VM `test`, it will be deployed to node2 since `cpu-manager-policy` is set to `none` in node1, node3.
Subsequently, if we disable CPU pinning in node2 and restart VM test, it won't deploy successfully since no node has set
`cpu-manager-policy` to `static`.

#### Story 3: users who don't want to deploy workload to nodes that set cpu-manager-policy to static
If users are aware of which nodes apply the static CPU manager policy, they can utilize [node affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity).
Another approach is to use [node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector),
KubeVirt will label `cpumanager=false` to nodes that use cpu manager policy `none` and label nodes with `cpumanager=true`
when the policy is set to `static`.

### API changes
- N/A

## Design

### Implementation Overview
To implement this proposal, there are several steps need to be taken.
1. Enable [CPU Manager Static Policy](https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/#static-policy).
  Currently Harvester use `none` policy which is the default one. And we also need to set kubelet
  `--kube-reserved` or `--system-reserved` options. This step needs to delete cpu_manager_state file and restart rke2-server (or you can say kubelet).
2. Enable KubeVirt `CPUManager` feature gate. Then KubeVirt could add [cputune](https://libvirt.org/formatdomain.html#cpu-tuning)
  part to libvirt domain xml to pin guest's vCPUs in virtual machine to the host's pCPUs.
3. Allow virtual machine to apply [Guaranteed QoS](https://kubernetes.io/docs/concepts/workloads/pods/pod-qos/#guaranteed).
  Currently Harvester force all virtual machines apply overcommitting resources which make all virtual machine pods under
  [Burstable QoS](https://kubernetes.io/docs/concepts/workloads/pods/pod-qos/#burstable). While the CPU Manager static
  policy only reserved cpus when both cpu requests and limits values must be the same integer.

To change cpu-manager policy, utilizing `Plan` CRD here to deploy jobs to all nodes. We need to modify the secret `cattle-system/cpu-manager`, and fill every policy settings in each node
that we want to change in the `.stringdata.configs` field. It should be a json format like the following example.
In the following example, we want to change node-0 to none policy, node-1 to static policy, node-2 to none policy.
```json
{
  "configs": [
    {
      "name": "node-0",
      "policy": "none"
    },
    {
      "name": "node-1",
      "policy": "static"
    },
    {
      "name": "node-2",
      "policy": "none"
    }
  ]
}
```
- name: node name
- policy: cpu manager policy name. should be either `static` or `none`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cpu-manager
  namespace: cattle-system
type: Opaque
stringData:
  configs: '{"configs": [{"name": "node-0", "policy": "none"}, {"name": "node-1", "policy": "static", {"name": "node-2", "policy": "none"}]}'
---
apiVersion: upgrade.cattle.io/v1
kind: Plan
metadata:
  name: check-rke2-server
  namespace: cattle-system
spec:
  concurrency: 1
  nodeSelector:
    matchLabels:
      harvesterhci.io/managed: "true"
  serviceAccountName: system-upgrade-controller
  version: v1.1.0
  secrets:
    - name: cpu-manager
      path: /cpu-manager
  upgrade:
    image: registry.suse.com/bci/bci-base:15.5
    command: ["/bin/sh", "-c"]
    args:
      # see the following description
```
`.spec.upgrade.args` section should do the following things:
1. check if the current node id is in `/cpu-manager/configs`, if yes, go to next step, if not, do nothing and return.
2. check if `--cpu-manager-policy` is not the same as the node policy in /cpu-manager/configs json file, if yes, go to next step, if not, do nothing and return.
3. modify `/etc/rancher/rke2/config.yaml.d/90-harvester-server.yaml` and change the `--cpu-manager-policy`
4. remove `/var/lib/kubelet/cpu_manager_state`
5. restart rke2-server or rke2-agent (where the kubelet belongs to)

Once the above implementations are completed, we can activate VM CPU pinning by including `dedicatedCpuPlacement=true`
in `.spec.template.cpu`. Additionally, ensuring that CPU limits and requests are identical and integers (i.e., Guaranteed QoS).
```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
spec:
  template:
    spec:
      domain:
        cpu:
          cores: 2
          sockets: 1
          threads: 1
          dedicatedCpuPlacement: true
        resources:
          limits:
            cpu: 2
          requests:
            cpu: 2
[...]
```

#### Web UI
Regarding the Web UI, we should display the number of CPUs already pinned on each node,
as well as the remaining number of CPUs available for allocation on each node.

### Test plan
TBD

## Discussion
- How much CPU resource allocation should be reserved? We have to set cpu resources in either `--kube-reserved` or `--system-reserved`
before setting `cpu-manager-policy` to `static`. Apart from the resources required for Harvester's own startup,
we also need to account for the possibility of needing additional resources during upgrades.
- In [Story 2: Set cpu-manager-policy from static to none](#Story-2-Set-cpu-manager-policy-from-static-to-none), after changing cpu manager policy from none to static and then change it back to none.
All existing pods still use the same cpu sets as settings in static policy. Currently, I'm not sure if this will affect the pod performance or not.
