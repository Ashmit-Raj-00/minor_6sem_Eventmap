package store

import "strings"

func SeedDefaultAdmin(m *Memory, email, password string) {
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return
	}
	_, _ = m.CreateUser(email, password, RoleAdmin)
}

