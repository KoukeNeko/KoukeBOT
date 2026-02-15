package chat

import (
	"sort"
	"strings"
)

type tripletSections struct {
	Status   string
	Analysis string
	Response string
}

type assistantOutputView struct {
	Content   string
	Display   string
	Parsed    bool
	Sections  tripletSections
	RawOutput string
}

func parseTripletSections(raw string) (tripletSections, bool) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lower := strings.ToLower(normalized)

	type marker struct {
		name string
		tag  string
		pos  int
	}

	markers := []marker{
		{name: "status", tag: "[status]", pos: strings.Index(lower, "[status]")},
		{name: "analysis", tag: "[analysis]", pos: strings.Index(lower, "[analysis]")},
		{name: "response", tag: "[response]", pos: strings.Index(lower, "[response]")},
	}
	found := make([]marker, 0, 3)
	for _, m := range markers {
		if m.pos >= 0 {
			found = append(found, m)
		}
	}
	if len(found) == 0 {
		return tripletSections{}, false
	}
	sort.Slice(found, func(i, j int) bool {
		return found[i].pos < found[j].pos
	})

	out := tripletSections{}
	for i, m := range found {
		start := m.pos + len(m.tag)
		end := len(normalized)
		if i+1 < len(found) {
			end = found[i+1].pos
		}
		if start > len(normalized) || start > end {
			continue
		}
		content := trimTripletSectionPrefix(normalized[start:end])
		switch m.name {
		case "status":
			out.Status = content
		case "analysis":
			out.Analysis = content
		case "response":
			out.Response = content
		}
	}

	if out.Response == "" {
		return tripletSections{}, false
	}
	return out, true
}

func trimTripletSectionPrefix(s string) string {
	s = strings.TrimSpace(s)
	for {
		if s == "" {
			return s
		}
		switch {
		case strings.HasPrefix(s, ":"):
			s = strings.TrimSpace(strings.TrimPrefix(s, ":"))
		case strings.HasPrefix(s, "："):
			s = strings.TrimSpace(strings.TrimPrefix(s, "："))
		case strings.HasPrefix(s, "-"):
			s = strings.TrimSpace(strings.TrimPrefix(s, "-"))
		case strings.HasPrefix(s, ">"):
			s = strings.TrimSpace(strings.TrimPrefix(s, ">"))
		default:
			return strings.TrimSpace(s)
		}
	}
}

func prepareAssistantOutput(raw string, policy PromptGuardPolicy) assistantOutputView {
	raw = strings.TrimSpace(raw)
	if raw == "" || !policy.TripletOutputEnabled {
		return assistantOutputView{
			Content:   raw,
			Display:   raw,
			Parsed:    false,
			RawOutput: raw,
		}
	}

	sections, ok := parseTripletSections(raw)
	if !ok {
		return assistantOutputView{
			Content:   raw,
			Display:   raw,
			Parsed:    false,
			RawOutput: raw,
		}
	}

	display := raw
	switch policy.AnalysisVisibility {
	case AnalysisVisibilityHidden:
		display = sections.Response
	case AnalysisVisibilityVisible, AnalysisVisibilityDebug:
		display = raw
	default:
		display = raw
	}

	return assistantOutputView{
		Content:   strings.TrimSpace(sections.Response),
		Display:   strings.TrimSpace(display),
		Parsed:    true,
		Sections:  sections,
		RawOutput: raw,
	}
}
