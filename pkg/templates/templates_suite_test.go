package templates

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const testNs = "testns"

var (
	k8sConfig       *rest.Config
	testEnv         *envtest.Environment
	ctx             context.Context
	cancel          context.CancelFunc
	clusterClaimGVR = schema.GroupVersionResource{
		Group:    "cluster.open-cluster-management.io",
		Version:  "v1alpha1",
		Resource: "clusterclaims",
	}
)

func TestMain(m *testing.M) {
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {
	defer tearDown()

	setUp()

	return m.Run()
}

func setUp() {
	ctx, cancel = context.WithCancel(context.TODO())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{"../../test_data/crds/clusterclaim.yaml"},
	}

	var err error

	k8sConfig, err = testEnv.Start()
	if err != nil {
		panic(err.Error())
	}

	k8sClient, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		panic(err.Error())
	}

	// SetUp test resources

	// test namespace
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNs,
		},
	}

	_, err = k8sClient.CoreV1().Namespaces().Create(ctx, &ns, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	// sample secret
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testsecret",
		},
		StringData: map[string]string{
			"secretkey1": "secretkey1Val",
			"secretkey2": "secretkey2Val",
		},
		Type: "Opaque",
	}

	_, err = k8sClient.CoreV1().Secrets(testNs).Create(ctx, &secret, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	// sample configmap
	configmap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testconfigmap",
		},
		Data: map[string]string{
			"cmkey1":         "cmkey1Val",
			"cmkey2":         "cmkey2Val",
			"ingressSources": "[10.10.10.10, 1.1.1.1]",
		},
	}

	// sample configmaps to test different label selectors
	configmaplba := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testcm-enva",
			Labels: map[string]string{
				"app": "test",
				"env": "a",
			},
		},
		Data: map[string]string{
			"cmkey1": "cmkey1Val",
		},
	}

	// sample configmap env b
	configmaplbb := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testcm-envb",
			Labels: map[string]string{
				"app": "test",
				"env": "b",
			},
		},
		Data: map[string]string{
			"cmkey1": "cmkey1Val",
		},
	}

	// sample configmap env c
	configmaplbc := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testcm-envc",
			Labels: map[string]string{
				"app": "test",
				"env": "c",
			},
		},
		Data: map[string]string{
			"cmkey1": "cmkey1Val",
		},
	}

	_, err = k8sClient.CoreV1().ConfigMaps(testNs).Create(ctx, &configmap, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	_, err = k8sClient.CoreV1().ConfigMaps(testNs).Create(ctx, &configmaplba, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	_, err = k8sClient.CoreV1().ConfigMaps(testNs).Create(ctx, &configmaplbb, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	_, err = k8sClient.CoreV1().ConfigMaps(testNs).Create(ctx, &configmaplbc, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	k8sDynClient, err := dynamic.NewForConfig(k8sConfig)
	if err != nil {
		panic(err.Error())
	}

	clusterClaim := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cluster.open-cluster-management.io/v1alpha1",
			"kind":       "ClusterClaim",
			"metadata": map[string]interface{}{
				"name": "env",
			},
			"spec": map[string]interface{}{
				"value": "dev",
			},
		},
	}

	_, err = k8sDynClient.Resource(clusterClaimGVR).Create(ctx, &clusterClaim, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}
}

func tearDown() {
	cancel()

	err := testEnv.Stop()
	if err != nil {
		panic(err.Error())
	}
}
