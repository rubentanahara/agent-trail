package tui

import (
	"cmp"
	"fmt"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const partialCostMark = "+"

type turn struct {
	session string
	summary string
	cost    float64
	priced  bool
	partial bool
}

type turnLedger struct {
	bySource map[trail.Source]*turn
	ordered  []*turn
}

func (t turn) costLabel() string {
	if !t.priced {
		return ""
	}
	label := trail.ShortDollars(t.cost)
	if t.partial {
		label += partialCostMark
	}
	return label
}

func (t *turn) add(usage *trail.Usage) {
	if !usage.Priced {
		t.partial = true
		return
	}
	t.cost += usage.CostUSD
	t.priced = true
}

func isPrompt(event trail.Event) bool {
	return event.Kind == trail.KindUser && event.Agent == ""
}

func (m model) turnOwners() map[string]string {
	owners := make(map[string]string, len(m.sessions))
	for key, session := range m.sessions {
		owners[key] = cmp.Or(session.Parent, key)
	}
	return owners
}

func (m model) turns() turnLedger {
	ledger := turnLedger{bySource: map[trail.Source]*turn{}}
	current := map[string]*turn{}
	owners := m.turnOwners()
	for _, event := range m.events {
		owner := cmp.Or(owners[event.Session], event.Session)
		switch {
		case isPrompt(event) && owner == event.Session:
			started := &turn{session: owner, summary: event.Summary}
			current[owner] = started
			ledger.bySource[event.Source] = started
			ledger.ordered = append(ledger.ordered, started)
		case event.Usage != nil && current[owner] != nil:
			current[owner].add(event.Usage)
		}
	}
	return ledger
}

func (l turnLedger) withCost(event trail.Event) trail.Event {
	if started, ok := l.bySource[event.Source]; ok && isPrompt(event) {
		event.Summary = joinCost(started.costLabel(), event.Summary)
	}
	return event
}

func joinCost(label, summary string) string {
	if label == "" {
		return summary
	}
	return label + " " + summary
}

func (l turnLedger) line(session string) string {
	var count int
	var costliest *turn
	for _, t := range l.ordered {
		if t.session != session {
			continue
		}
		count++
		if t.priced && (costliest == nil || t.cost > costliest.cost) {
			costliest = t
		}
	}
	if count == 0 {
		return ""
	}
	line := fmt.Sprintf("%d %s", count, promptNoun(count))
	if costliest == nil {
		return line
	}
	return line + " · costliest " + joinCost(costliest.costLabel(), costliest.summary)
}

func promptNoun(count int) string {
	if count == 1 {
		return "prompt"
	}
	return "prompts"
}
