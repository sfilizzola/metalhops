package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalid = errors.New("idempotency: invalid request")
var ErrReused = errors.New("idempotency: key reused")

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	operationPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
)

type Request struct {
	UserID    string
	Operation string
	Key       string
	Command   []byte
}

type Result struct {
	Status int
	Body   []byte
}

func Execute(ctx context.Context, tx pgx.Tx, now time.Time, req Request, fn func(context.Context) (Result, error)) (Result, error) {
	if !uuidPattern.MatchString(req.UserID) || !operationPattern.MatchString(req.Operation) || !validKey(req.Key) || len(req.Command) == 0 || len(req.Command) > 65536 {
		return Result{}, ErrInvalid
	}
	sum := sha256.Sum256(req.Command)
	hash := hex.EncodeToString(sum[:])

	if _, err := tx.Exec(ctx, `
		delete from idempotency_records
		where ctid in (
			select ctid from idempotency_records
			where expires_at <= $1
			order by expires_at
			limit 100
			for update skip locked
		)`, now); err != nil {
		return Result{}, err
	}
	lockKey := req.UserID + "\x1f" + req.Operation + "\x1f" + req.Key
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return Result{}, err
	}

	var storedHash string
	var status int
	var body []byte
	var expires time.Time
	err := tx.QueryRow(ctx, `
		select request_hash, response_status, response_body, expires_at
		from idempotency_records
		where user_id = $1 and operation = $2 and key = $3`,
		req.UserID, req.Operation, req.Key).Scan(&storedHash, &status, &body, &expires)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	if err == nil {
		if expires.After(now) {
			if storedHash == hash {
				return Result{Status: status, Body: body}, nil
			}
			return Result{}, ErrReused
		}
		if _, err := tx.Exec(ctx, `
			delete from idempotency_records
			where user_id = $1 and operation = $2 and key = $3`,
			req.UserID, req.Operation, req.Key); err != nil {
			return Result{}, err
		}
	}

	result, err := fn(ctx)
	if err != nil {
		return Result{}, err
	}
	if result.Status < 200 || result.Status > 299 {
		return result, nil
	}
	if !json.Valid(result.Body) {
		return Result{}, ErrInvalid
	}
	var stored []byte
	if err := tx.QueryRow(ctx, `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values ($1, $2, $3, $4, $5, $6, $7)
		returning response_body`,
		req.UserID, req.Operation, req.Key, hash, result.Status, result.Body, now.Add(24*time.Hour),
	).Scan(&stored); err != nil {
		return Result{}, err
	}
	return Result{Status: result.Status, Body: stored}, nil
}

func validKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7E {
			return false
		}
	}
	return true
}
