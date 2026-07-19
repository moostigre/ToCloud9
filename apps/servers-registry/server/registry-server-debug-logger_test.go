package server

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

type resetRPCServerStub struct {
	pb.UnimplementedServersRegistryServiceServer
	targetsCalled  bool
	reassignCalled bool
}

func (s *resetRPCServerStub) GetInstanceResetTargets(context.Context, *pb.GetInstanceResetTargetsRequest) (*pb.GetInstanceResetTargetsResponse, error) {
	s.targetsCalled = true
	return &pb.GetInstanceResetTargetsResponse{}, nil
}

func (s *resetRPCServerStub) ReassignInstanceAfterReset(context.Context, *pb.ReassignInstanceAfterResetRequest) (*pb.ReassignInstanceAfterResetResponse, error) {
	s.reassignCalled = true
	return &pb.ReassignInstanceAfterResetResponse{}, nil
}

func TestDebugMiddlewareDelegatesInstanceResetRPCs(t *testing.T) {
	real := &resetRPCServerStub{}
	middleware := NewServersRegistryDebugLoggerMiddleware(real, zerolog.Nop())

	_, err := middleware.GetInstanceResetTargets(context.Background(), &pb.GetInstanceResetTargetsRequest{})
	require.NoError(t, err)
	_, err = middleware.ReassignInstanceAfterReset(context.Background(), &pb.ReassignInstanceAfterResetRequest{})
	require.NoError(t, err)
	require.True(t, real.targetsCalled)
	require.True(t, real.reassignCalled)
}
