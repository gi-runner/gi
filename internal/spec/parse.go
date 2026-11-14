package spec

import (
	"regexp"
	"strconv"
	"strings"
)

type doc struct {
	title    string
	stories  []story
	reqs     []requirement
	acs      []requirement
	criteria []string
	edges    []string
}

type story struct {
	id, title, body string
	scenes          []scene
}

type scene struct {
	name, given, when, then string
	file                    string
	line                    int
	ids                     []string
}

type requirement struct {
	id, text string
	clarify  bool
}

var (
	userStoryRE = regexp.MustCompile(`(?i)^###\s+User Story\s+(\d+)\s+[-—–]\s+(.+)$`)
	priorityRE  = regexp.MustCompile(`(?i)\s+\(Priority:.*\)$`)
	frRE        = regexp.MustCompile(`(?i)^[-*]\s+\*{0,2}(FR-\d+)\*{0,2}\s*:\s*(.+)$`)
	acRE        = regexp.MustCompile(`(?i)^[-*]\s+\*{0,2}(AC-\d+(?:-\d+)*)\*{0,2}\s*:\s*(.+)$`)
	scRE        = regexp.MustCompile(`(?i)^[-*]\s+\*{0,2}SC-\d+\*{0,2}\s*:\s*(.+)$`)
	reqHeadRE   = regexp.MustCompile(`(?i)^###\s+Requirement:\s+(.+)$`)
	sceneHeadRE = regexp.MustCompile(`(?i)^####\s+Scenario:\s+(.+)$`)
	gwtRE       = regexp.MustCompile(`(?i)^[-*]\s+(GIVEN|WHEN|THEN)\s+(.*)$`)
	sceneWordRE = regexp.MustCompile(`(?i)(?:^|[\s,.;])(given|when|then)\b`)
	shallRE     = regexp.MustCompile(`(?i)^WHEN\s+(.+?)\s+THE SYSTEM SHALL\s+(.+)$`)
	numRE       = regexp.MustCompile(`^\d+\.\s+(.*)$`)
	linkRE      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

func parseDoc(text string) doc {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	switch detect(text) {
	case "kiro":
		return parseKiro(text)
	case "openspec":
		return parseOpenSpec(text)
	default:
		return parseSpecKit(text)
	}
}

func detect(text string) string {
	if strings.Contains(strings.ToUpper(text), "THE SYSTEM SHALL") {
		return "kiro"
	}
	if strings.Contains(strings.ToLower(text), "#### scenario:") {
		return "openspec"
	}
	return "speckit"
}

func parseSpecKit(text string) doc {
	var d doc
	var cur *story
	var edges bool
	n := 0
	for raw := range strings.SplitSeq(text, "\n") {
		n++
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if title, ok := strings.CutPrefix(line, "# "); ok && d.title == "" {
			d.title = strings.TrimSpace(strings.TrimPrefix(title, "Feature Specification:"))
		}
		if id, title, ok := storyHead(line); ok {
			d.stories = append(d.stories, story{id: "US" + id, title: title})
			cur = &d.stories[len(d.stories)-1]
			edges = false
			continue
		}
		if isEdgeHead(line) {
			edges = true
			continue
		}
		if strings.HasPrefix(line, "#") {
			edges = false
		}
		if edges && cur == nil {
			if item, ok := bullet(line); ok {
				d.edges = append(d.edges, item)
			}
			continue
		}
		if fr := frRE.FindStringSubmatch(line); fr != nil {
			d.reqs = append(d.reqs, requirement{id: canonID(fr[1]), text: fr[2], clarify: clarifies(fr[2])})
			continue
		}
		if ac := acRE.FindStringSubmatch(line); ac != nil {
			d.acs = append(d.acs, requirement{id: canonID(ac[1]), text: ac[2]})
			continue
		}
		if sc := scRE.FindStringSubmatch(line); sc != nil {
			d.criteria = append(d.criteria, sc[1])
			continue
		}
		if cur == nil {
			continue
		}
		if isAcceptance(line) {
			continue
		}
		if sc, ok := acceptance(line); ok {
			sc.line = n
			sc.ids = findIDs(line)
			cur.scenes = append(cur.scenes, sc)
			continue
		}
		if edges {
			if item, ok := bullet(line); ok {
				d.edges = append(d.edges, item)
			}
			continue
		}
		cur.body += line + "\n"
	}
	return d
}

func parseKiro(text string) doc {
	var d doc
	var current *story
	var pending string
	var pendingLine int
	flush := func() {
		pending = strings.TrimSpace(pending)
		if pending == "" {
			return
		}
		if m := shallRE.FindStringSubmatch(pending); m != nil && current != nil {
			current.scenes = append(current.scenes, scene{when: m[1], then: m[2], line: pendingLine})
		}
		pending = ""
	}
	n := 0
	for raw := range strings.SplitSeq(text, "\n") {
		n++
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			continue
		}
		if title, ok := strings.CutPrefix(line, "# "); ok && d.title == "" {
			d.title = strings.TrimSpace(title)
			continue
		}
		if strings.HasPrefix(line, "#") {
			flush()
			title := strings.TrimSpace(strings.TrimLeft(line, "#"))
			d.stories = append(d.stories, story{id: "US" + strconv.Itoa(len(d.stories)+1), title: title})
			current = &d.stories[len(d.stories)-1]
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "WHEN ") {
			flush()
			if current == nil {
				d.stories = append(d.stories, story{id: "US1", title: d.title})
				current = &d.stories[0]
			}
			pending = line
			pendingLine = n
			continue
		}
		if pending != "" {
			pending += " " + line
		}
	}
	flush()
	return d
}

