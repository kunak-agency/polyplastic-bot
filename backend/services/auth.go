package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Вход в кабинет (FEATURE_AUTH, итерация 24.10, п. 8): логин и пароль, хэш bcrypt cost 12,
// сессии в Postgres (domain.Session, 12 ч), не больше LoginMaxFailures неудач в минуту с IP.
// Флаг выключен — сервис есть, но HTTP-слой его не зовёт: маршрутов /api/auth/* нет.

// BcryptCost — стоимость bcrypt: ~250 мс на проверку, вход редкий.
const BcryptCost = 12

// Ограничение подбора пароля: не больше LoginMaxFailures неудачных входов за LoginWindow с IP.
const (
	LoginMaxFailures = 5
	LoginWindow      = time.Minute
)

// MinPasswordLen — пароль короче — 422.
const MinPasswordLen = 8

// Коды отказов входа (agency/system/api_contract_auth.md).
const (
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
	CodeTooManyAttempts    = "TOO_MANY_ATTEMPTS"
)

// Сообщения отказов: их видит пользователь кабинета.
const (
	msgInvalidCredentials = "Неверный логин или пароль"
	msgTooManyAttempts    = "Слишком много попыток входа. Подождите минуту"
	msgNoSession          = "Нужно войти в кабинет"
)

// ErrTooManyAttempts — лимит неудачных входов с IP исчерпан (HTTP 429).
var ErrTooManyAttempts = errors.New("too many login attempts")

// AuthService — вход, выход, проверка сессии.
type AuthService struct {
	repos   *repositories.Repos
	enabled bool
	now     func() time.Time
	limiter *loginLimiter
}

func newAuthService(r *repositories.Repos, enabled bool, now func() time.Time) *AuthService {
	return &AuthService{repos: r, enabled: enabled, now: now, limiter: newLoginLimiter(LoginMaxFailures, LoginWindow)}
}

// Enabled — FEATURE_AUTH включён.
func (s *AuthService) Enabled() bool { return s != nil && s.enabled }

// HashPassword — bcrypt cost 12.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), BcryptCost)
	return string(h), err
}

// checkPassword — пароль совпадает с хэшем.
func checkPassword(hash, pw string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash — сравнение при неизвестном логине, чтобы время ответа не выдавало, есть ли логин.
// Считается при первом входе, а не при старте: сиду и тестам он не нужен.
var dummyHash = sync.OnceValue(func() string {
	h, _ := bcrypt.GenerateFromPassword([]byte("korsik-dummy-password"), BcryptCost)
	return string(h)
})

// NormalizeLogin — логин без пробелов по краям, в нижнем регистре.
func NormalizeLogin(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Login — проверка пароля и новая сессия. Деактивированный — тот же отказ, что неверный пароль.
func (s *AuthService) Login(ctx context.Context, login, password, ip string) (*domain.User, *domain.Session, error) {
	if !s.repos.Enabled() {
		return nil, nil, repositories.ErrNoDatabase
	}
	now := s.now()
	if s.limiter.Blocked(ip, now) {
		return nil, nil, ErrTooManyAttempts
	}
	u, err := s.repos.Users.ByLogin(ctx, NormalizeLogin(login))
	if err != nil {
		return nil, nil, err
	}
	hash := dummyHash()
	if u != nil {
		hash = u.PasswordHash
	}
	ok := checkPassword(hash, password)
	if u == nil || !ok || !u.Active {
		s.limiter.Fail(ip, now)
		return nil, nil, errs.NewUnauthorizedError(msgInvalidCredentials).WithCode(CodeInvalidCredentials)
	}
	id, err := domain.NewSessionID()
	if err != nil {
		return nil, nil, err
	}
	sess := &domain.Session{ID: id, UserID: u.ID, ExpiresAt: now.Add(domain.SessionTTL), CreatedIP: ip, CreatedAt: now}
	if err := s.repos.Sessions.Create(ctx, sess); err != nil {
		return nil, nil, err
	}
	s.limiter.Reset(ip)
	return u, sess, nil
}

// Logout — удалить сессию; пустой id — ничего.
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" || !s.repos.Enabled() {
		return nil
	}
	return s.repos.Sessions.Delete(ctx, sessionID)
}

// UserBySession — пользователь живой сессии. Нет сессии, истекла, пользователь удалён или
// деактивирован — 401.
func (s *AuthService) UserBySession(ctx context.Context, sessionID string) (*domain.User, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	unauth := errs.NewUnauthorizedError(msgNoSession)
	if len(sessionID) != 64 {
		return nil, unauth
	}
	sess, err := s.repos.Sessions.ByID(ctx, sessionID, s.now())
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, unauth
	}
	u, err := s.repos.Users.ByID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.Active {
		return nil, unauth
	}
	return u, nil
}

// ── Текущий пользователь в контексте запроса ───────────────────────────────

type viewerKey struct{}

// WithViewer — контекст с текущим пользователем кабинета.
func WithViewer(ctx context.Context, u *domain.User) context.Context {
	return context.WithValue(ctx, viewerKey{}, u)
}

// ViewerFrom — текущий пользователь; nil — входа нет (FEATURE_AUTH выключен, бот, сид).
func ViewerFrom(ctx context.Context) *domain.User {
	u, _ := ctx.Value(viewerKey{}).(*domain.User)
	return u
}

// RecruiterScope — чьих новичков видит пользователь: id рекрутера для роли recruiter, nil —
// всех (hr_lead, вход выключен).
func RecruiterScope(u *domain.User) *uuid.UUID {
	if u != nil && u.Role == domain.RoleRecruiter {
		id := u.ID
		return &id
	}
	return nil
}

// ── Лимит неудачных входов ──────────────────────────────────────────────────

// loginLimiter — неудачные входы по IP в памяти процесса: скользящее окно.
type loginLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, fails: map[string][]time.Time{}}
}

// recent — неудачи ip в окне (под мьютексом).
func (l *loginLimiter) recent(ip string, now time.Time) []time.Time {
	list := l.fails[ip]
	keep := list[:0]
	for _, t := range list {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(l.fails, ip)
		return nil
	}
	l.fails[ip] = keep
	return keep
}

// Blocked — с ip уже max неудач за окно.
func (l *loginLimiter) Blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip, now)) >= l.max
}

// Fail — ещё одна неудача. Заодно чистит устаревшие записи других IP, чтобы карта не росла.
func (l *loginLimiter) Fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.fails) > 1000 {
		for k := range l.fails {
			l.recent(k, now)
		}
	}
	l.fails[ip] = append(l.recent(ip, now), now)
}

// Reset — удачный вход снимает счёт.
func (l *loginLimiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}
