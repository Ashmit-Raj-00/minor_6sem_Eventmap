package store

import (
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func LoadFromCSV(m *Memory, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("csv dir required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	type fileSpec struct {
		name string
		fn   func([][]string) error
	}

	specs := []fileSpec{
		{"users.csv", func(rows [][]string) error { return loadUsers(m, rows) }},
		{"events.csv", func(rows [][]string) error { return loadEvents(m, rows) }},
		{"sessions.csv", func(rows [][]string) error { return loadSessions(m, rows) }},
		{"participants.csv", func(rows [][]string) error { return loadParticipants(m, rows) }},
		{"user_points.csv", func(rows [][]string) error { return loadUserPoints(m, rows) }},
		{"event_points.csv", func(rows [][]string) error { return loadEventPoints(m, rows) }},
		{"checkins.csv", func(rows [][]string) error { return loadCheckins(m, rows) }},
	}

	for _, s := range specs {
		path := filepath.Join(dir, s.name)
		rows, err := readCSVFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("%s: %w", s.name, err)
		}
		if len(rows) == 0 {
			continue
		}
		if err := s.fn(rows); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}

	return nil
}

func SaveToCSV(m *Memory, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("csv dir required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if err := writeCSVAtomic(dir, "users.csv", dumpUsers(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "events.csv", dumpEvents(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "sessions.csv", dumpSessions(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "participants.csv", dumpParticipants(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "user_points.csv", dumpUserPoints(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "event_points.csv", dumpEventPoints(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "checkins.csv", dumpCheckins(m)); err != nil {
		return err
	}
	return nil
}

func readCSVFile(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

func writeCSVAtomic(dir, name string, rows [][]string) error {
	path := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	w := csv.NewWriter(tmp)
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func mustTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err == nil {
		return t
	}
	t, _ = time.Parse(time.RFC3339, s)
	return t
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func splitTags(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "|")
}

func loadUsers(m *Memory, rows [][]string) error {
	// header: id,username,role,salt,password_hash_b64,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 6 {
			continue
		}
		id := strings.TrimSpace(r[0])
		username := strings.TrimSpace(r[1])
		role := Role(strings.TrimSpace(r[2]))
		salt := strings.TrimSpace(r[3])
		hashB64 := strings.TrimSpace(r[4])
		createdAt := parseTime(r[5])
		if id == "" || username == "" {
			continue
		}
		if role != RoleAdmin && role != RoleOrganizer && role != RoleAttendee {
			role = RoleAttendee
		}
		var hash []byte
		if hashB64 != "" {
			b, err := base64.RawStdEncoding.DecodeString(hashB64)
			if err == nil {
				hash = b
			}
		}
		u := User{
			ID:           id,
			Username:     username,
			Role:         role,
			Salt:         salt,
			PasswordHash: hash,
			CreatedAt:    createdAt,
		}
		m.usersByID[u.ID] = u
		m.usersByUsername[strings.ToLower(username)] = u.ID
	}
	return nil
}

func loadEvents(m *Memory, rows [][]string) error {
	// header: id,title,description,starts_at,ends_at,lat,lng,address,tags,checkin_radius_km,created_by,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 12 {
			continue
		}
		e := Event{
			ID:              strings.TrimSpace(r[0]),
			Title:           r[1],
			Description:     r[2],
			StartsAt:        parseTime(r[3]),
			EndsAt:          parseTime(r[4]),
			Lat:             parseFloat(r[5]),
			Lng:             parseFloat(r[6]),
			Address:         r[7],
			Tags:            splitTags(r[8]),
			CheckinRadiusKm: parseFloat(r[9]),
			CreatedBy:       strings.TrimSpace(r[10]),
			CreatedAt:       parseTime(r[11]),
		}
		if e.ID == "" {
			continue
		}
		e.Tags = normalizeTags(e.Tags)
		m.eventsByID[e.ID] = e
	}
	return nil
}

