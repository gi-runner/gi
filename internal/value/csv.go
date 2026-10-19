package value

import (
	"encoding/csv"
	"strings"
)

func ParseCSV(raw string) ([]any, error) {
	raw = strings.TrimPrefix(raw, "\uFEFF")
	r := csv.NewReader(strings.NewReader(raw))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []any{}, nil
	}
	header := rows[0]
	out := make([]any, 0, len(rows)-1)
	for _, row := range rows[1:] {
		obj := map[string]any{}
		for i, h := range header {
			if h == "" {
				continue
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			obj[h] = cell
		}
		out = append(out, obj)
	}
	return out, nil
}
