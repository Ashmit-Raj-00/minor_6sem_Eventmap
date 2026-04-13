package store

import (
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

// CSV persistence is intended for local/dev. For production, replace this with a real DB.

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
		{"participants.csv", func(rows [][]string) error { return loadParticipants(m, rows) }},
		{"tasks.csv", func(rows [][]string) error { return loadTasks(m, rows) }},
		{"submissions.csv", func(rows [][]string) error { return loadSubmissions(m, rows) }},
		{"user_xp.csv", func(rows [][]string) error { return loadUserXP(m, rows) }},
		{"event_xp.csv", func(rows [][]string) error { return loadEventXP(m, rows) }},
		{"xp_logs.csv", func(rows [][]string) error { return loadXPLogs(m, rows) }},
		{"chat.csv", func(rows [][]string) error { return loadChat(m, rows) }},
		{"queries.csv", func(rows [][]string) error { return loadQueries(m, rows) }},
		{"notifications.csv", func(rows [][]string) error { return loadNotifications(m, rows) }},
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
	if err := writeCSVAtomic(dir, "participants.csv", dumpParticipants(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "tasks.csv", dumpTasks(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "submissions.csv", dumpSubmissions(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "user_xp.csv", dumpUserXP(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "event_xp.csv", dumpEventXP(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "xp_logs.csv", dumpXPLogs(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "chat.csv", dumpChat(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "queries.csv", dumpQueries(m)); err != nil {
		return err
	}
	if err := writeCSVAtomic(dir, "notifications.csv", dumpNotifications(m)); err != nil {
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

func mustTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

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

func parseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes" || s == "y"
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
	// header: id,name,email,photo_url,role,created_at
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
		name := strings.TrimSpace(r[1])
		email := strings.TrimSpace(strings.ToLower(r[2]))
		photoURL := strings.TrimSpace(r[3])
		role := Role(strings.TrimSpace(r[4]))
		createdAt := parseTime(r[5])
		if id == "" || email == "" {
			continue
		}
		if role != RoleOperator && role != RoleCommander {
			role = RoleOperator
		}
		u := User{
			ID:        id,
			Name:      name,
			Email:     email,
			PhotoURL:  photoURL,
			Role:      role,
			CreatedAt: createdAt,
		}
		m.usersByID[u.ID] = u
		m.usersByEmail[email] = u.ID
	}
	return nil
}

func dumpUsers(m *Memory) [][]string {
	out := [][]string{{"id", "name", "email", "photo_url", "role", "created_at"}}
	ids := make([]string, 0, len(m.usersByID))
	for id := range m.usersByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		u := m.usersByID[id]
		out = append(out, []string{
			u.ID,
			u.Name,
			u.Email,
			u.PhotoURL,
			string(u.Role),
			mustTime(u.CreatedAt),
		})
	}
	return out
}

func loadEvents(m *Memory, rows [][]string) error {
	// header: id,title,description,goal,goal_target,goal_unit,instructions,visibility,status,starts_at,ends_at,lat,lng,address,tags,created_by,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 17 {
			continue
		}
		e := Event{
			ID:           strings.TrimSpace(r[0]),
			Title:        strings.TrimSpace(r[1]),
			Description:  r[2],
			Goal:         strings.TrimSpace(r[3]),
			GoalTarget:   parseInt(r[4]),
			GoalUnit:     strings.TrimSpace(r[5]),
			Instructions: r[6],
			Visibility:   EventVisibility(strings.TrimSpace(r[7])),
			Status:       EventStatus(strings.TrimSpace(r[8])),
			StartsAt:     parseTime(r[9]),
			EndsAt:       parseTime(r[10]),
			Lat:          parseFloat(r[11]),
			Lng:          parseFloat(r[12]),
			Address:      r[13],
			Tags:         splitTags(r[14]),
			CreatedBy:    strings.TrimSpace(r[15]),
			CreatedAt:    parseTime(r[16]),
		}
		if e.ID == "" {
			continue
		}
		if e.Visibility != EventPublic && e.Visibility != EventPrivate {
			e.Visibility = EventPublic
		}
		if e.Status != EventDraft && e.Status != EventActive && e.Status != EventCompleted && e.Status != EventArchived {
			e.Status = EventActive
		}
		m.eventsByID[e.ID] = e
	}
	return nil
}

func dumpEvents(m *Memory) [][]string {
	out := [][]string{{"id", "title", "description", "goal", "goal_target", "goal_unit", "instructions", "visibility", "status", "starts_at", "ends_at", "lat", "lng", "address", "tags", "created_by", "created_at"}}
	ids := make([]string, 0, len(m.eventsByID))
	for id := range m.eventsByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := m.eventsByID[id]
		out = append(out, []string{
			e.ID,
			e.Title,
			e.Description,
			e.Goal,
			strconv.Itoa(e.GoalTarget),
			e.GoalUnit,
			e.Instructions,
			string(e.Visibility),
			string(e.Status),
			mustTime(e.StartsAt),
			mustTime(e.EndsAt),
			strconv.FormatFloat(e.Lat, 'f', 6, 64),
			strconv.FormatFloat(e.Lng, 'f', 6, 64),
			e.Address,
			joinTags(e.Tags),
			e.CreatedBy,
			mustTime(e.CreatedAt),
		})
	}
	return out
}

func loadParticipants(m *Memory, rows [][]string) error {
	// header: user_id,event_id,joined_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "user_id") {
			continue
		}
		if len(r) < 3 {
			continue
		}
		p := Participant{
			UserID:   strings.TrimSpace(r[0]),
			EventID:  strings.TrimSpace(r[1]),
			JoinedAt: parseTime(r[2]),
		}
		if p.UserID == "" || p.EventID == "" {
			continue
		}
		if m.participants[p.EventID] == nil {
			m.participants[p.EventID] = map[string]Participant{}
		}
		m.participants[p.EventID][p.UserID] = p
	}
	return nil
}

func dumpParticipants(m *Memory) [][]string {
	out := [][]string{{"user_id", "event_id", "joined_at"}}
	type key struct{ e, u string }
	var keys []key
	for e, mm := range m.participants {
		for u := range mm {
			keys = append(keys, key{e: e, u: u})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].e == keys[j].e {
			return keys[i].u < keys[j].u
		}
		return keys[i].e < keys[j].e
	})
	for _, k := range keys {
		p := m.participants[k.e][k.u]
		out = append(out, []string{p.UserID, p.EventID, mustTime(p.JoinedAt)})
	}
	return out
}

