package conf

import (
	"strings"

	z "github.com/Oudwins/zog"
)

type ProjectsConfig struct {
	ParentPaths []string `json:"parentPaths" zog:"parentPaths"`
}

var ProjectsConfigSchema = z.Struct(z.Shape{
	"ParentPaths": z.Slice(z.String()).DefaultFunc(func() any {
		return []string{"~/projects", "~/Documents"}
	}).Transform(normalizeParentPathsTransform),
})

func normalizeParentPathsTransform(data any, c z.Ctx) error {
	parentPaths, ok := data.(*[]string)
	if !ok {
		return nil
	}

	normalized := make([]string, 0, len(*parentPaths))
	for _, parentPath := range *parentPaths {
		parentPath = strings.TrimSpace(parentPath)
		if parentPath == "" {
			continue
		}
		expanded, err := expandPath(parentPath)
		if err != nil {
			return err
		}
		normalized = append(normalized, expanded)
	}

	*parentPaths = normalized
	return nil
}
