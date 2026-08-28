package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	redis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	sessionEvictionSubjectPrefix = "tc9.gateway.session.evict."
	evictionWorkerCount          = 32
	evictionStreamMaxLength      = 4096
	evictionAcknowledgeTimeout   = 4 * time.Second
	heartbeatCommandTimeout      = time.Second
	sessionTeardownTimeout       = 3 * time.Second
	heartbeatExpirySafetyMargin  = time.Second
)

var (
	ErrSessionOwnershipSuperseded  = errors.New("session ownership was superseded by another login")
	ErrSessionOwnershipUnavailable = errors.New("session ownership is unavailable on this gateway")
	errRedisOwnershipEpochChanged  = errors.New("Redis ownership epoch changed")

	// The owner key and the previous gateway's stream use the same realm hash
	// tag. The compare, replacement and durable eviction event therefore remain
	// atomic on both standalone Redis and Redis Cluster.
	prepareSessionOwnershipScript = redis.NewScript(`
local epoch = redis.call('GET', KEYS[1]) or ''
if epoch ~= ARGV[1] then
  return {-1, epoch}
end
local current = redis.call('GET', KEYS[2]) or ''
if current ~= ARGV[2] then
  return {0, current}
end
if current ~= '' and current ~= ARGV[3] then
  redis.call('XADD', KEYS[3], 'MAXLEN', '~', ARGV[6], '*',
	'token', ARGV[4], 'ack_key', ARGV[5])
end
return {1, current}
`)

	commitSessionOwnershipScript = redis.NewScript(`
local epoch = redis.call('GET', KEYS[1]) or ''
if epoch ~= ARGV[1] then
  return -1
end
if redis.call('GET', KEYS[2]) ~= ARGV[2] then
  return -2
end
local current = redis.call('GET', KEYS[3]) or ''
if current ~= '' and current ~= ARGV[3] and current ~= ARGV[4] then
  return 0
end
redis.call('SET', KEYS[3], ARGV[4])
redis.call('DEL', KEYS[2])
return 1
`)

	releasePendingClaimScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

	releaseSessionOwnershipScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

	initializeRedisEpochScript = redis.NewScript(`
local epoch = redis.call('GET', KEYS[1])
if epoch then
  return epoch
end
redis.call('SET', KEYS[1], ARGV[1])
redis.call('SET', KEYS[2], '1', 'PX', ARGV[2])
return ARGV[1]
`)
)

type sessionEvictionRequest struct {
	Token  string `json:"token"`
	AckKey string `json:"ack_key"`
}

type processedEviction struct {
	done      chan struct{}
	confirmed bool
	expiresAt time.Time
}

// SessionOwnershipService stores durable, token-fenced ownership in Redis.
// Takeovers are delivered through both NATS (fast path) and a Redis Stream
// (durable path). Redis traffic while players are idle is one heartbeat per
// gateway, independent of the number of connected players.
type SessionOwnershipService struct {
	redis       redis.UniversalClient
	nats        *nats.Conn
	logger      *zerolog.Logger
	gatewayID   string
	realmID     uint32
	livenessTTL time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.RWMutex
	sessions   map[string]func(context.Context) bool
	workers    chan struct{}
	wg         sync.WaitGroup
	healthy    atomic.Bool
	heartbeat  atomic.Int64
	sub        *nats.Subscription
	dispatchMu sync.Mutex
	redisEpoch string
	failOnce   sync.Once

	evictionMu         sync.Mutex
	processedEvictions map[string]*processedEviction
}

func NewSessionOwnershipService(rdb redis.UniversalClient, nc *nats.Conn, logger *zerolog.Logger, gatewayID string, realmID uint32, livenessTTL time.Duration) *SessionOwnershipService {
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionOwnershipService{
		redis: rdb, nats: nc, logger: logger, gatewayID: gatewayID, realmID: realmID,
		livenessTTL: livenessTTL, ctx: ctx, cancel: cancel,
		sessions: make(map[string]func(context.Context) bool), workers: make(chan struct{}, evictionWorkerCount),
		processedEvictions: make(map[string]*processedEviction),
	}
}

