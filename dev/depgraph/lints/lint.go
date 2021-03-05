package lints

import (
	"fmt"

	"github.com/sourcegraph/sourcegraph/dev/depgraph/graph"
)

type Lint func(graph *graph.DependencyGraph) error

var lintsByName = map[string]Lint{
	"NoDeadPackages":             NoDeadPackages,
	"NoReachingIntoCommands":     NoReachingIntoCommands,
	"NoBinarySpecificSharedCode": NoBinarySpecificSharedCode,
}

var DefaultLints []string

func init() {
	for name := range lintsByName {
		DefaultLints = append(DefaultLints, name)
	}
}

func Run(graph *graph.DependencyGraph, names []string) error {
	lints := make([]Lint, 0, len(names))
	for _, name := range names {
		lint, ok := lintsByName[name]
		if !ok {
			return fmt.Errorf("unknown lint '%s'", name)
		}

		lints = append(lints, lint)
	}

	var errors []lintError
	for _, lint := range lints {
		if err := lint(graph); err != nil {
			lError, ok := err.(lintErrors)
			if !ok {
				return err
			}

			errors = append(errors, lError...)
		}
	}

	return multi(errors)
}
