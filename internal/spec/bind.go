package spec

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type built struct {
	files []genFile
	notes []Note
}

func build(d doc, ops []operation) built {
	var b built
	var global []Note
	for _, edge := range d.edges {
		note := Note{Kind: "edge", Text: edge}
		b.notes = append(b.notes, note)
		global = append(global, note)
	}
	for _, req := range d.reqs {
		if req.clarify {
			note := Note{Kind: "clarify", Text: req.id + " " + req.text}
			b.notes = append(b.notes, note)
			global = append(global, note)
		}
	}
	used := map[string]bool{}
	load := loadLines(d.criteria)
	loaded := false
	slugs := map[string]int{}
	var unplaced []Note
	for _, st := range d.stories {
		file, notes, consumed := storyFile(d, st, ops)
		for key := range consumed {
			used[key] = true
		}
		b.notes = append(b.notes, notes...)
		if file.count == 0 {
			unplaced = append(unplaced, notes...)
			continue
		}
		file.slug = uniqueSlug(slugs, file.slug)
		file.opens = append([]Note{}, notes...)
		noteLoad(&b, &file, load, &loaded)
		b.files = append(b.files, file)
	}
	rest, extra := contractFile(d, ops, used)
	b.notes = append(b.notes, extra...)
	if rest.count > 0 {
		rest.slug = uniqueSlug(slugs, rest.slug)
		rest.opens = append([]Note{}, extra...)
		noteLoad(&b, &rest, load, &loaded)
		b.files = append(b.files, rest)
	} else {
		unplaced = append(unplaced, extra...)
	}
	if len(b.files) > 0 {
		head := append(append([]Note{}, global...), unplaced...)
		b.files[0].opens = append(head, b.files[0].opens...)
	}
	placeReqs(b.files, d)
	for i := range b.files {
		b.files[i].text = render(b.files[i])
	}
	return b
}

