// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	fake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func TestMain(m *testing.M) {
	var simpleClient kubernetes.Interface = fake.NewSimpleClientset()

	// setup test resources

	testns := "testns"
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testns,
		},
	}
	simpleClient.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})

	// secret
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testsecret",
		},
		Data: map[string][]byte{
			"secretkey1": []byte("secretkey1Val"),
			"secretkey2": []byte("secretkey2Val"),
		},
		Type: "Opaque",
	}
	simpleClient.CoreV1().Secrets(testns).Create(context.TODO(), &secret, metav1.CreateOptions{})

	// configmap
	configmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testconfigmap",
		},
		Data: map[string]string{
			"cmkey1": "cmkey1Val",
			"cmkey2": "cmkey2Val",
		},
	}
	simpleClient.CoreV1().ConfigMaps(testns).Create(context.TODO(), &configmap, metav1.CreateOptions{})

	InitializeKubeClient(&simpleClient, nil)

	exitVal := m.Run()
	os.Exit(exitVal)
}

func TestResolveTemplate(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		inputTmpl      string
		expectedResult string
		expectedErr    error
	}{
		{
			`data: '{{ fromSecret "testns" "testsecret" "secretkey1" }}'`,
			"data: c2VjcmV0a2V5MVZhbA==",
			nil,
		},
		{
			`param: '{{ fromConfigMap "testns" "testconfigmap" "cmkey1"  }}'`,
			"param: cmkey1Val",
			nil,
		},
		{
			`config1: '{{ "testdata" | base64enc  }}'`,
			"config1: dGVzdGRhdGE=",
			nil,
		},
		{
			`config2: '{{ "dGVzdGRhdGE=" | base64dec  }}'`,
			"config2: testdata",
			nil,
		},
		{
			`test: '{{ blah "asdf"  }}'`,
			"",
			errors.New(`failed to parse the template map map[test:{{ blah "asdf"  }}]: template: tmpl:1: function "blah" not defined`),
		},
	}

	for _, test := range testcases {
		// unmarshall to Interface
		tmplMap, _ := fromYAML(test.inputTmpl)
		val, err := ResolveTemplate(tmplMap)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
			}
			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else {
			val, _ := toYAML(val)
			if val != test.expectedResult {
				t.Fatalf("expected : %s , got : %s", test.expectedResult, val)
			}
		}
	}
}

func TestHasTemplate(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		input  string
		result bool
	}{
		{" I am a sample template ", false},
		{" I am a {{ sample }}  template ", true},
	}

	for _, test := range testcases {
		val := HasTemplate(test.input)
		if val != test.result {
			t.Fatalf("expected : %v , got : %v", test.result, val)
		}
	}
}

func TestAtoi(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		input  string
		result int
	}{
		{"123", 123},
	}

	for _, test := range testcases {
		val := atoi(test.input)
		if val != test.result {
			t.Fatalf("expected : %v , got : %v", test.result, val)
		}
	}
}

func TestToBool(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		input  string
		result bool
	}{
		{"1", true},
		{"blah", false},
		{"TRUE", true},
		{"F", false},
		{"true", true},
		{"false", false},
	}

	for _, test := range testcases {
		val := toBool(test.input)
		if val != test.result {
			t.Fatalf("expected : %v , got : %v", test.result, val)
		}
	}
}

func TestProcessForDataTypes(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		input          string
		expectedResult string
	}{
		{`key : "{{ "1" | toBool }}"`, `key : {{ "1" | toBool }}`},
		{
			`key : |
			"{{ "6" | toInt }}"`,
			`key : {{ "6" | toInt }}`,
		},
		{
			`key1 : "{{ "1" | toInt }}"
		  key2 : |-
		 		{{ "test" | toBool | toInt }}`,
			`key1 : {{ "1" | toInt }}
		  key2 : {{ "test" | toBool | toInt }}`,
		},
	}

	for _, test := range testcases {
		val := processForDataTypes(test.input)
		if val != test.expectedResult {
			t.Fatalf("expected : %v , got : %v", test.expectedResult, val)
		}
	}
}

func ExampleResolveTemplate() {
	policyYAML := `
---
apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: demo-sampleapp-config
  namespace: sampleapp
spec:
  namespaceSelector:
    exclude:
    - kube-*
    include:
    - default
  object-templates:
  - complianceType: musthave
    objectDefinition:
      kind: ConfigMap
      apiVersion: v1
      metadata:
        name: demo-sampleapp-config
        namespace: test
      data:
        message: '{{ "VGVtcGxhdGVzIHJvY2shCg==" | base64dec }}'
    remediationAction: enforce
    severity: high
`
	policyMap := map[string]interface{}{}

	if err := yaml.Unmarshal([]byte(policyYAML), &policyMap); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unmarshal the policy YAML: %v\n", err)
		panic(err)
	}

	policyMapProcessed, err := ResolveTemplate(policyMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to process the policy YAML: %v\n", err)
		panic(err)
	}

	objTmpls := policyMapProcessed.(map[string]interface{})["spec"].(map[string]interface{})["object-templates"]
	objDef := objTmpls.([]interface{})[0].(map[string]interface{})["objectDefinition"]
	message, ok := objDef.(map[string]interface{})["data"].(map[string]interface{})["message"].(string)
	if !ok {
		fmt.Fprintf(os.Stderr, "Failed to process the policy YAML: %v\n", err)
		panic(err)
	}

	fmt.Println(message)

	// Output:
	// Templates rock!
}
