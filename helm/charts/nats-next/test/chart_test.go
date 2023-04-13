package test

import (
	"github.com/ghodss/yaml"
	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/nats-io/nats-server/v2/conf"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type Resources struct {
	Conf                  Resource[map[string]any]
	ConfigMap             Resource[corev1.ConfigMap]
	HeadlessService       Resource[corev1.Service]
	Ingress               Resource[networkingv1.Ingress]
	NatsBoxContentsSecret Resource[corev1.Secret]
	NatsBoxContextSecret  Resource[corev1.Secret]
	NatsBoxDeployment     Resource[appsv1.Deployment]
	Service               Resource[corev1.Service]
	StatefulSet           Resource[appsv1.StatefulSet]
	PodMonitor            Resource[monitoringv1.PodMonitor]
	ExtraResource0        Resource[corev1.ConfigMap]
	ExtraResource1        Resource[corev1.Service]
}

func (r *Resources) Iter() []MutableResource {
	return []MutableResource{
		r.Conf.Mutable(),
		r.ConfigMap.Mutable(),
		r.HeadlessService.Mutable(),
		r.Ingress.Mutable(),
		r.NatsBoxContentsSecret.Mutable(),
		r.NatsBoxContextSecret.Mutable(),
		r.NatsBoxDeployment.Mutable(),
		r.Service.Mutable(),
		r.StatefulSet.Mutable(),
		r.PodMonitor.Mutable(),
		r.ExtraResource0.Mutable(),
		r.ExtraResource1.Mutable(),
	}
}

type Resource[T any] struct {
	ID       string
	HasValue bool
	Value    T
}

func (r *Resource[T]) Mutable() MutableResource {
	return MutableResource{
		ID:        r.ID,
		HasValueP: &r.HasValue,
		ValueP:    &r.Value,
	}
}

type MutableResource struct {
	ID        string
	HasValueP *bool
	ValueP    any
}

type K8sResource struct {
	Kind     string      `yaml:"kind"`
	Metadata K8sMetadata `yaml:"metadata"`
}

type K8sMetadata struct {
	Name string `yaml:"name"`
}

func GenerateResources(fullName string) *Resources {
	return &Resources{
		Conf: Resource[map[string]any]{
			ID: "nats.conf",
		},
		ConfigMap: Resource[corev1.ConfigMap]{
			ID: "ConfigMap/" + fullName + "-config",
		},
		HeadlessService: Resource[corev1.Service]{
			ID: "Service/" + fullName + "-headless",
		},
		Ingress: Resource[networkingv1.Ingress]{
			ID: "Ingress/" + fullName + "-ws",
		},
		NatsBoxContentsSecret: Resource[corev1.Secret]{
			ID: "Secret/" + fullName + "-box-contents",
		},
		NatsBoxContextSecret: Resource[corev1.Secret]{
			ID: "Secret/" + fullName + "-box-context",
		},
		NatsBoxDeployment: Resource[appsv1.Deployment]{
			ID: "Deployment/" + fullName + "-box",
		},
		Service: Resource[corev1.Service]{
			ID: "Service/" + fullName,
		},
		StatefulSet: Resource[appsv1.StatefulSet]{
			ID: "StatefulSet/" + fullName,
		},
		PodMonitor: Resource[monitoringv1.PodMonitor]{
			ID: "PodMonitor/" + fullName,
		},
		ExtraResource0: Resource[corev1.ConfigMap]{
			ID: "ConfigMap/" + fullName + "-extra",
		},
		ExtraResource1: Resource[corev1.Service]{
			ID: "Service/" + fullName + "-extra",
		},
	}
}

type Test struct {
	ChartName   string
	ReleaseName string
	Namespace   string
	FullName    string
	Values      string
}

func DefaultTest() *Test {
	return &Test{
		ChartName:   "nats",
		ReleaseName: "nats",
		Namespace:   "nats",
		FullName:    "nats",
		Values:      "{}",
	}
}

func HelmRender(t *testing.T, test *Test) *Resources {
	t.Helper()

	helmChartPath, err := filepath.Abs("..")
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "values.*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(test.Values)); err != nil {
		tmpFile.Close()
		require.NoError(t, err)
	}
	err = tmpFile.Close()
	require.NoError(t, err)

	options := &helm.Options{
		ValuesFiles:    []string{tmpFile.Name()},
		KubectlOptions: k8s.NewKubectlOptions("", "", test.Namespace),
	}
	output := helm.RenderTemplate(t, options, helmChartPath, test.ReleaseName, nil)
	outputs := strings.Split(output, "---")

	resources := GenerateResources("nats")
	for _, o := range outputs {
		meta := K8sResource{}
		err := yaml.Unmarshal([]byte(o), &meta)
		require.NoError(t, err)

		id := meta.Kind + "/" + meta.Metadata.Name
		for _, r := range resources.Iter() {
			if id == r.ID {
				helm.UnmarshalK8SYaml(t, o, r.ValueP)
				*r.HasValueP = true
				break
			}
		}
	}

	require.True(t, resources.ConfigMap.HasValue)
	_, ok := resources.ConfigMap.Value.Data["nats.conf"]
	require.True(t, ok)

	confDir, err := os.MkdirTemp("", "")
	require.NoError(t, err)
	defer os.RemoveAll(confDir)

	for k, v := range resources.ConfigMap.Value.Data {
		err := os.WriteFile(filepath.Join(confDir, k), []byte(v), 0644)
		require.NoError(t, err)
	}

	_ = os.Setenv("HOSTNAME", "nats-0")
	resources.Conf.Value, err = conf.ParseFile(filepath.Join(confDir, "nats.conf"))
	resources.Conf.HasValue = true

	return resources
}

func RenderAndCheck(t *testing.T, test *Test, expected *Resources) {
	t.Helper()
	actual := HelmRender(t, test)
	a := assert.New(t)

	if actual.ConfigMap.Value.Data != nil {
		natsConf, ok := actual.ConfigMap.Value.Data["nats.conf"]
		if ok {
			if expected.ConfigMap.Value.Data == nil {
				expected.ConfigMap.Value.Data = map[string]string{}
			}
			expected.ConfigMap.Value.Data["nats.conf"] = natsConf
		}
	}

	if actual.StatefulSet.Value.Spec.Template.Annotations != nil {
		configMapHash, ok := actual.StatefulSet.Value.Spec.Template.Annotations["checksum/config"]
		if ok {
			if expected.StatefulSet.Value.Spec.Template.Annotations == nil {
				expected.StatefulSet.Value.Spec.Template.Annotations = map[string]string{}
			}
			expected.StatefulSet.Value.Spec.Template.Annotations["checksum/config"] = configMapHash
		}
	}

	expectedResources := expected.Iter()
	actualResources := actual.Iter()
	require.Len(t, actualResources, len(expectedResources))

	for i, _ := range expectedResources {
		expectedResource := expectedResources[i]
		actualResource := actualResources[i]
		if a.Equal(expectedResource.HasValueP, actualResource.HasValueP) && *actualResource.HasValueP {
			a.Equal(expectedResource.ValueP, actualResource.ValueP)
		}
	}
}