func placeReqs(files []genFile, d doc) {
	if len(files) == 0 {
		return
	}
	var reqs []genReq
	seen := map[string]bool{}
	add := func(id, text string) {
		id = canonID(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		reqs = append(reqs, genReq{id: id, text: strings.TrimSpace(text)})
	}
	for _, req := range d.reqs {
		add(req.id, req.text)
	}
	for _, req := range d.acs {
		add(req.id, req.text)
	}
	for _, st := range d.stories {
		for _, id := range findIDs(st.title + "\n" + st.body) {
			add(id, "")
		}
		for _, sc := range st.scenes {
			for _, id := range sc.criterionIDs() {
				add(id, "")
			}
		}
	}
	for _, req := range reqs {
		at := fileFor(files, d, req.id)
		files[at].reqs = append(files[at].reqs, req)
	}
}

func fileFor(files []genFile, d doc, id string) int {
	for _, st := range d.stories {
		if !mentions(st, id) {
			continue
		}
		for i, file := range files {
			if file.story == st.id {
				return i
			}
		}
	}
	return 0
}

func mentions(st story, id string) bool {
	if containsID(st.title+"\n"+st.body, id) {
		return true
	}
	for _, sc := range st.scenes {
		if containsID(strings.Join(sc.criterionIDs(), " "), id) {
			return true
		}
	}
	return false
}

func containsID(text, id string) bool {
	for _, found := range findIDs(text) {
		if found == id {
			return true
		}
	}
	return false
}

func noteLoad(b *built, file *genFile, load []string, loaded *bool) {
	if *loaded || len(load) == 0 {
		return
	}
	file.loadLines = load
	file.load = true
	*loaded = true
	note := Note{Kind: "load", Text: "duration is 30s; the spec did not state a window"}
	b.notes = append(b.notes, note)
	file.opens = append(file.opens, note)
}

func storyFile(d doc, st story, ops []operation) (genFile, []Note, map[string]bool) {
	file := genFile{slug: slug(st.title), title: st.title, tags: storyTags(d, st), story: st.id}
	if file.title == "" {
		file.title = "API"
	}
	if file.slug == "" {
		file.slug = "feature"
	}
	for _, req := range d.reqs {
		if req.clarify && slices.Contains(file.tags, req.id) {
			file.clarifies = append(file.clarifies, req.id)
		}
	}
	used := map[string]bool{}
	names := map[string]int{}
	var notes []Note
	for _, sc := range st.scenes {
		op, ok := bindScene(sc, ops)
		if !ok {
			notes = append(notes, Note{Kind: "skipped", Text: located(sc, sceneName(sc)+": name one call in When, for example POST /orders/{id}")})
			continue
		}
		if !op.synthetic {
			used[op.method+" "+op.path] = true
		}
		if msg, ok := ready(op); !ok {
			notes = append(notes, Note{Kind: "example", Text: located(sc, msg)})
			continue
		}
		scene := genScene{tags: sc.criterionIDs(), name: uniqueName(names, sceneName(sc)), where: specWhere(sc), lines: sceneLines(op)}
		file.scenarios = append(file.scenarios, scene)
		if note, ok := typeNote(scene.name, scene.lines); ok {
			notes = append(notes, note)
		}
	}
	file.count = len(file.scenarios)
	return file, notes, used
}

func contractFile(d doc, ops []operation, used map[string]bool) (genFile, []Note) {
	file := genFile{slug: "contract", title: d.title}
	if file.title == "" {
		file.title = "Contract"
	}
	names := map[string]int{}
	var notes []Note
	for _, op := range ops {
		if used[op.method+" "+op.path] {
			continue
		}
		if msg, ok := ready(op); !ok {
			notes = append(notes, Note{Kind: "example", Text: msg})
			continue
		}
		name := op.summary
		if name == "" {
			name = op.id
		}
		if name == "" {
			name = strings.ToUpper(op.method) + " " + op.path
		}
		scene := genScene{
			tags:  []string{"contract"},
			name:  uniqueName(names, strings.Join(strings.Fields(name), " ")),
			lines: sceneLines(op.clone()),
		}
		file.scenarios = append(file.scenarios, scene)
		if note, ok := typeNote(scene.name, scene.lines); ok {
			notes = append(notes, note)
		}
	}
	file.count = len(file.scenarios)
	return file, notes
}

func storyTags(d doc, st story) []string {
	tags := make([]string, 0, 1+len(d.reqs))
	tags = append(tags, st.id)
	var text strings.Builder
	text.WriteString(st.body)
	for _, sc := range st.scenes {
		text.WriteByte('\n')
		text.WriteString(sc.given)
		text.WriteByte('\n')
		text.WriteString(sc.when)
		text.WriteByte('\n')
		text.WriteString(sc.then)
		text.WriteByte('\n')
		text.WriteString(sc.name)
	}
	body := text.String()
	var frs []string
	for _, req := range d.reqs {
		if len(d.stories) == 1 || strings.Contains(body, req.id) {
			frs = append(frs, req.id)
		}
	}
	slices.Sort(frs)
	return append(tags, frs...)
}

func bindScene(sc scene, ops []operation) (operation, bool) {
	if method, path, ok := methodPath(sc.when); ok {
		if op, found := findOp(ops, method, path); found {
			return applyCall(op, sc.then, path), true
		}
		return applyCall(synthetic(method, path), sc.then, path), true
	}
	text := strings.TrimSpace(sc.name + " " + sc.when + " " + sc.then)
	if op, ok := mentionedID(text, ops); ok {
		return applyCall(op, sc.then, ""), true
	}
	if op, ok := mentionedPath(text, ops); ok {
		return applyCall(op, sc.then, ""), true
	}
	if len(ops) == 1 {
		return applyCall(ops[0].clone(), sc.then, ""), true
	}
	return operation{}, false
}

func applyCall(op operation, then, rawPath string) operation {
	if n := statusCode(then); n > 0 {
		op.status = n
	}
	if rawPath != "" {
		fillPathExamples(&op, rawPath)
	}
	_, query := splitQuery(rawPath)
	for _, p := range query {
		if !hasParam(op.params, p.in, p.name) {
			op.params = append(op.params, p)
		}
	}
	return op
}

func fillPathExamples(op *operation, raw string) {
	path, _ := splitQuery(raw)
	given, pattern := splitPath(path), splitPath(op.path)
	if len(given) != len(pattern) {
		return
	}
	for i := range pattern {
		name, ok := strings.CutPrefix(pattern[i], "{")
		if !ok || !strings.HasSuffix(name, "}") || given[i] == pattern[i] {
			continue
		}
		name = strings.TrimSuffix(name, "}")
		setParamExample(op, name, given[i])
	}
}

func setParamExample(op *operation, name, value string) {
	for i := range op.params {
		if op.params[i].in == "path" && op.params[i].name == name {
			if op.params[i].example == nil {
				op.params[i].example = value
			}
			return
		}
	}
	op.params = append(op.params, parameter{name: name, in: "path", required: true, example: value})
}

func ready(op operation) (string, bool) {
	var missing []string
	for _, part := range splitPath(op.path) {
		name, ok := strings.CutPrefix(part, "{")
		if !ok || !strings.HasSuffix(name, "}") {
			continue
		}
		name = strings.TrimSuffix(name, "}")
		if !paramExample(op, name) {
			missing = append(missing, name)
		}
	}
	missing = append(missing, credentialGaps(op)...)
	if op.bodyRequired && op.bodyExample == nil {
		switch {
		case op.body != nil:
			if gaps := uncovered(*op.body); len(gaps) > 0 {
				missing = append(missing, gaps...)
			} else if !bodyCovered(*op.body) {
				missing = append(missing, "request")
			}
		default:
			missing = append(missing, "request")
		}
	}
	if len(missing) == 0 {
		return "", true
	}
	return strings.ToUpper(op.method) + " " + op.path + ": add example for " + strings.Join(missing, ", "), false
}

func paramExample(op operation, name string) bool {
	for _, p := range op.params {
		if p.in == "path" && p.name == name && p.example != nil {
			return true
		}
	}
	return false
}

func uncovered(s schema) []string {
	if s.Example != nil || len(s.Enum) > 0 {
		return nil
	}
	var out []string
	for _, key := range s.Required {
		prop, ok := s.Properties[key]
		if !ok || !bodyCovered(prop) {
			out = append(out, key)
		}
	}
	return out
}

func bodyCovered(s schema) bool {
	if s.Example != nil || len(s.Enum) > 0 {
		return true
	}
	if s.Type == "object" || len(s.Properties) > 0 {
		if len(s.Required) == 0 {
			return false
		}
		for _, key := range s.Required {
			prop, ok := s.Properties[key]
			if !ok || !bodyCovered(prop) {
				return false
			}
		}
		return true
	}
	return false
}

func specWhere(sc scene) string {
	if sc.file == "" || sc.line == 0 {
		return ""
	}
	return sc.file + ":" + strconv.Itoa(sc.line)
}

func located(sc scene, text string) string {
	if where := specWhere(sc); where != "" {
		return where + ": " + text
	}
	return text
}

func synthetic(method, raw string) operation {
	path, query := splitQuery(raw)
	return operation{method: method, path: path, params: query, status: defaultStatus(method), synthetic: true}
}

func findOp(ops []operation, method, raw string) (operation, bool) {
	path, _ := splitQuery(raw)
	for _, op := range ops {
		if op.method == method && pathsEqual(op.path, path) {
			return op.clone(), true
		}
	}
	return operation{}, false
}

func mentionedID(text string, ops []operation) (operation, bool) {
	var hits []operation
	for _, op := range ops {
		if op.id != "" && hasWord(text, op.id) {
			hits = append(hits, op)
		}
	}
	if len(hits) != 1 {
		return operation{}, false
	}
	return hits[0].clone(), true
}

func mentionedPath(text string, ops []operation) (operation, bool) {
	var hits []operation
	for _, op := range ops {
		if strings.Contains(text, op.path) {
			hits = append(hits, op)
		}
	}
	if len(hits) != 1 {
		return operation{}, false
	}
	return hits[0].clone(), true
}

func pathsEqual(pattern, given string) bool {
	if pattern == given {
		return true
	}
	left, right := splitPath(pattern), splitPath(given)
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	for i := range left {
		if strings.HasPrefix(left[i], "{") && strings.HasSuffix(left[i], "}") {
			continue
		}
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	path, _, _ = strings.Cut(path, "?")
	var out []string
	for part := range strings.SplitSeq(strings.Trim(path, "/"), "/") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitQuery(raw string) (string, []parameter) {
	path, query, ok := strings.Cut(raw, "?")
	if !ok {
		return path, nil
	}
	var params []parameter
	for pair := range strings.SplitSeq(query, "&") {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || !ident(name) {
			continue
		}
		params = append(params, parameter{name: name, in: "query", required: true, example: value})
	}
	return path, params
}

func hasParam(params []parameter, in, name string) bool {
	for _, p := range params {
		if p.in == in && p.name == name {
			return true
		}
	}
	return false
}

func (op operation) clone() operation {
	op.params = append([]parameter(nil), op.params...)
	return op
}

var (
	callRE   = regexp.MustCompile(`(?i)\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(/[A-Za-z0-9_./{}~:?=&%-]+)`)
	statusRE = regexp.MustCompile(`\b([1-5]\d\d)\b`)
)

func methodPath(text string) (string, string, bool) {
	m := callRE.FindStringSubmatch(text)
	if m == nil {
		return "", "", false
	}
	return strings.ToLower(m[1]), strings.TrimRight(m[2], ".,);"), true
}

func statusCode(text string) int {
	m := statusRE.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func hasWord(text, word string) bool {
	rest := text
	for {
		i := strings.Index(rest, word)
		if i < 0 {
			return false
		}
		end := i + len(word)
		left := i == 0 || !isWordByte(rest[i-1])
		right := end == len(rest) || !isWordByte(rest[end])
		if left && right {
			return true
		}
		rest = rest[i+1:]
	}
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func sceneName(sc scene) string {
	name := sc.name
	if name == "" {
		name = sc.then
	}
	if name == "" {
		name = sc.when
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "request"
	}
	return trimLen(name, 80)
}

func trimLen(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}

func uniqueName(seen map[string]int, name string) string {
	seen[name]++
	if seen[name] == 1 {
		return name
	}
	return name + " " + strconv.Itoa(seen[name])
}

func uniqueSlug(seen map[string]int, slug string) string {
	seen[slug]++
	if seen[slug] == 1 {
		return slug
	}
	return slug + "-" + strconv.Itoa(seen[slug])
}

func credentialGaps(op operation) []string {
	gaps, _ := credentials(op)
	return gaps
}

func credentials(op operation) ([]string, map[string]bool) {
	send := map[string]bool{}
	var missing []string
	for _, p := range op.params {
		if p.in != "query" && p.in != "header" && p.in != "cookie" {
			continue
		}
		if !p.required {
			continue
		}
		if p.example == nil || (p.in == "query" && !ident(p.name)) {
			missing = append(missing, p.name)
			continue
		}
		send[p.in+"\x00"+p.name] = true
	}
	secMissing, secSend := securitySend(op)
	for _, name := range secMissing {
		if !hasFold(missing, name) {
			missing = append(missing, name)
		}
	}
	for key := range secSend {
		send[key] = true
	}
	return missing, send
}

func securitySend(op operation) ([]string, map[string]bool) {
	if !op.securitySet || len(op.security) == 0 {
		return nil, nil
	}
	var first []string
	for _, alt := range op.security {
		if len(alt) == 0 {
			return nil, nil
		}
		gaps, send, ok := oneSecurity(op, alt)
		if ok {
			return nil, send
		}
		if first == nil {
			first = gaps
		}
	}
	return first, nil
}

func oneSecurity(op operation, alt []string) ([]string, map[string]bool, bool) {
	var gaps []string
	send := map[string]bool{}
	for _, name := range alt {
		scheme, ok := op.schemes[name]
		if !ok {
			gaps = append(gaps, name)
			continue
		}
		in, param, ok := scheme.place()
		if !ok {
			gaps = append(gaps, name)
			continue
		}
		got, found := paramWithExample(op, in, param)
		if !found || (in == "query" && !ident(got.name)) {
			gaps = append(gaps, param)
			continue
		}
		send[in+"\x00"+got.name] = true
	}
	if len(gaps) > 0 {
		return gaps, nil, false
	}
	return nil, send, true
}

func (s secScheme) place() (string, string, bool) {
	switch s.typ {
	case "apiKey":
		if s.name == "" {
			return "", "", false
		}
		switch s.in {
		case "header", "query", "cookie":
			return s.in, s.name, true
		default:
			return "", "", false
		}
	case "http", "oauth2", "openIdConnect":
		return "header", "Authorization", true
	default:
		return "", "", false
	}
}

func paramWithExample(op operation, in, name string) (parameter, bool) {
	for _, p := range op.params {
		if p.in == in && p.example != nil && nameMatch(in, p.name, name) {
			return p, true
		}
	}
	return parameter{}, false
}

func nameMatch(in, got, want string) bool {
	if in == "header" {
		return strings.EqualFold(got, want)
	}
	return got == want
}

func hasFold(list []string, name string) bool {
	for _, item := range list {
		if strings.EqualFold(item, name) {
			return true
		}
	}
	return false
}
