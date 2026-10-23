package trail

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
)

const (
	CutNotice          = "[The start of this conversation is not included.]\n"
	conversationStamp  = "01-02 15:04"
	conversationJoiner = "\n"
)

var conversationKinds = map[Kind]bool{KindUser: true, KindAssistant: true, KindTool: true}

type ConversationText struct {
	Text string
	Cut  bool
}

func Conversation(events []Event, limit int) ConversationText {
	var spoken []Event
	for _, event := range events {
		if conversationKinds[event.Kind] {
			spoken = append(spoken, event)
		}
	}
	slices.SortStableFunc(spoken, func(a, b Event) int { return a.Time.Compare(b.Time) })
	full := fullTexts(spoken)
	lines := make([]string, len(spoken))
	for i, event := range spoken {
		lines[i] = conversationLine(event, full[event.Source])
	}
	return newestWithin(lines, limit)
}

func conversationLine(event Event, text string) string {
	if event.Kind == KindTool || text == "" {
		text = strings.Join(strings.Fields(event.Summary), " ")
	}
	return fmt.Sprintf("[%s] %s %s: %s", event.Time.Local().Format(conversationStamp), event.Who(), event.Kind, text)
}

func newestWithin(lines []string, limit int) ConversationText {
	used, first := 0, len(lines)
	for first > 0 && used+len(lines[first-1])+len(conversationJoiner) <= limit {
		first--
		used += len(lines[first]) + len(conversationJoiner)
	}
	kept := strings.Join(lines[first:], conversationJoiner)
	if first == len(lines) && len(lines) > 0 {
		kept = strings.ToValidUTF8(tail(lines[first-1], limit), "")
		first--
	}
	if first == 0 {
		return ConversationText{Text: kept}
	}
	return ConversationText{Text: CutNotice + kept, Cut: true}
}

func tail(text string, size int) string {
	return text[max(0, len(text)-size):]
}

func fullTexts(events []Event) map[Source]string {
	wanted := map[string]map[int64]bool{}
	cli := map[string]CLI{}
	for _, event := range events {
		if event.Kind == KindTool {
			continue
		}
		path := event.Source.Path
		if wanted[path] == nil {
			wanted[path] = map[int64]bool{}
		}
		wanted[path][event.Source.Offset] = true
		cli[path] = event.CLI
	}
	texts := map[Source]string{}
	for path, offsets := range wanted {
		scanTexts(path, cli[path], offsets, texts)
	}
	return texts
}

func scanTexts(path string, cli CLI, offsets map[int64]bool, texts map[Source]string) {
	handle, err := os.Open(path)
	if err != nil {
		return
	}
	defer handle.Close()
	reader := bufio.NewReader(handle)
	lineParser := newParsers[cli](path)
	var offset int64
	for len(offsets) > 0 {
		line, err := reader.ReadBytes('\n')
		if offsets[offset] {
			delete(offsets, offset)
			parsed, _ := lineParser.parse(line, &Session{})
			for index, event := range parsed {
				texts[Source{Path: path, Offset: offset, Index: index}] = event.Text
			}
		}
		offset += int64(len(line))
		if err != nil {
			return
		}
	}
}
