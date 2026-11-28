package auth

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// Limiter bloquea temporalmente una IP tras varios intentos fallidos (inicio de sesión y código de
// configuración). En IPv6 cuenta por /64, para que no baste con cambiar de dirección.
type Limiter struct {
	mu       sync.Mutex
	m        map[string]*attempts
	maxFails int
	window   time.Duration
	lockFor  time.Duration
	now      func() time.Time
}

type attempts struct {
	fails       int
	first       time.Time
	lockedUntil time.Time
}

// NewLimiter: maxFails fallos dentro de window bloquean durante lockFor.
func NewLimiter(maxFails int, window, lockFor time.Duration) *Limiter {
	return &Limiter{m: map[string]*attempts{}, maxFails: maxFails, window: window, lockFor: lockFor, now: time.Now}
}

func limiterKey(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	ip = ip.Unmap().WithZone("")
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}

// Blocked indica si la IP está bloqueada y hasta cuándo.
func (l *Limiter) Blocked(remote string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[limiterKey(remote)]
	if !ok {
		return false, 0
	}
	if d := a.lockedUntil.Sub(l.now()); d > 0 {
		return true, d
	}
	return false, 0
}

// Fail apunta un intento fallido.
func (l *Limiter) Fail(remote string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > 10000 {
		for k, v := range l.m {
			if now.Sub(v.first) > l.window && now.After(v.lockedUntil) {
				delete(l.m, k)
			}
		}
	}
	k := limiterKey(remote)
	a, ok := l.m[k]
	if !ok || now.Sub(a.first) > l.window {
		a = &attempts{first: now, lockedUntil: a.lockedUntilOrZero()}
		l.m[k] = a
	}
	a.fails++
	if a.fails >= l.maxFails {
		a.lockedUntil = now.Add(l.lockFor)
		a.fails = 0
		a.first = now
	}
}

func (a *attempts) lockedUntilOrZero() time.Time {
	if a == nil {
		return time.Time{}
	}
	return a.lockedUntil
}

// Success borra los fallos de la IP.
func (l *Limiter) Success(remote string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, limiterKey(remote))
}
