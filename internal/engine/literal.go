package engine

import (
	"fmt"
	"strings"

	"github.com/gi-runner/gi/internal/value"
	"gopkg.in/yaml.v3"
)

func (e *exec) yamlStep(rest, doc string) error {
	name, raw, err := e.literal(rest, doc, "yaml")
	if err != nil {
		return err
	}
	var v any
	if err := yaml.Unmarshal([]byte(raw), &v); err != nil {
		return fmt.Errorf("yaml: %w", err)
	}
	return e.assign(name, value.Normalize(v))
}

func (e *exec) csvStep(rest, doc string) error {
	name, raw, err := e.literal(rest, doc, "csv")
	if err != nil {
		return err
	}
	rows, err := value.ParseCSV(raw)
	if err != nil {
		return fmt.Errorf("csv: %w", err)
	}
	return e.assign(name, rows)
}

func (e *exec) literal(rest, doc, kind string) (string, string, error) {
	name, rhs, ok := splitAssign(rest)
	if !ok {
		return "", "", fmt.Errorf("%s expects name = value", kind)
	}
	if strings.TrimSpace(rhs) == "" {
		if strings.TrimSpace(doc) == "" {
			return "", "", fmt.Errorf("%s expects a value", kind)
		}
		return name, doc, nil
	}
	v, err := e.eval(rhs)
	if err != nil {
		return "", "", err
	}
	return name, value.Sprint(v), nil
}
