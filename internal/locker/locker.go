package locker

import (
	"sync"

	"github.com/google/uuid"
)

// UserLocker предоставляет per-user мьютексы для сериализации
// конкурентных операций на один баланс в рамках одного инстанса.
//
// Это дополнительный уровень защиты поверх PostgreSQL SELECT ... FOR UPDATE.
// Снижает нагрузку на БД, сериализуя запросы до обращения к транзакции.
//
// Ограничение: работает только в рамках одного процесса.
// При горизонтальном масштабировании PostgreSQL FOR UPDATE остаётся основной защитой.
type UserLocker struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*userLock
}

type userLock struct {
	mu       sync.Mutex
	refCount int
}

func New() *UserLocker {
	return &UserLocker{
		locks: make(map[uuid.UUID]*userLock),
	}
}

func (l *UserLocker) Lock(userID uuid.UUID) {
	l.mu.Lock()
	ul, ok := l.locks[userID]
	if !ok {
		ul = &userLock{}
		l.locks[userID] = ul
	}
	ul.refCount++
	l.mu.Unlock()

	ul.mu.Lock()
}

func (l *UserLocker) Unlock(userID uuid.UUID) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ul, ok := l.locks[userID]
	if !ok {
		return
	}

	ul.refCount--
	if ul.refCount == 0 {
		delete(l.locks, userID)
	}

	ul.mu.Unlock()
}