func loadSessions(m *Memory, rows [][]string) error {
	// header: id,event_id,title,starts_at,ends_at,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 6 {
			continue
		}
		s := Session{
			ID:        strings.TrimSpace(r[0]),
			EventID:   strings.TrimSpace(r[1]),
			Title:     r[2],
			StartsAt:  parseTime(r[3]),
			EndsAt:    parseTime(r[4]),
			CreatedAt: parseTime(r[5]),
		}
		if s.ID == "" || s.EventID == "" {
			continue
		}
		m.sessions[s.EventID] = append(m.sessions[s.EventID], s)
	}
	for eid := range m.sessions {
		sort.Slice(m.sessions[eid], func(i, j int) bool { return m.sessions[eid][i].StartsAt.Before(m.sessions[eid][j].StartsAt) })
	}
	return nil
}

func loadParticipants(m *Memory, rows [][]string) error {
	// header: event_id,user_id,joined_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "event_id") {
			continue
		}
		if len(r) < 3 {
			continue
		}
		eventID := strings.TrimSpace(r[0])
		userID := strings.TrimSpace(r[1])
		if eventID == "" || userID == "" {
			continue
		}
		if m.participants[eventID] == nil {
			m.participants[eventID] = map[string]Participant{}
		}
		m.participants[eventID][userID] = Participant{
			EventID:  eventID,
			UserID:   userID,
			JoinedAt: parseTime(r[2]),
		}
	}
	return nil
}

func loadUserPoints(m *Memory, rows [][]string) error {
	// header: user_id,points
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "user_id") {
			continue
		}
		if len(r) < 2 {
			continue
		}
		uid := strings.TrimSpace(r[0])
		if uid == "" {
			continue
		}
		m.userPoints[uid] = parseInt(r[1])
	}
	return nil
}

func loadEventPoints(m *Memory, rows [][]string) error {
	// header: event_id,user_id,points
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "event_id") {
			continue
		}
		if len(r) < 3 {
			continue
		}
		eid := strings.TrimSpace(r[0])
		uid := strings.TrimSpace(r[1])
		if eid == "" || uid == "" {
			continue
		}
		if m.eventPoints[eid] == nil {
			m.eventPoints[eid] = map[string]int{}
		}
		m.eventPoints[eid][uid] = parseInt(r[2])
	}
	return nil
}

func loadCheckins(m *Memory, rows [][]string) error {
	// header: event_id,user_id,checked_in_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "event_id") {
			continue
		}
		if len(r) < 3 {
			continue
		}
		eid := strings.TrimSpace(r[0])
		uid := strings.TrimSpace(r[1])
		if eid == "" || uid == "" {
			continue
		}
		if m.checkins[eid] == nil {
			m.checkins[eid] = map[string]time.Time{}
		}
		m.checkins[eid][uid] = parseTime(r[2])
	}
	return nil
}

func dumpUsers(m *Memory) [][]string {
	rows := [][]string{{"id", "username", "role", "salt", "password_hash_b64", "created_at"}}
	ids := make([]string, 0, len(m.usersByID))
	for id := range m.usersByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		u := m.usersByID[id]
		hashB64 := ""
		if len(u.PasswordHash) > 0 {
			hashB64 = base64.RawStdEncoding.EncodeToString(u.PasswordHash)
		}
		rows = append(rows, []string{
			u.ID,
			u.Username,
			string(u.Role),
			u.Salt,
			hashB64,
			mustTime(u.CreatedAt),
		})
	}
	return rows
}

