package engine

import "github.com/gi-runner/gi/internal/feature"

// Apply selection before evaluating dynamic Examples as well as before running
// scenarios. Excluded outlines must not execute setup or other expressions.
func selected(sc feature.Scenario, opt Options) (bool, error) {
	if sc.IsSetup || feature.HasTag(sc.Tags, "ignore") ||
		(opt.LoadMode && feature.HasTag(sc.Tags, "noload")) ||
		!feature.AllowsEnv(sc.Tags, opt.Env) {
		return false, nil
	}
	return feature.Selected(sc.Tags, opt.Tag)
}
