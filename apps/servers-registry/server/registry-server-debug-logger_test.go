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
	finalizeCalled bool
}

func (s *resetRPCServerStub) GetInstanceResetTargets(context.Context, *pb.GetInstanceResetTargetsRequest) (*pb.GetInstanceResetTargetsResponse, error) {
	s.targetsCalled = true
	return &pb.GetInstanceResetTargetsResponse{}, nil
}

func (s *resetRPCServerStub) FinalizeInstanceReset(context.Context, *pb.FinalizeInstanceResetRequest) (*pb.FinalizeInstanceResetResponse, error) {
	s.finalizeCalled = true
	return &pb.FinalizeInstanceResetResponse{}, nil
}

func TestDebugMiddlewareDelegatesInstanceResetRPCs(t *testing.T) {
	real := &resetRPCServerStub{}
	middleware := NewServersRegistryDebugLoggerMiddleware(real, zerolog.Nop())

	_, err := middleware.GetInstanceResetTargets(context.Background(), &pb.GetInstanceResetTargetsRequest{})
	require.NoError(t, err)
	_, err = middleware.FinalizeInstanceReset(context.Background(), &pb.FinalizeInstanceResetRequest{})
	require.NoError(t, err)
	require.True(t, real.targetsCalled)
	require.True(t, real.finalizeCalled)
}