func parseOpenSpec(text string) doc {
	var d doc
	var current *story
	var sc *scene
	n := 0
	for raw := range strings.SplitSeq(text, "\n") {
		n++
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if title, ok := strings.CutPrefix(line, "# "); ok && d.title == "" {
			d.title = strings.TrimSpace(title)
		}
		if m := reqHeadRE.FindStringSubmatch(line); m != nil {
			d.stories = append(d.stories, story{id: "US" + strconv.Itoa(len(d.stories)+1), title: strings.TrimSpace(m[1])})
			current = &d.stories[len(d.stories)-1]
			sc = nil
			continue
		}
		if m := sceneHeadRE.FindStringSubmatch(line); m != nil && current != nil {
			current.scenes = append(current.scenes, scene{name: strings.TrimSpace(m[1]), line: n})
			sc = &current.scenes[len(current.scenes)-1]
			continue
		}
		if sc == nil {
			continue
		}
		if m := gwtRE.FindStringSubmatch(line); m != nil {
			switch strings.ToUpper(m[1]) {
			case "GIVEN":
				sc.given = strings.TrimSpace(m[2])
			case "WHEN":
				sc.when = strings.TrimSpace(m[2])
			case "THEN":
				sc.then = strings.TrimSpace(m[2])
			}
		}
	}
	return d
}

func storyHead(line string) (id, title string, ok bool) {
	m := userStoryRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], priorityRE.ReplaceAllString(strings.TrimSpace(m[2]), ""), true
}

func isEdgeHead(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	return strings.EqualFold(strings.Trim(line, "# "), "Edge Cases")
}

func isAcceptance(line string) bool {
	return strings.Contains(strings.ToLower(stripMd(line)), "acceptance scenario")
}

func acceptance(line string) (scene, bool) {
	m := numRE.FindStringSubmatch(stripMd(line))
	if m == nil {
		return scene{}, false
	}
	return splitScene(m[1])
}

func splitScene(text string) (scene, bool) {
	var marks []struct {
		at   int
		word string
	}
	seen := map[string]bool{}
	for _, loc := range sceneWordRE.FindAllStringSubmatchIndex(text, -1) {
		word := strings.ToLower(text[loc[2]:loc[3]])
		if seen[word] {
			continue
		}
		seen[word] = true
		marks = append(marks, struct {
			at   int
			word string
		}{loc[2], word})
	}
	if !seen["when"] || !seen["then"] {
		return scene{}, false
	}
	var sc scene
	for i, mark := range marks {
		end := len(text)
		if i+1 < len(marks) {
			end = marks[i+1].at
		}
		body := strings.Trim(text[mark.at+len(mark.word):end], " \t,.;")
		switch mark.word {
		case "given":
			sc.given = body
		case "when":
			sc.when = body
		case "then":
			sc.then = body
		}
	}
	return sc, sc.when != "" && sc.then != ""
}

func bullet(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if rest, ok := strings.CutPrefix(line, "- "); ok {
		return strings.TrimSpace(rest), rest != ""
	}
	if rest, ok := strings.CutPrefix(line, "* "); ok {
		return strings.TrimSpace(rest), rest != ""
	}
	return "", false
}

func stripMd(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = linkRE.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

func clarifies(text string) bool {
	return strings.Contains(strings.ToUpper(text), "NEEDS CLARIFICATION")
}

func (sc scene) criterionIDs() []string {
	return findIDs(strings.Join(sc.ids, " ") + "\n" + sc.name + "\n" + sc.given + "\n" + sc.when + "\n" + sc.then)
}