func loadTasks(m *Memory, rows [][]string) error {
	// header: id,event_id,title,description,type,priority,difficulty,deadline,has_location,lat,lng,assigned_to,status,started_by,started_at,submitted_at,completed_at,last_feedback,created_by,created_at,updated_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 21 {
			continue
		}
		t := Task{
			ID:           strings.TrimSpace(r[0]),
			EventID:      strings.TrimSpace(r[1]),
			Title:        strings.TrimSpace(r[2]),
			Description:  r[3],
			Type:         TaskType(strings.TrimSpace(r[4])),
			Priority:     TaskPriority(strings.TrimSpace(r[5])),
			Difficulty:   parseInt(r[6]),
			Deadline:     parseTime(r[7]),
			HasLocation:  parseBool(r[8]),
			Lat:          parseFloat(r[9]),
			Lng:          parseFloat(r[10]),
			AssignedTo:   strings.TrimSpace(r[11]),
			Status:       TaskStatus(strings.TrimSpace(r[12])),
			StartedBy:    strings.TrimSpace(r[13]),
			StartedAt:    parseTime(r[14]),
			SubmittedAt:  parseTime(r[15]),
			CompletedAt:  parseTime(r[16]),
			LastFeedback: r[17],
			CreatedBy:    strings.TrimSpace(r[18]),
			CreatedAt:    parseTime(r[19]),
			UpdatedAt:    parseTime(r[20]),
		}
		if t.ID == "" || t.EventID == "" {
			continue
		}
		m.tasksByID[t.ID] = t
		m.tasksByEvent[t.EventID] = append(m.tasksByEvent[t.EventID], t.ID)
	}
	return nil
}

func dumpTasks(m *Memory) [][]string {
	out := [][]string{{"id", "event_id", "title", "description", "type", "priority", "difficulty", "deadline", "has_location", "lat", "lng", "assigned_to", "status", "started_by", "started_at", "submitted_at", "completed_at", "last_feedback", "created_by", "created_at", "updated_at"}}
	ids := make([]string, 0, len(m.tasksByID))
	for id := range m.tasksByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t := m.tasksByID[id]
		out = append(out, []string{
			t.ID,
			t.EventID,
			t.Title,
			t.Description,
			string(t.Type),
			string(t.Priority),
			strconv.Itoa(t.Difficulty),
			mustTime(t.Deadline),
			strconv.FormatBool(t.HasLocation),
			strconv.FormatFloat(t.Lat, 'f', 6, 64),
			strconv.FormatFloat(t.Lng, 'f', 6, 64),
			t.AssignedTo,
			string(t.Status),
			t.StartedBy,
			mustTime(t.StartedAt),
			mustTime(t.SubmittedAt),
			mustTime(t.CompletedAt),
			t.LastFeedback,
			t.CreatedBy,
			mustTime(t.CreatedAt),
			mustTime(t.UpdatedAt),
		})
	}
	return out
}

