package trail

import (
	"os"
	"runtime"
	"sync"
	"time"
)

type startRead struct {
	path    string
	file    *transcript
	session *Session
	events  []Event
}

func (t *Trail) startReads(cutoff time.Time) []*startRead {
	var reads []*startRead
	for _, path := range t.discover() {
		file := t.transcripts[path]
		info, err := os.Stat(path)
		if err == nil && info.ModTime().Before(cutoff) {
			file.parked, file.parkedSize = true, info.Size()
			continue
		}
		reads = append(reads, &startRead{path: path, file: file, session: t.session(file, path)})
	}
	return reads
}

func (t *Trail) readConcurrently(reads []*startRead) []Event {
	groups := groupBySession(reads)
	pending := make(chan []*startRead)
	var workers sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		workers.Go(func() {
			for group := range pending {
				for _, read := range group {
					read.events = t.readSession(read.path, read.file, read.session)
				}
			}
		})
	}
	for _, group := range groups {
		pending <- group
	}
	close(pending)
	workers.Wait()
	var events []Event
	for _, read := range reads {
		events = append(events, read.events...)
	}
	return events
}

func groupBySession(reads []*startRead) [][]*startRead {
	var groups [][]*startRead
	position := map[string]int{}
	for _, read := range reads {
		key := read.file.sessionKey
		index, seen := position[key]
		if !seen {
			index = len(groups)
			position[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], read)
	}
	return groups
}
