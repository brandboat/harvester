package secret

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func Test_validateNodeCPUManagerPolicies(t *testing.T) {
	testCases := []struct {
		name   string
		secret *v1.Secret
		errMsg string
	}{
		{
			name:   "nil secret",
			secret: nil,
			errMsg: "node cpu manager policies is nil",
		},
		{
			name: "nil secret",
			secret: &v1.Secret{
				Data: nil,
			},
			errMsg: "node cpu manager policies is nil",
		},
		{
			name: "nil secret",
			secret: &v1.Secret{
				Data: map[string][]byte{},
			},
			errMsg: "node cpu manager policies is nil",
		},
		{
			name: "incorrect configs",
			secret: &v1.Secret{
				Data: map[string][]byte{
					NodeCPUManagerPolicies: []byte("{\"foo\": \"bar\"}"),
				},
			},
			errMsg: "json parse error: json: unknown field \"foo\"",
		},
		{
			name: "empty configs",
			secret: &v1.Secret{
				Data: map[string][]byte{
					NodeCPUManagerPolicies: []byte("{}"),
				},
			},
			errMsg: "empty configs is prohibited",
		},
		{
			name: "incorrect policy",
			secret: &v1.Secret{
				Data: map[string][]byte{
					NodeCPUManagerPolicies: []byte("{\"configs\": [{\"name\": \"foo\", \"policy\": \"bar\"}]}"),
				},
			},
			errMsg: "cpu manager policy should be either none or static",
		},
		{
			name: "static policy",
			secret: &v1.Secret{
				Data: map[string][]byte{
					NodeCPUManagerPolicies: []byte("{\"configs\": [{\"name\": \"foo\", \"policy\": \"static\"}]}"),
				},
			},
			errMsg: "",
		},
		{
			name: "none policy",
			secret: &v1.Secret{
				Data: map[string][]byte{
					NodeCPUManagerPolicies: []byte("{\"configs\": [{\"name\": \"foo\", \"policy\": \"none\"}]}"),
				},
			},
			errMsg: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateNodeCPUManagerPolicies(testCase.secret)
			if testCase.errMsg != "" {
				assert.Equal(t, testCase.errMsg, err.Error())
			} else {
				assert.Nil(t, err)
			}
		})

	}
}
