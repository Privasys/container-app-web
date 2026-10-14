// Copyright (c) Privasys. All rights reserved.
// Licensed under the GNU Affero General Public License v3.0. See LICENSE.

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
	"unicode/utf8"
)

// event is one accepted webhook delivery.
type event struct {
	ID          int       `json:"id"`
	ReceivedAt  time.Time `json:"received_at"`
	ContentType string    `json:"content_type,omitempty"`
	Body        string    `json:"body"`
	// Encoding is "base64" when the body was not valid UTF-8.
	Encoding string `json:"encoding,omitempty"`
}

// eventStore keeps the last n events in memory and appends every one to a
// log file on the app's sealed volume when there is one. The volume is
// encrypted under a key only the enclave holds, so the log stays private
// even though the events arrived in the clear.
type eventStore struct {
	mu      sync.Mutex
	ring    []event
	size    int
	next    int
	logPath string
}

func newEventStore(size int, logPath string) *eventStore {
	s := &eventStore{size: size, logPath: logPath}
	s.load()
	return s
}

// load replays the log so the ring survives a restart.
func (s *eventStore) load() {
	if s.logPath == "" {
		return
	}
	f, err := os.Open(s.logPath)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*maxWebhookBody)
	for sc.Scan() {
		var ev event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		s.push(ev)
		if ev.ID >= s.next {
			s.next = ev.ID + 1
		}
	}
}

func (s *eventStore) push(ev event) {
	if len(s.ring) == s.size {
		copy(s.ring, s.ring[1:])
		s.ring = s.ring[:s.size-1]
	}
	s.ring = append(s.ring, ev)
}

// Add records a delivery and returns it.
func (s *eventStore) Add(contentType string, body []byte) event {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := event{ID: s.next, ReceivedAt: time.Now().UTC(), ContentType: contentType}
	if utf8.Valid(body) {
		ev.Body = string(body)
	} else {
		ev.Body = base64.StdEncoding.EncodeToString(body)
		ev.Encoding = "base64"
	}
	s.next++
	s.push(ev)
	s.appendLog(ev)
	return ev
}

func (s *eventStore) appendLog(ev event) {
	if s.logPath == "" {
		return
	}
	f, err := os.OpenFile(s.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("event log: %v", err)
		return
	}
	defer f.Close()
	line, _ := json.Marshal(ev)
	if _, err := f.Write(append(line, '\n')); err != nil {
		log.Printf("event log: %v", err)
	}
}

// Len is the number of events in memory.
func (s *eventStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ring)
}

// List returns a copy of the events in memory, oldest first.
func (s *eventStore) List() []event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]event, len(s.ring))
	copy(out, s.ring)
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
