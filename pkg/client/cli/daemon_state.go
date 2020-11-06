package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/datawire/telepresence2/pkg/client"
	"github.com/datawire/telepresence2/pkg/rpc/daemon"
)

type daemonState struct {
	cmd      *cobra.Command
	dns      string
	fallback string
	conn     *grpc.ClientConn
	grpc     daemon.DaemonClient
}

func newDaemonState(cmd *cobra.Command, dns, fallback string) (*daemonState, error) {
	ds := &daemonState{cmd: cmd, dns: dns, fallback: fallback}
	err := assertDaemonStarted()
	if err == nil {
		err = ds.connect()
	}
	return ds, err
}

func (ds *daemonState) EnsureState() (bool, error) {
	if ds.isConnected() {
		return false, nil
	}
	quitLegacyDaemon(ds.cmd.OutOrStdout())

	fmt.Fprintln(ds.cmd.OutOrStdout(), "Launching Telepresence Daemon", client.DisplayVersion())

	err := runAsRoot(client.GetExe(), []string{"daemon-foreground", ds.dns, ds.fallback},
		ds.cmd.InOrStdin(), ds.cmd.OutOrStdout(), ds.cmd.ErrOrStderr())
	if err != nil {
		return false, errors.Wrap(err, "failed to launch the server")
	}

	if err = client.WaitUntilSocketAppears("daemon", client.DaemonSocketName, 10*time.Second); err != nil {
		return false, fmt.Errorf("Daemon service did not come up!\nTake a look at %s for more information.", client.Logfile)
	}
	err = ds.connect()
	return err == nil, err
}

func (ds *daemonState) DeactivateState() error {
	if !ds.isConnected() {
		return nil
	}
	fmt.Fprint(ds.cmd.OutOrStdout(), "Telepresence Daemon quitting...")
	var err error
	if client.SocketExists(client.DaemonSocketName) {
		_, err = ds.grpc.Quit(context.Background(), &empty.Empty{})
	}
	ds.disconnect()
	if err == nil {
		err = client.WaitUntilSocketVanishes("daemon", client.DaemonSocketName, 5*time.Second)
	}
	if err == nil {
		fmt.Fprintln(ds.cmd.OutOrStdout(), "done")
	} else {
		fmt.Fprintln(ds.cmd.OutOrStdout())
	}
	return err
}

// isConnected returns true if a connection has been established to the daemon
func (ds *daemonState) isConnected() bool {
	return ds.conn != nil
}

// connect opens the client connection to the daemon.
func (ds *daemonState) connect() (err error) {
	if ds.conn, err = grpc.Dial(client.SocketURL(client.DaemonSocketName), grpc.WithInsecure()); err == nil {
		ds.grpc = daemon.NewDaemonClient(ds.conn)
	}
	return
}

// disconnect closes the client connection to the daemon.
func (ds *daemonState) disconnect() {
	conn := ds.conn
	ds.conn = nil
	ds.grpc = nil
	if conn != nil {
		conn.Close()
	}
}

const legacySocketName = "/var/run/edgectl.socket"

// quitLegacyDaemon ensures that an older printVersion of the daemon quits and removes the old socket.
func quitLegacyDaemon(out io.Writer) {
	if !client.SocketExists(legacySocketName) {
		return // no legacy daemon is running
	}
	if conn, err := net.Dial("unix", legacySocketName); err == nil {
		defer conn.Close()

		_, _ = io.WriteString(conn, `{"Args": ["edgectl", "quit"], "APIVersion": 1}`)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			fmt.Fprintf(out, "Legacy daemon: %s\n", scanner.Text())
		}
	}
}