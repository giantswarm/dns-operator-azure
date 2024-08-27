package infracluster

import "strings"

const (
	ResourceTagNamePrefix = "azure-resource-tag/"
)

func GetResourceTagsFromInfraClusterAnnotations(annotations map[string]string) map[string]*string {
	if annotations == nil {
		return nil
	}
	if len(annotations) == 0 {
		return nil
	}
	tags := make(map[string]*string)
	for key, value := range annotations {
		if strings.HasPrefix(key, ResourceTagNamePrefix) {
			tagKey := strings.TrimPrefix(key, ResourceTagNamePrefix)
			tagValue := value
			tags[tagKey] = &tagValue
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}
