package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Persistent history: every message ever seen is appended to one JSONL file
// per channel under history/. The archive accumulates for as long as the app
// runs — restarts lose nothing — and the search API reads straight from it.
// ---------------------------------------------------------------------------

type Store struct {
	mu   sync.Mutex
	dir  string
	seen map[string]bool // "channel/id" already on disk
}

var reSafeChannel = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, seen: make(map[string]bool)}
	s.loadIndex()
	return s, nil
}

// loadIndex scans existing files once at startup so appends can dedupe.
func (s *Store) loadIndex() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		channel := strings.TrimSuffix(e.Name(), ".jsonl")
		for _, m := range s.readFile(channel) {
			s.seen[m.Channel+"/"+strconv.Itoa(m.ID)] = true
		}
	}
}

func (s *Store) filePath(channel string) string {
	return filepath.Join(s.dir, channel+".jsonl")
}

// Append writes messages the archive has not seen yet. Empty service posts
// are skipped, exactly like in the live feed.
func (s *Store) Append(msgs []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byChannel := map[string][]Message{}
	for _, m := range msgs {
		if m.Channel == "" || !reSafeChannel.MatchString(m.Channel) {
			continue
		}
		if strings.TrimSpace(m.Text) == "" && !m.HasMedia() {
			continue
		}
		key := m.Channel + "/" + strconv.Itoa(m.ID)
		if s.seen[key] {
			continue
		}
		s.seen[key] = true
		byChannel[m.Channel] = append(byChannel[m.Channel], m)
	}

	for channel, group := range byChannel {
		fh, err := os.OpenFile(s.filePath(channel), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			continue
		}
		w := bufio.NewWriter(fh)
		for _, m := range group {
			if line, err := json.Marshal(m); err == nil {
				_, _ = w.Write(line)
				_, _ = w.WriteString("\n")
			}
		}
		_ = w.Flush()
		_ = fh.Close()
	}
}

// readFile parses one channel's archive, skipping corrupt lines.
func (s *Store) readFile(channel string) []Message {
	fh, err := os.Open(s.filePath(channel))
	if err != nil {
		return nil
	}
	defer fh.Close()

	var out []Message
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var m Message
		if err := json.Unmarshal(sc.Bytes(), &m); err == nil && m.ID > 0 {
			out = append(out, m)
		}
	}
	return out
}

// LoadRecent returns the newest n messages of one channel, oldest-first.
func (s *Store) LoadRecent(channel string, n int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !reSafeChannel.MatchString(channel) {
		return nil
	}
	msgs := s.readFile(channel)
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID < msgs[j].ID })
	if len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	return msgs
}

// Search scans the archive for a case-insensitive substring. channel == ""
// searches every channel. Newest results first, capped at limit.
func (s *Store) Search(query, channel string, limit int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || limit < 1 {
		return nil
	}

	var channels []string
	if channel != "" {
		if !reSafeChannel.MatchString(channel) {
			return nil
		}
		channels = []string{channel}
	} else {
		entries, err := os.ReadDir(s.dir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonl") {
				channels = append(channels, strings.TrimSuffix(e.Name(), ".jsonl"))
			}
		}
	}

	var hits []Message
	for _, ch := range channels {
		for _, m := range s.readFile(ch) {
			if strings.Contains(strings.ToLower(m.Text), query) {
				hits = append(hits, m)
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Time != hits[j].Time {
			return hits[i].Time > hits[j].Time
		}
		return hits[i].ID > hits[j].ID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// Count reports how many messages the archive holds in total.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

// RemoveChannel deletes a channel's archive when the user removes the channel.
func (s *Store) RemoveChannel(channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !reSafeChannel.MatchString(channel) {
		return
	}
	_ = os.Remove(s.filePath(channel))
	prefix := channel + "/"
	for k := range s.seen {
		if strings.HasPrefix(k, prefix) {
			delete(s.seen, k)
		}
	}
}