func (s *SessionOwnershipService) Listen() error {
	epoch, err := s.initializeRedisEpoch(s.ctx)
	if err != nil {
		return fmt.Errorf("initialize Redis ownership epoch: %w", err)
	}
	s.redisEpoch = epoch
	if err := s.writeHeartbeat(s.ctx); err != nil {
		return fmt.Errorf("write initial gateway session heartbeat: %w", err)
	}
	s.heartbeat.Store(time.Now().UnixNano())
	sub, err := s.nats.Subscribe(sessionEvictionSubjectPrefix+s.gatewayID, func(message *nats.Msg) {
		var request sessionEvictionRequest
		if err := json.Unmarshal(message.Data, &request); err != nil {
			s.logger.Error().Err(err).Msg("can't decode session eviction request")
			return
		}
		// Do not block the NATS subscription when all workers are busy. The
		// durable Redis stream remains the retry path.
		_ = s.tryDispatchEviction(request)
	})
	if err != nil {
		return err
	}
	s.sub = sub
	if err = s.nats.Flush(); err != nil {
		return err
	}
	s.healthy.Store(true)
	s.wg.Add(3)
	go s.runHeartbeat()
	go s.runHeartbeatWatchdog()
	go s.consumeEvictionStream()
	return nil
}

func (s *SessionOwnershipService) Close() {
	s.dispatchMu.Lock()
	s.healthy.Store(false)
	if s.sub != nil {
		_ = s.sub.Unsubscribe()
	}
	s.dispatchMu.Unlock()
	s.cancel()
	s.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.redis.Del(ctx, s.gatewayLivenessKey(s.gatewayID)).Err(); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Warn().Err(err).Msg("can't remove gateway session heartbeat")
	}
}

// Done closes when ownership fencing can no longer be guaranteed. Gateways
// must terminate so their supervisor can restart them before accepting more
// authenticated sessions.
func (s *SessionOwnershipService) Done() <-chan struct{} {
	return s.ctx.Done()
}

func (s *SessionOwnershipService) Register(token string, evict func(context.Context) bool) func() {
	s.mu.Lock()
	s.sessions[token] = evict
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
	}
}

func (s *SessionOwnershipService) ClaimAccount(ctx context.Context, accountID uint32, token string) error {
	return s.claim(ctx, s.accountKey(accountID), token)
}

func (s *SessionOwnershipService) ClaimCharacter(ctx context.Context, characterGUID uint64, token string) error {
	return s.claim(ctx, s.characterKey(characterGUID), token)
}

func (s *SessionOwnershipService) ReleaseAccount(ctx context.Context, accountID uint32, token string) error {
	return s.release(ctx, s.accountKey(accountID), token)
}

func (s *SessionOwnershipService) ReleaseCharacter(ctx context.Context, characterGUID uint64, token string) error {
	return s.release(ctx, s.characterKey(characterGUID), token)
}

func (s *SessionOwnershipService) claim(ctx context.Context, key, token string) error {
	if !s.healthy.Load() {
		return ErrSessionOwnershipUnavailable
	}
	if err := s.waitForRecoveryFence(ctx); err != nil {
		return err
	}
	owner := s.owner(token)
	pendingKey := s.pendingClaimKey(key)
	leaseOwner := owner + "|" + strconv.FormatInt(time.Now().UnixNano(), 36)
	leaseTTL := 2 * s.livenessTTL
	if leaseTTL < time.Minute {
		leaseTTL = time.Minute
	}
	for {
		acquired, err := s.redis.SetNX(ctx, pendingKey, leaseOwner, leaseTTL).Result()
		if err != nil {
			return fmt.Errorf("acquire pending session claim: %w", err)
		}
		if acquired {
			break
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-s.ctx.Done():
			timer.Stop()
			return ErrSessionOwnershipUnavailable
		case <-timer.C:
		}
	}
	defer s.releasePendingClaim(pendingKey, leaseOwner)

	for attempts := 0; attempts < 8; attempts++ {
		previous, err := s.redis.Get(ctx, key).Result()
		if errors.Is(err, redis.Nil) {
			previous = ""
		} else if err != nil {
			return fmt.Errorf("read session ownership: %w", err)
		}
		if previous == owner {
			return s.commitClaim(ctx, pendingKey, leaseOwner, key, previous, owner)
		}

		previousGateway, previousToken, validPrevious := parseSessionOwner(previous)
		if previous != "" && !validPrevious {
			return errors.New("invalid existing session owner")
		}
		streamKey := s.evictionStreamKey(s.gatewayID)
		if validPrevious {
			streamKey = s.evictionStreamKey(previousGateway)
		}
		ackKey := s.evictionAckKey(token, attempts)
		result, err := prepareSessionOwnershipScript.Run(
			ctx, s.redis, []string{s.redisEpochKey(), key, streamKey}, s.redisEpoch,
			previous, owner, previousToken, ackKey, evictionStreamMaxLength,
		).Slice()
		if err != nil {
			return fmt.Errorf("claim session ownership: %w", err)
		}
		claimed, err := scriptInt64(result, 0)
		if err != nil {
			return err
		}
		if claimed == -1 {
			s.loseFencing(errors.New("Redis ownership epoch changed during claim"))
			return ErrSessionOwnershipUnavailable
		}
		if claimed == 0 {
			continue
		}

		if validPrevious && previous != owner {
			s.publishFastEviction(previousGateway, sessionEvictionRequest{Token: previousToken, AckKey: ackKey})
			if err := s.waitForEvictionAcknowledgement(ctx, previousGateway, ackKey); err != nil {
				return err
			}
		}
		return s.commitClaim(ctx, pendingKey, leaseOwner, key, previous, owner)
	}
	return errors.New("session ownership changed too frequently")
}

