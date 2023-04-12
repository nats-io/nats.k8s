package test

import (
	"github.com/ghodss/yaml"
	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/nats-io/nats-server/v2/conf"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
			ID: "Ingress/" + fullName,
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
	Name        string
	ReleaseName string
	Namespace   string
	FullName    string
	Values      string
}

func HelmRender(t *testing.T, test *Test) *Resources {
	t.Helper()

	helmChartPath, err := filepath.Abs("..")
	releaseName := "nats"
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
		KubectlOptions: k8s.NewKubectlOptions("", "", "nats"),
	}
	output := helm.RenderTemplate(t, options, helmChartPath, releaseName, nil)
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
