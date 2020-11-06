package manager

import (
	"context"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/datawire/ambassador/pkg/dlog"

	"github.com/datawire/telepresence2/pkg/rpc"
	"github.com/datawire/telepresence2/pkg/version"
)

type Manager struct {
	rpc.UnimplementedManagerServer

	state *State
}

type wall struct{}

func (wall) Now() time.Time {
	return time.Now()
}

func NewManager(ctx context.Context) *Manager {
	return &Manager{state: NewState(ctx, wall{})}
}

func (*Manager) Version(context.Context, *empty.Empty) (*rpc.VersionInfo2, error) {
	return &rpc.VersionInfo2{Version: version.Version}, nil
}

func (m *Manager) ArriveAsClient(ctx context.Context, client *rpc.ClientInfo) (*rpc.SessionInfo, error) {
	dlog.Debug(ctx, "ArriveAsClient called")

	if val := validateClient(client); val != "" {
		return nil, status.Errorf(codes.InvalidArgument, val)
	}

	sessionID := m.state.AddClient(client)

	return &rpc.SessionInfo{SessionId: sessionID}, nil
}

func (m *Manager) ArriveAsAgent(ctx context.Context, agent *rpc.AgentInfo) (*rpc.SessionInfo, error) {
	dlog.Debug(ctx, "ArriveAsAgent called")

	if val := validateAgent(agent); val != "" {
		return nil, status.Errorf(codes.InvalidArgument, val)
	}

	sessionID := m.state.AddAgent(agent)

	return &rpc.SessionInfo{SessionId: sessionID}, nil
}

func (m *Manager) Remain(ctx context.Context, session *rpc.SessionInfo) (*empty.Empty, error) {
	dlog.Debug(ctx, "Remain called")

	if ok := m.state.Mark(session.SessionId); !ok {
		return nil, status.Errorf(codes.NotFound, "Session %q not found", session.SessionId)
	}

	return &empty.Empty{}, nil
}

func (m *Manager) Depart(ctx context.Context, session *rpc.SessionInfo) (*empty.Empty, error) {
	dlog.Debug(ctx, "Depart called")

	m.state.Remove(session.SessionId)

	return &empty.Empty{}, nil
}

func (m *Manager) WatchAgents(session *rpc.SessionInfo, stream rpc.Manager_WatchAgentsServer) error {
	ctx := stream.Context()
	sessionID := session.SessionId

	dlog.Debug(ctx, "WatchAgents called", sessionID)

	if !m.state.HasClient(sessionID) {
		return status.Errorf(codes.NotFound, "Client session %q not found", session.SessionId)
	}

	entry := m.state.Get(sessionID)
	sessionCtx := entry.Context()
	changed := m.state.WatchAgents(sessionID)

	for {
		// FIXME This will loop over the presence list looking for agents for
		// every single watcher. How inefficient!
		res := &rpc.AgentInfoSnapshot{Agents: m.state.GetAgents()}

		if err := stream.Send(res); err != nil {
			return err
		}

		select {
		case <-changed:
			// It's time to send another message. Loop.
		case <-ctx.Done():
			// Manager is shutting down.
			return nil
		case <-sessionCtx.Done():
			// Manager believes this session has ended.
			return nil
		}
	}
}

func (m *Manager) WatchIntercepts(session *rpc.SessionInfo, stream rpc.Manager_WatchInterceptsServer) error {
	ctx := stream.Context()
	sessionID := session.SessionId

	dlog.Debug(ctx, "WatchIntercepts called", sessionID)

	entry := m.state.Get(sessionID)
	if entry == nil {
		return status.Errorf(codes.NotFound, "Session %q not found", sessionID)
	}

	sessionCtx := entry.Context()
	changed := m.state.WatchIntercepts(sessionID)

	for {
		res := &rpc.InterceptInfoSnapshot{
			Intercepts: m.state.GetIntercepts(sessionID),
		}
		if err := stream.Send(res); err != nil {
			return err
		}

		select {
		case <-changed:
			// It's time to send another message. Loop.
		case <-ctx.Done():
			// Manager is shutting down.
			return nil
		case <-sessionCtx.Done():
			// Manager believes this session has ended.
			return nil
		}
	}
}

func (m *Manager) CreateIntercept(ctx context.Context, ciReq *rpc.CreateInterceptRequest) (*rpc.InterceptInfo, error) {
	sessionID := ciReq.Session.SessionId
	spec := ciReq.InterceptSpec

	dlog.Debug(ctx, "CreateIntercept called", sessionID)

	if !m.state.HasClient(sessionID) {
		return nil, status.Errorf(codes.NotFound, "Client session %q not found", sessionID)
	}

	if val := validateIntercept(spec); val != "" {
		return nil, status.Errorf(codes.InvalidArgument, val)
	}

	for _, cept := range m.state.GetIntercepts(sessionID) {
		if cept.Spec.Name == spec.Name {
			return nil, status.Errorf(codes.AlreadyExists, "Intercept named %q already exists", spec.Name)
		}
	}

	return m.state.AddIntercept(sessionID, spec), nil
}

func (m *Manager) RemoveIntercept(ctx context.Context, riReq *rpc.RemoveInterceptRequest2) (*empty.Empty, error) {
	sessionID := riReq.Session.SessionId
	name := riReq.Name

	dlog.Debug(ctx, "RemoveIntercept called", sessionID, name)

	if !m.state.HasClient(sessionID) {
		return nil, status.Errorf(codes.NotFound, "Client session %q not found", sessionID)
	}

	if !m.state.RemoveIntercept(sessionID, name) {
		return nil, status.Errorf(codes.NotFound, "Intercept named %q not found", name)
	}

	return &empty.Empty{}, nil
}