func loadSubmissions(m *Memory, rows [][]string) error {
	// header: id,task_id,event_id,operator_id,image_url,comment,has_geo,lat,lng,status,quality,feedback,reviewed_by,reviewed_at,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 15 {
			continue
		}
		s := Submission{
			ID:         strings.TrimSpace(r[0]),
			TaskID:     strings.TrimSpace(r[1]),
			EventID:    strings.TrimSpace(r[2]),
			OperatorID: strings.TrimSpace(r[3]),
			ImageURL:   strings.TrimSpace(r[4]),
			Comment:    r[5],
			HasGeo:     parseBool(r[6]),
			Lat:        parseFloat(r[7]),
			Lng:        parseFloat(r[8]),
			Status:     SubmissionStatus(strings.TrimSpace(r[9])),
			Quality:    parseInt(r[10]),
			Feedback:   r[11],
			ReviewedBy: strings.TrimSpace(r[12]),
			ReviewedAt: parseTime(r[13]),
			CreatedAt:  parseTime(r[14]),
		}
		if s.ID == "" || s.TaskID == "" {
			continue
		}
		m.submissionsByID[s.ID] = s
		m.submissionsByTask[s.TaskID] = append(m.submissionsByTask[s.TaskID], s.ID)
	}
	return nil
}

func dumpSubmissions(m *Memory) [][]string {
	out := [][]string{{"id", "task_id", "event_id", "operator_id", "image_url", "comment", "has_geo", "lat", "lng", "status", "quality", "feedback", "reviewed_by", "reviewed_at", "created_at"}}
	ids := make([]string, 0, len(m.submissionsByID))
	for id := range m.submissionsByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := m.submissionsByID[id]
		out = append(out, []string{
			s.ID,
			s.TaskID,
			s.EventID,
			s.OperatorID,
			s.ImageURL,
			s.Comment,
			strconv.FormatBool(s.HasGeo),
			strconv.FormatFloat(s.Lat, 'f', 6, 64),
			strconv.FormatFloat(s.Lng, 'f', 6, 64),
			string(s.Status),
			strconv.Itoa(s.Quality),
			s.Feedback,
			s.ReviewedBy,
			mustTime(s.ReviewedAt),
			mustTime(s.CreatedAt),
		})
	}
	return out
}

func loadUserXP(m *Memory, rows [][]string) error {
	// header: user_id,xp
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "user_id") {
			continue
		}
		if len(r) < 2 {
			continue
		}
		userID := strings.TrimSpace(r[0])
		if userID == "" {
			continue
		}
		m.userXP[userID] = parseInt(r[1])
	}
	return nil
}

func dumpUserXP(m *Memory) [][]string {
	out := [][]string{{"user_id", "xp"}}
	ids := make([]string, 0, len(m.userXP))
	for id := range m.userXP {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, []string{id, strconv.Itoa(m.userXP[id])})
	}
	return out
}

func loadEventXP(m *Memory, rows [][]string) error {
	// header: event_id,user_id,xp
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
		if m.eventXP[eventID] == nil {
			m.eventXP[eventID] = map[string]int{}
		}
		m.eventXP[eventID][userID] = parseInt(r[2])
	}
	return nil
}

func dumpEventXP(m *Memory) [][]string {
	out := [][]string{{"event_id", "user_id", "xp"}}
	type key struct{ e, u string }
	var keys []key
	for e, mm := range m.eventXP {
		for u := range mm {
			keys = append(keys, key{e: e, u: u})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].e == keys[j].e {
			return keys[i].u < keys[j].u
		}
		return keys[i].e < keys[j].e
	})
	for _, k := range keys {
		out = append(out, []string{k.e, k.u, strconv.Itoa(m.eventXP[k.e][k.u])})
	}
	return out
}

func loadXPLogs(m *Memory, rows [][]string) error {
	// header: id,user_id,event_id,task_id,amount,reason,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 7 {
			continue
		}
		x := XPLog{
			ID:        strings.TrimSpace(r[0]),
			UserID:    strings.TrimSpace(r[1]),
			EventID:   strings.TrimSpace(r[2]),
			TaskID:    strings.TrimSpace(r[3]),
			Amount:    parseInt(r[4]),
			Reason:    r[5],
			CreatedAt: parseTime(r[6]),
		}
		if x.ID == "" || x.UserID == "" {
			continue
		}
		m.xpLogs = append(m.xpLogs, x)
	}
	return nil
}

