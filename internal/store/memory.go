package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"eventmap/internal/geo"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrForbidden     = errors.New("forbidden")
)

type MemoryConfig struct {
	PasswordIterations int
}

type Memory struct {
	mu sync.RWMutex

	passwordIterations int

	usersByID    map[string]User
	usersByEmail map[string]string

	eventsByID map[string]Event
	sessions   map[string][]Session
	participants map[string]map[string]Participant // eventID -> userID -> participant
}

func NewMemory(cfg MemoryConfig) *Memory {
	if cfg.PasswordIterations <= 0 {
		cfg.PasswordIterations = 120_000
	}
	return &Memory{
		passwordIterations: cfg.PasswordIterations,
		usersByID:          map[string]User{},
		usersByEmail:       map[string]string{},
		eventsByID:         map[string]Event{},
		sessions:           map[string][]Session{},
		participants:       map[string]map[string]Participant{},
	}
}

func (m *Memory) CreateUser(email, password string, role Role) (User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return User{}, errors.New("email and password required")
	}
	if role != RoleAdmin && role != RoleOrganizer && role != RoleAttendee {
		return User{}, errors.New("invalid role")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.usersByEmail[email]; ok {
		return User{}, ErrAlreadyExists
	}

	salt := randomID(16)
	u := User{
		ID:           randomID(18),
		Email:        email,
		Role:         role,
		Salt:         salt,
		PasswordHash: hashPassword(password, salt, m.passwordIterations),
		CreatedAt:    time.Now(),
	}
	m.usersByID[u.ID] = u
	m.usersByEmail[email] = u.ID
	return u, nil
}

func (m *Memory) GetUserByEmail(email string) (User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.usersByEmail[email]
	if !ok {
		return User{}, ErrNotFound
	}
	u, ok := m.usersByID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) GetUserByID(id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.usersByID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) VerifyPassword(email, password string) (User, error) {
	u, err := m.GetUserByEmail(email)
	if err != nil {
		return User{}, err
	}
	expected := hashPassword(password, u.Salt, m.passwordIterations)
	if !equalBytes(expected, u.PasswordHash) {
		return User{}, ErrForbidden
	}
	return u, nil
}

func (m *Memory) CreateEvent(e Event) (Event, error) {
	if strings.TrimSpace(e.Title) == "" {
		return Event{}, errors.New("title required")
	}
	if e.CreatedBy == "" {
		return Event{}, errors.New("created_by required")
	}
	e.ID = randomID(18)
	e.CreatedAt = time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsByID[e.ID] = e
	return e, nil
}

func (m *Memory) ListEvents() []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Event, 0, len(m.eventsByID))
	for _, e := range m.eventsByID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.Before(out[j].StartsAt) })
	return out
}

func (m *Memory) NearbyEvents(lat, lng, radiusKm float64) []Event {
	all := m.ListEvents()
	if radiusKm <= 0 {
		return all
	}
	out := make([]Event, 0, len(all))
	for _, e := range all {
		if geo.DistanceKm(lat, lng, e.Lat, e.Lng) <= radiusKm {
			out = append(out, e)
		}
	}
	return out
}

func (m *Memory) GetEvent(id string) (Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.eventsByID[id]
	if !ok {
		return Event{}, ErrNotFound
	}
	return e, nil
}

func (m *Memory) CreateSession(s Session) (Session, error) {
	if s.EventID == "" {
		return Session{}, errors.New("event_id required")
	}
	if strings.TrimSpace(s.Title) == "" {
		return Session{}, errors.New("title required")
	}
	if _, err := m.GetEvent(s.EventID); err != nil {
		return Session{}, err
	}

	s.ID = randomID(18)
	s.CreatedAt = time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.EventID] = append(m.sessions[s.EventID], s)
	sort.Slice(m.sessions[s.EventID], func(i, j int) bool {
		return m.sessions[s.EventID][i].StartsAt.Before(m.sessions[s.EventID][j].StartsAt)
	})
	return s, nil
}

func (m *Memory) ListSessions(eventID string) ([]Session, error) {
	if _, err := m.GetEvent(eventID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.sessions[eventID]
	out := make([]Session, len(s))
	copy(out, s)
	return out, nil
}

func (m *Memory) JoinEvent(eventID, userID string) (Participant, error) {
	if eventID == "" || userID == "" {
		return Participant{}, errors.New("event_id and user_id required")
	}
	if _, err := m.GetEvent(eventID); err != nil {
		return Participant{}, err
	}
	if _, err := m.GetUserByID(userID); err != nil {
		return Participant{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.participants[eventID] == nil {
		m.participants[eventID] = map[string]Participant{}
	}
	if _, ok := m.participants[eventID][userID]; ok {
		return Participant{}, ErrAlreadyExists
	}
	p := Participant{EventID: eventID, UserID: userID, JoinedAt: time.Now()}
	m.participants[eventID][userID] = p
	return p, nil
}

func (m *Memory) ListParticipants(eventID string) ([]Participant, error) {
	if _, err := m.GetEvent(eventID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	mm := m.participants[eventID]
	out := make([]Participant, 0, len(mm))
	for _, p := range mm {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JoinedAt.Before(out[j].JoinedAt) })
	return out, nil
}

func SeedDefaultUsers(m *Memory, cfg interface{ DefaultAdminEmail, DefaultAdminPassword string }) {
	email := strings.TrimSpace(cfg.DefaultAdminEmail)
	pass := strings.TrimSpace(cfg.DefaultAdminPassword)
	if email == "" || pass == "" {
		return
	}
	_, _ = m.CreateUser(email, pass, RoleAdmin)
}

func randomID(nbytes int) string {
	b := make([]byte, nbytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashPassword(password, salt string, iterations int) []byte {
	if iterations <= 1 {
		iterations = 1
	}
	sum := sha256.Sum256([]byte(salt + ":" + password))
	out := sum[:]
	for i := 0; i < iterations; i++ {
		n := sha256.Sum256(out)
		out = n[:]
	}
	final := make([]byte, len(out))
	copy(final, out)
	return final
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

