package trail

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type Detail struct {
	Text string
	Raw  string
}

func LoadDetail(event Event) (Detail, error) {
	line, err := readLineAt(event.Source.Path, event.Source.Offset)
	if err != nil {
		return Detail{}, err
	}
	detail := Detail{Raw: prettyJSON(line)}
	if event.Usage != nil {
		detail.Text = usageDetail(*event.Usage)
		return detail, nil
	}
	detail.Text = fullText(event, line)
	return detail, nil
}

func fullText(event Event, line []byte) string {
	events, err := newParsers[event.CLI](event.Source.Path).parse(line, &Session{})
	if err != nil || event.Source.Index >= len(events) {
		return event.Summary
	}
	return events[event.Source.Index].Text
}

func readLineAt(path string, offset int64) ([]byte, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	if _, err := handle.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return bufio.NewReader(handle).ReadBytes('\n')
}

func prettyJSON(line []byte) string {
	var out bytes.Buffer
	if json.Indent(&out, bytes.TrimSpace(line), "", "  ") != nil {
		return string(line)
	}
	return out.String()
}

func usageDetail(u Usage) string {
	cost := "not shown: no price for this model in the price list"
	if u.Priced {
		cost = Dollars(u.CostUSD) + " at API list prices"
	}
	return fmt.Sprintf("model        %s\ninput        %d\n  cache read %d\n  cache write %d\noutput       %d\n  reasoning  %d\nduration     %s\ncost         %s",
		u.Model, u.Input, u.CacheRead, u.CacheWrite, u.Output, u.Reasoning, u.Duration.Round(time.Millisecond), cost)
}
