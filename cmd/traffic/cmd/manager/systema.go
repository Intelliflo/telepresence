package manager

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/datawire/dlib/dgroup"
	systemarpc "github.com/datawire/telepresence2/pkg/rpc/systema"
	"github.com/datawire/telepresence2/pkg/systema"
)

type systemaCredentials struct {
	mgr *Manager
}

// GetRequestMetadata implements credentials.PerRPCCredentials.
func (c *systemaCredentials) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	// FIXME: This is just picking a token arbitrarily right now.
	var token string
	for _, client := range c.mgr.state.GetAllClients() {
		if client.BearerToken != "" {
			token = client.BearerToken
			break
		}
	}
	if token == "" {
		return nil, errors.New("no token has been provided by a client")
	}
	md := map[string]string{
		"X-Telepresence-ManagerID": c.mgr.env.AmbassadorClusterID,
		"Authorization":            "Bearer " + token,
	}
	return md, nil
}

// RequireTransportSecurity implements credentials.PerRPCCredentials.
func (c *systemaCredentials) RequireTransportSecurity() bool {
	return true
}

func (m *Manager) DialIntercept(ctx context.Context, interceptID string) (net.Conn, error) {
	// TODO: Don't hard-code
	dialer := &tls.Dialer{
		Config: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	return dialer.DialContext(ctx, "tcp", "ambassador.ambassador:443")
}

type systemaPool struct {
	mgr *Manager

	mu     sync.Mutex
	count  int64
	ctx    context.Context
	cancel context.CancelFunc
	client systemarpc.SystemACRUDClient
	wait   func() error
}

func NewSystemAPool(mgr *Manager) *systemaPool {
	return &systemaPool{
		mgr: mgr,
	}
}

func (p *systemaPool) Get() (systemarpc.SystemACRUDClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx == nil {
		host := p.mgr.env.SystemAHost
		port := p.mgr.env.SystemAPort

		ctx, cancel := context.WithCancel(dgroup.WithGoroutineName(p.mgr.ctx, "/systema"))
		client, wait, err := systema.ConnectToSystemA(
			ctx, p.mgr, net.JoinHostPort(host, port),
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{ServerName: host})),
			grpc.WithPerRPCCredentials(&systemaCredentials{p.mgr}))
		if err != nil {
			cancel()
			return nil, err
		}
		p.ctx, p.cancel, p.client, p.wait = ctx, cancel, client, wait
	}

	p.count++
	return p.client, nil
}

func (p *systemaPool) Done() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.count--
	var err error
	if p.count == 0 {
		p.cancel()
		err = p.wait()
		p.ctx, p.cancel, p.client, p.wait = nil, nil, nil, nil
	}
	return err
}