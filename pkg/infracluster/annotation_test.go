package infracluster

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
)

func Test_GetResourceTagsFromInfraClusterAnnotation(t *testing.T) {
	testValue := "test-value"

	testCases := []struct {
		name         string
		infraCluster runtime.Object
		expectedTags map[string]*string
	}{
		{
			name: "case0: Get resource tag from infra cluster annotations",
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"azure-resourcegroup-tag.test-key": testValue,
					},
				},
			},
			expectedTags: map[string]*string{
				"test-key": &testValue,
			},
		},
		{
			name: "case1: Get resource tag from infra cluster annotations with multiple tags",
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"azure-resourcegroup-tag.test-key":  testValue,
						"azure-resourcegroup-tag.test-key1": testValue,
					},
				},
			},
			expectedTags: map[string]*string{
				"test-key":  &testValue,
				"test-key1": &testValue,
			},
		},
		{
			name: "case2: Get resource tag from infra cluster annotations with no tags",
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expectedTags: nil,
		},
		{
			name: "case3: Get resource tag from infra cluster annotations with no annotations",
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expectedTags: nil,
		},
		{
			name: "case4: Get resource tag from infra cluster annotations with unprefixed tags",
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"test-key": testValue,
					},
				},
			},
			expectedTags: nil,
		},
		{
			name: "case5: Get resource tag from infra cluster annotations with some unprefixed tags",
			infraCluster: &infrav1.AzureCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"azure-resourcegroup-tag.test-key": testValue,
						"test-key":                         testValue,
					},
				},
			},
			expectedTags: map[string]*string{
				"test-key": &testValue,
			},
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			infraClusterObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tc.infraCluster)
			if err != nil {
				t.Fatal(err)
			}
			scope := Scope{
				InfraCluster: &unstructured.Unstructured{Object: infraClusterObj},
			}
			tags := GetResourceTagsFromInfraClusterAnnotations(scope.InfraClusterAnnotations())
			if len(tags) != len(tc.expectedTags) {
				t.Fatalf("expected %d tags, got %d", len(tc.expectedTags), len(tags))
			}

			for key, value := range tc.expectedTags {
				if tags[key] == nil {
					t.Fatalf("expected tag %s not found", key)
				}
				if *tags[key] != *value {
					t.Fatalf("expected tag %s value %s, got %s", key, *value, *tags[key])
				}
			}
		})
	}
}
