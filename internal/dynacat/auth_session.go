package dynacat

import (
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const OIDC_SESSION_COOKIE_NAME = "oidc_session"
const OIDC_SESSION_VALID_PERIOD = 14 * 24 * time.Hour

type oidcSession struct {
	Username  string
	Groups    []string
	Token     *oauth2.Token
	CreatedAt time.Time
}

type sessionStore struct {
	sessions sync.Map // map[sessionID]*oidcSession
}

func newSessionStore() *sessionStore {
	return &sessionStore{}
}

func (s *sessionStore) set(id string, session *oidcSession) {
	s.sessions.Store(id, session)
}

func (s *sessionStore) get(id string) (*oidcSession, bool) {
	v, ok := s.sessions.Load(id)
	if !ok {
		return nil, false
	}
	sess, ok := v.(*oidcSession)
	return sess, ok
}

func (s *sessionStore) delete(id string) {
	s.sessions.Delete(id)
}