func dumpEvents(m *Memory) [][]string {
	rows := [][]string{{"id", "title", "description", "starts_at", "ends_at", "lat", "lng", "address", "tags", "checkin_radius_km", "created_by", "created_at"}}
	ids := make([]string, 0, len(m.eventsByID))
	for id := range m.eventsByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := m.eventsByID[id]
		rows = append(rows, []string{
			e.ID,
			e.Title,
			e.Description,
			mustTime(e.StartsAt),
			mustTime(e.EndsAt),
			strconv.FormatFloat(e.Lat, 'f', -1, 64),
			strconv.FormatFloat(e.Lng, 'f', -1, 64),
			e.Address,
			joinTags(e.Tags),
			strconv.FormatFloat(e.CheckinRadiusKm, 'f', -1, 64),
			e.CreatedBy,
			mustTime(e.CreatedAt),
		})
	}
	return rows
}

func dumpSessions(m *Memory) [][]string {
	rows := [][]string{{"id", "event_id", "title", "starts_at", "ends_at", "created_at"}}
	eventIDs := make([]string, 0, len(m.sessions))
	for eid := range m.sessions {
		eventIDs = append(eventIDs, eid)
	}
	sort.Strings(eventIDs)
	for _, eid := range eventIDs {
		list := append([]Session(nil), m.sessions[eid]...)
		sort.Slice(list, func(i, j int) bool { return list[i].StartsAt.Before(list[j].StartsAt) })
		for _, s := range list {
			rows = append(rows, []string{
				s.ID,
				s.EventID,
				s.Title,
				mustTime(s.StartsAt),
				mustTime(s.EndsAt),
				mustTime(s.CreatedAt),
			})
		}
	}
	return rows
}

func dumpParticipants(m *Memory) [][]string {
	rows := [][]string{{"event_id", "user_id", "joined_at"}}
	eventIDs := make([]string, 0, len(m.participants))
	for eid := range m.participants {
		eventIDs = append(eventIDs, eid)
	}
	sort.Strings(eventIDs)
	for _, eid := range eventIDs {
		userIDs := make([]string, 0, len(m.participants[eid]))
		for uid := range m.participants[eid] {
			userIDs = append(userIDs, uid)
		}
		sort.Strings(userIDs)
		for _, uid := range userIDs {
			p := m.participants[eid][uid]
			rows = append(rows, []string{eid, uid, mustTime(p.JoinedAt)})
		}
	}
	return rows
}

func dumpUserPoints(m *Memory) [][]string {
	rows := [][]string{{"user_id", "points"}}
	userIDs := make([]string, 0, len(m.userPoints))
	for uid := range m.userPoints {
		userIDs = append(userIDs, uid)
	}
	sort.Strings(userIDs)
	for _, uid := range userIDs {
		rows = append(rows, []string{uid, strconv.Itoa(m.userPoints[uid])})
	}
	return rows
}

func dumpEventPoints(m *Memory) [][]string {
	rows := [][]string{{"event_id", "user_id", "points"}}
	eventIDs := make([]string, 0, len(m.eventPoints))
	for eid := range m.eventPoints {
		eventIDs = append(eventIDs, eid)
	}
	sort.Strings(eventIDs)
	for _, eid := range eventIDs {
		userIDs := make([]string, 0, len(m.eventPoints[eid]))
		for uid := range m.eventPoints[eid] {
			userIDs = append(userIDs, uid)
		}
		sort.Strings(userIDs)
		for _, uid := range userIDs {
			rows = append(rows, []string{eid, uid, strconv.Itoa(m.eventPoints[eid][uid])})
		}
	}
	return rows
}

func dumpCheckins(m *Memory) [][]string {
	rows := [][]string{{"event_id", "user_id", "checked_in_at"}}
	eventIDs := make([]string, 0, len(m.checkins))
	for eid := range m.checkins {
		eventIDs = append(eventIDs, eid)
	}
	sort.Strings(eventIDs)
	for _, eid := range eventIDs {
		userIDs := make([]string, 0, len(m.checkins[eid]))
		for uid := range m.checkins[eid] {
			userIDs = append(userIDs, uid)
		}
		sort.Strings(userIDs)
		for _, uid := range userIDs {
			rows = append(rows, []string{eid, uid, mustTime(m.checkins[eid][uid])})
		}
	}
	return rows
}