func (s *SessionOwnershipService) commitClaim(ctx context.Context, pendingKey, leaseOwner, key, previous, owner string) error {
	result, err := commitSessionOwnershipScript.Run(ctx, s.redis,
		[]string{s.redisEpochKey(), pendingKey, key}, s.redisEpoch, leaseOwner, previous, owner,
	).Int64()
	if err != nil {
		return fmt.Errorf("commit session ownership: %w", err)
	}
	switch result {
	case 1:
		return nil
	case -1:
		s.loseFencing(errors.New("Redis ownership epoch changed while committing claim"))
		return ErrSessionOwnershipUnavailable
	case -2:
		return errors.New("pending session claim lease expired")
	default:
		return ErrSessionOwnershipSuperseded
	}
}

func (s *SessionOwnershipService) releasePendingClaim(key, leaseOwner string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := releasePendingClaimScript.Run(ctx, s.redis, []string{key}, leaseOwner).Err(); err != nil {
		s.logger.Warn().Err(err).Msg("can't release pending session claim")
	}
}

func (s *SessionOwnershipService) publishFastEviction(gatewayID string, request sessionEvictionRequest) {
	payload, err := json.Marshal(request)
	if err != nil {
		return
	}
	if err = s.nats.Publish(sessionEvictionSubjectPrefix+gatewayID, payload); err != nil {
		s.logger.Warn().Err(err).Str("gatewayID", gatewayID).Msg("can't publish fast session eviction")
	}
}

