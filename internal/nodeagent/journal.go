package nodeagent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jourloy/0trace0-node/internal/controlapi"
)

type journalEntry struct {
	Cursor int64                   `json:"cursor"`
	Event  controlapi.SessionEvent `json:"event"`
}

func (s *Service) journalPath() string {
	return filepath.Join(s.cfg.StateDir, "events.jsonl")
}

func (s *Service) appendEventsLocked(events []controlapi.SessionEvent) error {
	if len(events) == 0 {
		return nil
	}
	if err := os.MkdirAll(s.cfg.StateDir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.journalPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, event := range events {
		if strings.TrimSpace(event.EventID) == "" {
			event.EventID = uuid.NewString()
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		s.state.LastEventCursor++
		if err := encoder.Encode(journalEntry{
			Cursor: s.state.LastEventCursor,
			Event:  event,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) eventsAfterLocked(cursor int64, limit int) (controlapi.NodeEventsResponse, error) {
	file, err := os.Open(s.journalPath())
	if err != nil {
		if os.IsNotExist(err) {
			return controlapi.NodeEventsResponse{
				Items:      []controlapi.NodeEventRecord{},
				NextCursor: strconv.FormatInt(s.state.LastEventCursor, 10),
			}, nil
		}
		return controlapi.NodeEventsResponse{}, err
	}
	defer file.Close()

	items := make([]controlapi.NodeEventRecord, 0, limit)
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	for scanner.Scan() {
		var entry journalEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return controlapi.NodeEventsResponse{}, err
		}
		if entry.Cursor <= cursor {
			continue
		}
		items = append(items, controlapi.NodeEventRecord{
			Cursor: strconv.FormatInt(entry.Cursor, 10),
			Event:  entry.Event,
		})
		if len(items) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return controlapi.NodeEventsResponse{}, err
	}

	return controlapi.NodeEventsResponse{
		Items:      items,
		NextCursor: strconv.FormatInt(s.state.LastEventCursor, 10),
	}, nil
}

func parseCursor(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}
	return value, nil
}

func parseLimit(raw string) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 100
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return 100
	}
	if value > 500 {
		return 500
	}
	return value
}
