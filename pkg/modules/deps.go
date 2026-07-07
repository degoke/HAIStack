package modules

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// DependencyResolver validates dependency presence and version compatibility
// before install. v1 policy: exact module name match and semver-compatible
// minimum version; fail on ambiguity or cycles.
type DependencyResolver struct {
	modules store.ModuleStore
}

// NewDependencyResolver creates a resolver backed by the installed module store.
func NewDependencyResolver(modules store.ModuleStore) *DependencyResolver {
	return &DependencyResolver{modules: modules}
}

// Resolve checks that the module's dependencies are satisfied by installed
// modules and that the combined dependency graph is acyclic.
func (r *DependencyResolver) Resolve(ctx context.Context, mod *Module) error {
	installed, err := r.modules.List(ctx)
	if err != nil {
		return fmt.Errorf("list installed modules: %w", err)
	}

	// Build a lookup of installed module manifests by name and check for
	// ambiguous duplicates. Module names are unique in the store, so this is a
	// structural safety check in case that invariant is ever violated.
	byName := make(map[string]store.ModuleRecord)
	for _, rec := range installed {
		if existing, ok := byName[rec.Name]; ok {
			if existing.Version != rec.Version {
				return fmt.Errorf("%w: module %q has conflicting versions %q and %q", ErrAmbiguousModule, rec.Name, existing.Version, rec.Version)
			}
			return fmt.Errorf("%w: module %q registered multiple times", ErrAmbiguousModule, rec.Name)
		}
		byName[rec.Name] = rec
	}

	// Direct dependency checks. Self-references are treated as cycles before any
	// missing-module check.
	for _, dep := range mod.Manifest.Dependencies {
		if dep.Name == mod.Manifest.Name {
			return fmt.Errorf("%w: %q depends on itself", ErrCircularDependency, mod.Manifest.Name)
		}
		rec, ok := byName[dep.Name]
		if !ok {
			return fmt.Errorf("%w: %q requires %q %s", ErrMissingDependency, mod.Manifest.Name, dep.Name, dep.Version)
		}
		ok, err := isCompatibleMinimum(rec.Version, dep.Version)
		if err != nil {
			return fmt.Errorf("%w: compare %q %q vs %q: %v", ErrDependencyVersionMismatch, dep.Name, rec.Version, dep.Version, err)
		}
		if !ok {
			return fmt.Errorf("%w: %q requires %q >= %s, installed %s", ErrDependencyVersionMismatch, mod.Manifest.Name, dep.Name, dep.Version, rec.Version)
		}
	}

	// Cycle detection across the new module and all installed modules.
	graph := buildDependencyGraph(mod, installed)
	if cycle := findCycle(graph); cycle != nil {
		return fmt.Errorf("%w: %s", ErrCircularDependency, cycleString(cycle))
	}

	return nil
}

// dependencyGraph maps a module name to the names it depends on.
type dependencyGraph map[string][]string

func buildDependencyGraph(mod *Module, installed []store.ModuleRecord) dependencyGraph {
	graph := make(dependencyGraph)
	graph[mod.Manifest.Name] = dependencyNames(mod.Manifest.Dependencies)
	for _, rec := range installed {
		manifest, _ := manifestFromMetadata(rec.Metadata)
		graph[rec.Name] = dependencyNames(manifest.Dependencies)
	}
	return graph
}

func dependencyNames(deps []DependencyRef) []string {
	out := make([]string, len(deps))
	for i, dep := range deps {
		out[i] = dep.Name
	}
	return out
}

func findCycle(g dependencyGraph) []string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(g))
	var result []string
	var dfs func(node string, path []string) bool
	dfs = func(node string, path []string) bool {
		color[node] = gray
		path = append(path, node)
		for _, dep := range g[node] {
			if color[dep] == gray {
				// Found cycle: include the repeated node at the end to close the
				// loop visibly.
				cycle := append([]string(nil), path...)
				cycle = append(cycle, dep)
				result = cycle
				return true
			}
			if color[dep] == white {
				if dfs(dep, path) {
					return true
				}
			}
		}
		color[node] = black
		return false
	}
	for node := range g {
		if color[node] == white {
			if dfs(node, nil) {
				break
			}
		}
	}
	return result
}

func cycleString(cycle []string) string {
	if len(cycle) == 0 {
		return ""
	}
	// Trim leading nodes before the first occurrence of the repeated node so
	// the cycle is readable.
	last := cycle[len(cycle)-1]
	start := 0
	for i, node := range cycle[:len(cycle)-1] {
		if node == last {
			start = i
			break
		}
	}
	return strings.Join(cycle[start:], " -> ")
}