func (s *SessionOwnershipService) waitForEvictionAcknowledgement(ctx context.Context, gatewayID, ackKey string) error {
	for {
		alive, err := s.redis.Exists(ctx, s.gatewayLivenessKey(gatewayID)).Result()
		if err != nil {
			return fmt.Errorf("check previous gateway liveness: %w", err)
		}
		if alive == 0 {
			return nil
		}
		wait := evictionAcknowledgeTimeout
		if wait > time.Second {
			wait = time.Second
		}
		result, err := s.redis.BLPop(ctx, wait, ackKey).Result()
		if err == nil && len(result) != 0 {
			return nil
		}
		if err != nil && !errors.Is(err, redis.Nil) {
			return fmt.Errorf("wait for session eviction acknowledgement: %w", err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
}

func (s *SessionOwnershipService) tryDispatchEviction(request sessionEvictionRequest) bool {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	if !s.healthy.Load() {
		return false
	}
	select {
	case s.workers <- struct{}{}:
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.workers }()
			s.processEviction(request)
		}()
		return true
	default:
		return false
	}
}

func (s *SessionOwnershipService) processEviction(request sessionEvictionRequest) {
	evictionID := request.AckKey
	if evictionID == "" {
		evictionID = request.Token
	}
	state, leader := s.beginEviction(evictionID)
	if !leader {
		select {
		case <-state.done:
			if state.confirmed && request.AckKey != "" {
				s.acknowledgeEviction(request.AckKey)
			}
		case <-s.ctx.Done():
		}
		return
	}

	s.mu.RLock()
	evict := s.sessions[request.Token]
	s.mu.RUnlock()
	confirmed := true
	if evict != nil {
		s.logger.Info().Msg("evicting a superseded local gateway session")
		confirmed = evict(s.ctx)
	}
	s.evictionMu.Lock()
	state.confirmed = confirmed
	close(state.done)
	s.evictionMu.Unlock()
	if confirmed && request.AckKey != "" {
		s.acknowledgeEviction(request.AckKey)
	}
}

func (s *SessionOwnershipService) beginEviction(id string) (*processedEviction, bool) {
	s.evictionMu.Lock()
	defer s.evictionMu.Unlock()
	now := time.Now()
	if state := s.processedEvictions[id]; state != nil && state.expiresAt.After(now) {
		return state, false
	}
	if len(s.processedEvictions) >= evictionStreamMaxLength {
		for key, state := range s.processedEvictions {
			if !state.expiresAt.After(now) {
				delete(s.processedEvictions, key)
			}
		}
		if len(s.processedEvictions) >= evictionStreamMaxLength {
			for key, state := range s.processedEvictions {
				select {
				case <-state.done:
					delete(s.processedEvictions, key)
				default:
				}
				if len(s.processedEvictions) < evictionStreamMaxLength {
					break
				}
			}
		}
	}
	state := &processedEviction{done: make(chan struct{}), expiresAt: now.Add(time.Minute)}
	s.processedEvictions[id] = state
	return state, true
}

func (s *SessionOwnershipService) acknowledgeEviction(ackKey string) {
	backoff := 100 * time.Millisecond
	for s.ctx.Err() == nil {
		pipe := s.redis.Pipeline()
		pipe.LPush(s.ctx, ackKey, "1")
		pipe.Expire(s.ctx, ackKey, 30*time.Second)
		if _, err := pipe.Exec(s.ctx); err == nil {
			return
		} else {
			s.logger.Warn().Err(err).Msg("can't acknowledge session eviction; retrying")
		}
		timer := time.NewTimer(backoff)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < time.Second {
			backoff *= 2
		}
	}
}

func (s *SessionOwnershipService) consumeEvictionStream() {
	defer s.wg.Done()
	stream := s.evictionStreamKey(s.gatewayID)
	lastID := "0-0"
	for s.ctx.Err() == nil {
		result, err := s.redis.XRead(s.ctx, &redis.XReadArgs{
			Streams: []string{stream, lastID}, Count: 64, Block: 2 * time.Second,
		}).Result()
		if err != nil {
			if !errors.Is(err, redis.Nil) && s.ctx.Err() == nil {
				s.logger.Warn().Err(err).Msg("can't consume durable session evictions")
				time.Sleep(time.Second)
			}
			continue
		}
		for _, streamResult := range result {
			for _, message := range streamResult.Messages {
				lastID = message.ID
				request := sessionEvictionRequest{
					Token: stringValue(message.Values["token"]), AckKey: stringValue(message.Values["ack_key"]),
				}
				for !s.tryDispatchEviction(request) && s.ctx.Err() == nil {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}
	}
}

func (s *SessionOwnershipService) runHeartbeat() {
	defer s.wg.Done()
	interval := s.livenessTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			heartbeatCtx, cancel := context.WithTimeout(s.ctx, heartbeatCommandTimeout)
			err := s.writeHeartbeat(heartbeatCtx)
			cancel()
			if err != nil && s.ctx.Err() == nil {
				s.logger.Warn().Err(err).Msg("can't refresh gateway session heartbeat")
				if errors.Is(err, errRedisOwnershipEpochChanged) {
					s.loseFencing(err)
					return
				}
				continue
			}
			s.heartbeat.Store(time.Now().UnixNano())
		case <-s.ctx.Done():
			return
		}
	}
}

// runHeartbeatWatchdog is intentionally independent of Redis I/O. Even if a
// client call stalls, it starts bounded session teardown early enough for the
// gateway liveness key to remain present until teardown has completed.
func (s *SessionOwnershipService) runHeartbeatWatchdog() {
	defer s.wg.Done()
	margin := sessionTeardownTimeout + heartbeatExpirySafetyMargin
	if margin >= s.livenessTTL {
		margin = s.livenessTTL / 2
	}
	for s.ctx.Err() == nil {
		lastSuccess := time.Unix(0, s.heartbeat.Load())
		remaining := time.Until(lastSuccess.Add(s.livenessTTL - margin))
		if remaining < 0 {
			remaining = 0
		}
		timer := time.NewTimer(remaining)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if s.heartbeat.Load() == lastSuccess.UnixNano() {
				s.loseFencing(errors.New("gateway ownership heartbeat reached its teardown deadline"))
				return
			}
		}
	}
}

