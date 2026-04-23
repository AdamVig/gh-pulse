package main

import "fmt"

type checkContext struct {
	Kind       string
	Name       string
	Conclusion string
	State      string
}

type checkContextPayload struct {
	HeadContexts  []checkContext
	QueueContexts []checkContext
}

type rawContext struct {
	Type       string `json:"__typename"`
	Name       string
	Conclusion string
	Context    string
	State      string
}

func extractFailingCheckNames(payload checkContextPayload, capCount int) []string {
	ordered := make([]string, 0, capCount)
	seen := map[string]struct{}{}
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		ordered = append(ordered, name)
	}
	collect := func(contexts []checkContext) {
		for _, c := range contexts {
			switch c.Kind {
			case "CheckRun":
				if isFailingCheckRunConclusion(c.Conclusion) {
					add(c.Name)
				}
			case "StatusContext":
				if isFailingStatusContextState(c.State) {
					add(c.Name)
				}
			}
		}
	}
	collect(payload.HeadContexts)
	collect(payload.QueueContexts)
	if len(ordered) <= capCount {
		return ordered
	}
	trimmed := append([]string{}, ordered[:capCount]...)
	trimmed = append(trimmed, fmt.Sprintf("+%d more", len(ordered)-capCount))
	return trimmed
}

func isFailingCheckRunConclusion(conclusion string) bool {
	switch conclusion {
	case "FAILURE", "TIMED_OUT", "CANCELLED", "STARTUP_FAILURE", "ACTION_REQUIRED":
		return true
	default:
		return false
	}
}

func isFailingStatusContextState(state string) bool {
	return state == "FAILURE" || state == "ERROR"
}

func normalizeContexts(raw []rawContext) []checkContext {
	out := make([]checkContext, 0, len(raw))
	for _, c := range raw {
		name := c.Name
		if c.Type == "StatusContext" {
			name = c.Context
		}
		out = append(out, checkContext{Kind: c.Type, Name: name, Conclusion: c.Conclusion, State: c.State})
	}
	return out
}
