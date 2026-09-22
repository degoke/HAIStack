package main

import (
	"embed"
	"fmt"
	"path"
	"strings"
)

//go:embed workloads/*.yaml
var workloadFiles embed.FS

func workloadsFS(name string) ([]byte, error) {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	data, err := workloadFiles.ReadFile(path.Join("workloads", base))
	if err != nil {
		return nil, fmt.Errorf("workload %s: %w", name, err)
	}
	return data, nil
}