func (s *SessionOwnershipService) loseFencing(reason error) {
	s.failOnce.Do(func() {
		s.logger.Error().Err(reason).Msg("session ownership fencing was lost; disconnecting local sessions")
		s.healthy.Store(false)
		go func() {
			s.evictAllSessions()
			s.cancel()
		}()
	})
}

func (s *SessionOwnershipService) evictAllSessions() {
	s.mu.RLock()
	evictions := make([]func(context.Context) bool, 0, len(s.sessions))
	for _, evict := range s.sessions {
		evictions = append(evictions, evict)
	}
	s.mu.RUnlock()
	var wg sync.WaitGroup
	for _, evict := range evictions {
		wg.Add(1)
		go func(evict func(context.Context) bool) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), sessionTeardownTimeout)
			defer cancel()
			evict(ctx)
		}(evict)
	}
	wg.Wait()
}

func (s *SessionOwnershipService) writeHeartbeat(ctx context.Context) error {
	epoch, err := s.redis.Get(ctx, s.redisEpochKey()).Result()
	if errors.Is(err, redis.Nil) {
		return errRedisOwnershipEpochChanged
	}
	if err != nil {
		return err
	}
	if epoch != s.redisEpoch {
		return errRedisOwnershipEpochChanged
	}
	return s.redis.Set(ctx, s.gatewayLivenessKey(s.gatewayID), "1", s.livenessTTL).Err()
}

func (s *SessionOwnershipService) initializeRedisEpoch(ctx context.Context) (string, error) {
	candidate := s.gatewayID + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	return initializeRedisEpochScript.Run(ctx, s.redis,
		[]string{s.redisEpochKey(), s.recoveryFenceKey()}, candidate, s.livenessTTL.Milliseconds(),
	).Text()
}

func (s *SessionOwnershipService) waitForRecoveryFence(ctx context.Context) error {
	for {
		remaining, err := s.redis.PTTL(ctx, s.recoveryFenceKey()).Result()
		if errors.Is(err, redis.Nil) || remaining <= 0 {
			return nil
		}
		if err != nil {
			return fmt.Errorf("check Redis ownership recovery fence: %w", err)
		}
		wait := remaining
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *SessionOwnershipService) release(ctx context.Context, key, token string) error {
	return releaseSessionOwnershipScript.Run(ctx, s.redis, []string{key}, s.owner(token)).Err()
}

func (s *SessionOwnershipService) owner(token string) string {
	return s.gatewayID + "|" + token
}

func parseSessionOwner(value string) (string, string, bool) {
	parts := strings.SplitN(value, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *SessionOwnershipService) realmHashTag() string {
	// Account ownership is global across realms. Keeping all ownership and
	// eviction keys in one hash slot also preserves the atomic claim+evict Lua
	// operation on Redis Cluster.
	return "{gateway-session:global}"
}

func (s *SessionOwnershipService) accountKey(accountID uint32) string {
	return s.realmHashTag() + ":owner:account:" + strconv.FormatUint(uint64(accountID), 10)
}

func (s *SessionOwnershipService) characterKey(characterGUID uint64) string {
	return s.realmHashTag() + ":owner:character:" + strconv.FormatUint(uint64(s.realmID), 10) + ":" + strconv.FormatUint(characterGUID, 10)
}

func (s *SessionOwnershipService) evictionStreamKey(gatewayID string) string {
	return s.realmHashTag() + ":evictions:" + gatewayID
}

func (s *SessionOwnershipService) gatewayLivenessKey(gatewayID string) string {
	return s.realmHashTag() + ":gateway:" + gatewayID
}

func (s *SessionOwnershipService) redisEpochKey() string {
	return s.realmHashTag() + ":epoch"
}

func (s *SessionOwnershipService) recoveryFenceKey() string {
	return s.realmHashTag() + ":recovery"
}

func (s *SessionOwnershipService) pendingClaimKey(ownerKey string) string {
	return ownerKey + ":pending"
}

func (s *SessionOwnershipService) evictionAckKey(token string, attempt int) string {
	return s.realmHashTag() + ":ack:" + token + ":" + strconv.Itoa(attempt) + ":" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func scriptInt64(values []any, index int) (int64, error) {
	if index >= len(values) {
		return 0, errors.New("invalid session ownership script response")
	}
	switch value := values[index].(type) {
	case int64:
		return value, nil
	case string:
		return strconv.ParseInt(value, 10, 64)
	default:
		return 0, fmt.Errorf("invalid session ownership script value %T", value)
	}
}

func stringValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return ""
	}
}