func dumpXPLogs(m *Memory) [][]string {
	out := [][]string{{"id", "user_id", "event_id", "task_id", "amount", "reason", "created_at"}}
	for _, x := range m.xpLogs {
		out = append(out, []string{x.ID, x.UserID, x.EventID, x.TaskID, strconv.Itoa(x.Amount), x.Reason, mustTime(x.CreatedAt)})
	}
	return out
}

func loadChat(m *Memory, rows [][]string) error {
	// header: id,event_id,user_id,body,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 5 {
			continue
		}
		msg := ChatMessage{
			ID:        strings.TrimSpace(r[0]),
			EventID:   strings.TrimSpace(r[1]),
			UserID:    strings.TrimSpace(r[2]),
			Body:      r[3],
			CreatedAt: parseTime(r[4]),
		}
		if msg.ID == "" || msg.EventID == "" {
			continue
		}
		m.chatByEv[msg.EventID] = append(m.chatByEv[msg.EventID], msg)
	}
	return nil
}

func dumpChat(m *Memory) [][]string {
	out := [][]string{{"id", "event_id", "user_id", "body", "created_at"}}
	var evs []string
	for ev := range m.chatByEv {
		evs = append(evs, ev)
	}
	sort.Strings(evs)
	for _, ev := range evs {
		for _, msg := range m.chatByEv[ev] {
			out = append(out, []string{msg.ID, msg.EventID, msg.UserID, msg.Body, mustTime(msg.CreatedAt)})
		}
	}
	return out
}

func loadQueries(m *Memory, rows [][]string) error {
	// header: id,event_id,from_user_id,body,status,answer,answered_by,answered_at,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 9 {
			continue
		}
		q := Query{
			ID:         strings.TrimSpace(r[0]),
			EventID:    strings.TrimSpace(r[1]),
			FromUserID: strings.TrimSpace(r[2]),
			Body:       r[3],
			Status:     QueryStatus(strings.TrimSpace(r[4])),
			Answer:     r[5],
			AnsweredBy: strings.TrimSpace(r[6]),
			AnsweredAt: parseTime(r[7]),
			CreatedAt:  parseTime(r[8]),
		}
		if q.ID == "" || q.EventID == "" {
			continue
		}
		m.queriesByEv[q.EventID] = append(m.queriesByEv[q.EventID], q)
	}
	return nil
}

func dumpQueries(m *Memory) [][]string {
	out := [][]string{{"id", "event_id", "from_user_id", "body", "status", "answer", "answered_by", "answered_at", "created_at"}}
	var evs []string
	for ev := range m.queriesByEv {
		evs = append(evs, ev)
	}
	sort.Strings(evs)
	for _, ev := range evs {
		for _, q := range m.queriesByEv[ev] {
			out = append(out, []string{q.ID, q.EventID, q.FromUserID, q.Body, string(q.Status), q.Answer, q.AnsweredBy, mustTime(q.AnsweredAt), mustTime(q.CreatedAt)})
		}
	}
	return out
}

func loadNotifications(m *Memory, rows [][]string) error {
	// header: id,user_id,kind,read_at,created_at
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range rows {
		if i == 0 && len(r) > 0 && strings.EqualFold(strings.TrimSpace(r[0]), "id") {
			continue
		}
		if len(r) < 5 {
			continue
		}
		n := Notification{
			ID:        strings.TrimSpace(r[0]),
			UserID:    strings.TrimSpace(r[1]),
			Kind:      strings.TrimSpace(r[2]),
			ReadAt:    parseTime(r[3]),
			CreatedAt: parseTime(r[4]),
		}
		if n.ID == "" || n.UserID == "" {
			continue
		}
		m.notificationsByUser[n.UserID] = append(m.notificationsByUser[n.UserID], n)
	}
	// Ensure newest-first ordering.
	for userID := range m.notificationsByUser {
		ns := m.notificationsByUser[userID]
		sort.Slice(ns, func(i, j int) bool { return ns[i].CreatedAt.After(ns[j].CreatedAt) })
		m.notificationsByUser[userID] = ns
	}
	return nil
}

func dumpNotifications(m *Memory) [][]string {
	out := [][]string{{"id", "user_id", "kind", "read_at", "created_at"}}
	var users []string
	for u := range m.notificationsByUser {
		users = append(users, u)
	}
	sort.Strings(users)
	for _, u := range users {
		for _, n := range m.notificationsByUser[u] {
			out = append(out, []string{n.ID, n.UserID, n.Kind, mustTime(n.ReadAt), mustTime(n.CreatedAt)})
		}
	}
	return out
}

