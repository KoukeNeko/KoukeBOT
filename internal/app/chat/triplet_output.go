package chat

import (
	"regexp"
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
	Salvaged  bool
	Reason    string
	Sections  tripletSections
	RawOutput string
}

const (
	tripletParseReasonParsed                 = "parsed"
	tripletParseReasonNoMarkers              = "no_markers"
	tripletParseReasonMissingResponseMarker  = "missing_response_marker"
	tripletParseReasonEmptyResponse          = "empty_response"
	tripletParseReasonOrderInvalidAndSalvage = "order_invalid_response_salvaged"
)

type tripletParseOutcome struct {
	Sections tripletSections
	Parsed   bool
	Salvaged bool
	Reason   string
}

type tripletMarker struct {
	name  string
	start int
	end   int
}

var tripletMarkerPattern = regexp.MustCompile(`(?i)\[\s*(status|analysis|response)\s*\]`)

func parseTripletSections(raw string) (tripletSections, bool) {
	outcome := parseTripletOutput(raw)
	return outcome.Sections, outcome.Parsed
}

func parseTripletOutput(raw string) tripletParseOutcome {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	markers := findTripletMarkers(normalized)
	if len(markers) == 0 {
		return tripletParseOutcome{Reason: tripletParseReasonNoMarkers}
	}

	responseText, hasResponseMarker := extractLastSectionByName(normalized, markers, "response")

	statusIdx, analysisIdx, responseIdx, ok := findLastOrderedTriplet(markers)
	if !ok {
		if !hasResponseMarker {
			return tripletParseOutcome{Reason: tripletParseReasonMissingResponseMarker}
		}
		if strings.TrimSpace(responseText) == "" {
			return tripletParseOutcome{Reason: tripletParseReasonEmptyResponse}
		}
		return tripletParseOutcome{
			Sections: tripletSections{
				Response: responseText,
			},
			Salvaged: true,
			Reason:   tripletParseReasonOrderInvalidAndSalvage,
		}
	}

	responseEnd := len(normalized)
	if responseIdx+1 < len(markers) {
		responseEnd = markers[responseIdx+1].start
	}

	sections := tripletSections{
		Status:   extractSection(normalized, markers[statusIdx].end, markers[analysisIdx].start),
		Analysis: extractSection(normalized, markers[analysisIdx].end, markers[responseIdx].start),
		Response: extractSection(normalized, markers[responseIdx].end, responseEnd),
	}
	if strings.TrimSpace(sections.Response) == "" {
		if strings.TrimSpace(responseText) != "" {
			return tripletParseOutcome{
				Sections: tripletSections{
					Status:   sections.Status,
					Analysis: sections.Analysis,
					Response: responseText,
				},
				Salvaged: true,
				Reason:   tripletParseReasonOrderInvalidAndSalvage,
			}
		}
		return tripletParseOutcome{Reason: tripletParseReasonEmptyResponse}
	}

	return tripletParseOutcome{
		Sections: sections,
		Parsed:   true,
		Reason:   tripletParseReasonParsed,
	}
}

func findTripletMarkers(raw string) []tripletMarker {
	matches := tripletMarkerPattern.FindAllStringSubmatchIndex(raw, -1)
	markers := make([]tripletMarker, 0, len(matches))
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(raw[m[2]:m[3]]))
		if name != "status" && name != "analysis" && name != "response" {
			continue
		}
		markers = append(markers, tripletMarker{
			name:  name,
			start: m[0],
			end:   m[1],
		})
	}
	return markers
}

func findLastOrderedTriplet(markers []tripletMarker) (statusIdx, analysisIdx, responseIdx int, ok bool) {
	for r := len(markers) - 1; r >= 0; r-- {
		if markers[r].name != "response" {
			continue
		}
		for a := r - 1; a >= 0; a-- {
			if markers[a].name != "analysis" {
				continue
			}
			for s := a - 1; s >= 0; s-- {
				if markers[s].name == "status" {
					return s, a, r, true
				}
			}
		}
	}
	return 0, 0, 0, false
}

func extractLastSectionByName(raw string, markers []tripletMarker, name string) (string, bool) {
	for i := len(markers) - 1; i >= 0; i-- {
		if markers[i].name != name {
			continue
		}
		end := len(raw)
		if i+1 < len(markers) {
			end = markers[i+1].start
		}
		return extractSection(raw, markers[i].end, end), true
	}
	return "", false
}

func extractSection(raw string, start, end int) string {
	if start < 0 || end < 0 || start > len(raw) || end > len(raw) || start > end {
		return ""
	}
	return trimTripletSectionPrefix(raw[start:end])
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
		case strings.HasPrefix(s, "*"):
			s = strings.TrimSpace(strings.TrimPrefix(s, "*"))
		case strings.HasPrefix(s, "_"):
			s = strings.TrimSpace(strings.TrimPrefix(s, "_"))
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
			Reason:    "",
			RawOutput: raw,
		}
	}

	outcome := parseTripletOutput(raw)
	if !outcome.Parsed && !outcome.Salvaged {
		return assistantOutputView{
			Content:   raw,
			Display:   raw,
			Parsed:    false,
			Salvaged:  false,
			Reason:    outcome.Reason,
			Sections:  outcome.Sections,
			RawOutput: raw,
		}
	}

	sections := outcome.Sections
	display := raw
	switch policy.AnalysisVisibility {
	case AnalysisVisibilityHidden:
		display = strings.TrimSpace(sections.Response)
	case AnalysisVisibilityVisible, AnalysisVisibilityDebug:
		display = raw
	default:
		display = raw
	}

	return assistantOutputView{
		Content:   strings.TrimSpace(sections.Response),
		Display:   strings.TrimSpace(display),
		Parsed:    outcome.Parsed,
		Salvaged:  outcome.Salvaged,
		Reason:    outcome.Reason,
		Sections:  sections,
		RawOutput: raw,
	}
}
