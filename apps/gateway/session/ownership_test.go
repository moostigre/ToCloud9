package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	"github.com/walkline/ToCloud9/apps/gateway/sockets"
)

type ownershipTestSocket struct {
	read   chan *packet.Packet
	write  chan *packet.Packet
	closed chan struct{}
	once   sync.Once
}

func newOwnershipTestSocket() *ownershipTestSocket {
	return &ownershipTestSocket{read: make(chan *packet.Packet), write: make(chan *packet.Packet, 1), closed: make(chan struct{})}
}

func (s *ownershipTestSocket) Close()                                 { s.once.Do(func() { close(s.read); close(s.closed) }) }
func (s *ownershipTestSocket) ListenAndProcess(context.Context) error { return nil }
func (s *ownershipTestSocket) Address() string                        { return "test" }
func (s *ownershipTestSocket) SendPacket(p *packet.Packet)            { s.write <- p }
func (s *ownershipTestSocket) Send(p *packet.Writer) {
	s.write <- &packet.Packet{Opcode: p.Opcode, Data: p.Payload.Bytes()}
}
func (s *ownershipTestSocket) ReadChannel() <-chan *packet.Packet  { return s.read }
func (s *ownershipTestSocket) WriteChannel() chan<- *packet.Packet { return s.write }

type ownershipTestCoordinator struct {
	mu                sync.Mutex
	claimed           bool
	released          bool
	releasedCharacter uint64
	evict             func(context.Context) bool
}

func (o *ownershipTestCoordinator) Register(_ string, evict func(context.Context) bool) func() {
	o.mu.Lock()
	o.evict = evict
	o.mu.Unlock()
	return func() {}
}
func (o *ownershipTestCoordinator) ClaimAccount(context.Context, uint32, string) error {
	o.mu.Lock()
	o.claimed = true
	o.mu.Unlock()
	return nil
}
func (*ownershipTestCoordinator) ClaimCharacter(context.Context, uint64, string) error { return nil }
func (o *ownershipTestCoordinator) ReleaseAccount(context.Context, uint32, string) error {
	o.mu.Lock()
	o.released = true
	o.mu.Unlock()
	return nil
}
func (o *ownershipTestCoordinator) ReleaseCharacter(_ context.Context, guid uint64, _ string) error {
	o.mu.Lock()
	o.releasedCharacter = guid
	o.mu.Unlock()
	return nil
}

func TestGameSessionClaimsAndReleasesAccountOwnership(t *testing.T) {
	socket := newOwnershipTestSocket()
	coordinator := &ownershipTestCoordinator{}
	logger := zerolog.Nop()
	session := NewGameSession(context.Background(), &logger, socket, 42, nil, GameSessionParams{SessionOwnership: coordinator})

	done := make(chan struct{})
	go func() {
		session.HandlePackets(context.Background())
		close(done)
	}()
	socket.Close()
	<-done

	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if !coordinator.claimed || !coordinator.released {
		t.Fatalf("ownership lifecycle incomplete: claimed=%v released=%v", coordinator.claimed, coordinator.released)
	}
}

func TestGameSessionEvictionDisconnectsPreviousClient(t *testing.T) {
	socket := newOwnershipTestSocket()
	coordinator := &ownershipTestCoordinator{}
	logger := zerolog.Nop()
	session := NewGameSession(context.Background(), &logger, socket, 42, nil, GameSessionParams{SessionOwnership: coordinator})

	done := make(chan struct{})
	go func() {
		session.HandlePackets(context.Background())
		close(done)
	}()
	deadline := time.After(time.Second)
	var evict func(context.Context) bool
	for evict == nil {
		coordinator.mu.Lock()
		evict = coordinator.evict
		coordinator.mu.Unlock()
		select {
		case <-deadline:
			t.Fatal("session did not register its eviction callback")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	evict(ctx)
	select {
	case <-socket.closed:
	case <-ctx.Done():
		t.Fatal("eviction did not close the previous client")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("evicted session did not stop")
	}
}

func TestCanceledWorldConnectClosesUnpublishedSocketBeforePlayerLogin(t *testing.T) {
	socket := newOwnershipTestSocket()
	oldCreator := WorldSocketCreator
	WorldSocketCreator = func(*zerolog.Logger, string) (sockets.Socket, error) { return socket, nil }
	defer func() { WorldSocketCreator = oldCreator }()
	logger := zerolog.Nop()
	session := &GameSession{ctx: context.Background(), logger: &logger, authPacket: &packet.Packet{}}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := session.connectToGameServerWithAddress(ctx, 42, "world", nil)
		result <- err
	}()
	go func() { socket.read <- packet.NewWriter(packet.SMsgAuthChallenge).ToPacket() }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled world connect = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled world connect did not return")
	}
	select {
	case <-socket.closed:
	case <-time.After(time.Second):
		t.Fatal("canceled world connect left its unpublished socket open")
	}
	if got := len(socket.write); got != 1 {
		t.Fatalf("socket write count = %d, want only the auth packet and no player login", got)
	}
}
