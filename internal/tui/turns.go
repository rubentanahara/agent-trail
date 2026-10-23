package tui

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const partialCostMark = "+"

type turn struct {
	session string
	summary string
	at      time.Time
	cost    float64
	priced  bool
	partial bool
}

type ledgerCache struct {
	ledger   turnLedger
	revision int
	built    bool
	builds   int
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
	cache := m.ledger
	if cache == nil {
		return m.buildTurns()
	}
	if !cache.built || cache.revision != m.revision {
		cache.ledger, cache.revision, cache.built = m.buildTurns(), m.revision, true
		cache.builds++
	}
	return cache.ledger
}

func (m model) buildTurns() turnLedger {
	ledger := turnLedger{bySource: map[trail.Source]*turn{}}
	owners := m.turnOwners()
	byOwner := map[string][]*turn{}
	for _, event := range m.events {
		owner := cmp.Or(owners[event.Session], event.Session)
		if !isPrompt(event) || owner != event.Session {
			continue
		}
		started := &turn{session: owner, summary: event.Summary, at: event.Time}
		byOwner[owner] = append(byOwner[owner], started)
		ledger.bySource[event.Source] = started
		ledger.ordered = append(ledger.ordered, started)
	}
	for _, turns := range byOwner {
		slices.SortStableFunc(turns, func(a, b *turn) int { return a.at.Compare(b.at) })
	}
	for _, event := range m.events {
		if event.Usage == nil {
			continue
		}
		owner := cmp.Or(owners[event.Session], event.Session)
		if charged := latestTurnAt(byOwner[owner], event.Time); charged != nil {
			charged.add(event.Usage)
		}
	}
	return ledger
}

func latestTurnAt(turns []*turn, at time.Time) *turn {
	after := sort.Search(len(turns), func(i int) bool { return turns[i].at.After(at) })
	if after == 0 {
		return nil
	}
	return turns[after-1]
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
