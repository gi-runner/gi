package engine

import "github.com/gi-runner/gi/internal/feature"

// Selected applies tags, env, and @noload before Examples evaluate or scenarios run.
// Excluded outlines must not execute setup or other expressions.
func Selected(sc feature.Scenario, opt Options) (bool, error) {
	if sc.IsSetup || feature.HasTag(sc.Tags, "ignore") ||
		(opt.LoadMode && feature.HasTag(sc.Tags, "noload")) ||
		!feature.AllowsEnv(sc.Tags, opt.Env) {
		return false, nil
	}
	return feature.Selected(sc.Tags, opt.Tag)
}
