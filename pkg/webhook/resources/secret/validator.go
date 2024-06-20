package secret

import (
	"bytes"
	"encoding/json"
	"fmt"

	admissionregv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	werror "github.com/harvester/harvester/pkg/webhook/error"
	"github.com/harvester/harvester/pkg/webhook/types"
)

const (
	NodeCPUManagerPolicies = "node-cpu-manager-policies"
)

func NewValidator() types.Validator {
	return &secretValidator{}
}

type secretValidator struct {
	types.DefaultValidator
}

func (v *secretValidator) Resource() types.Resource {
	return types.Resource{
		Names:      []string{v1beta1.SettingResourceName},
		Scope:      admissionregv1.ClusterScope,
		APIGroup:   v1beta1.SchemeGroupVersion.Group,
		APIVersion: v1beta1.SchemeGroupVersion.Version,
		ObjectType: &v1beta1.Setting{},
		OperationTypes: []admissionregv1.OperationType{
			admissionregv1.Create,
			admissionregv1.Update,
		},
	}
}

type nodeCPUManagerPolicies struct {
	Configs []nodeCPUManagerPolicy `json:"configs"`
}

type nodeCPUManagerPolicy struct {
	Name   string `json:"name"`
	Policy string `json:"policy"`
}

func (v *secretValidator) Create(_ *types.Request, newObj runtime.Object) error {
	secret := newObj.(*v1.Secret)
	if secret.Name == NodeCPUManagerPolicies {
		validateNodeCPUManagerPolicies(secret)
	}
	return nil
}

func (v *secretValidator) Update(_ *types.Request, _ runtime.Object, newObj runtime.Object) error {
	secret := newObj.(*v1.Secret)
	if secret.Name == NodeCPUManagerPolicies {
		validateNodeCPUManagerPolicies(secret)
	}
	return nil
}

func validateNodeCPUManagerPolicies(secret *v1.Secret) error {
	if secret == nil || secret.Data == nil || secret.Data[NodeCPUManagerPolicies] == nil {
		return werror.NewInvalidError("node cpu manager policies is nil", "")
	}

	decoder := json.NewDecoder(bytes.NewReader(secret.Data[NodeCPUManagerPolicies]))
	decoder.DisallowUnknownFields()

	var policies nodeCPUManagerPolicies
	if err := decoder.Decode(&policies); err != nil {
		return werror.NewInvalidError(fmt.Sprintf("json parse error: %s", err.Error()), NodeCPUManagerPolicies)
	}
	if len(policies.Configs) == 0 {
		return werror.NewInvalidError("empty configs is prohibited", "")
	}
	for _, config := range policies.Configs {
		if config.Policy != "static" && config.Policy != "none" {
			return werror.NewInvalidError("cpu manager policy should be either none or static", "")
		}
	}
	return nil
}
