//go:build !dqlite
// +build !dqlite

// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package app

import (
	"context"
	"crypto/tls"
	"database/sql"
	"net"
	"path/filepath"
	"time"

	"github.com/juju/errors"
	_ "github.com/mattn/go-sqlite3"

	"github.com/juju/juju/database/client"
)

// Option can be used to tweak app parameters.
type Option func()

type SnapshotParams struct {
	Threshold uint64
	Trailing  uint64
}

// WithAddress sets the network address of the application node.
//
// Other application nodes must be able to connect to this application node
// using the given address.
//
// If the application node is not the first one in the cluster, the address
// must match the value that was passed to the App.Add() method upon
// registration.
//
// If not given the first non-loopback IP address of any of the system network
// interfaces will be used, with port 9000.
//
// The address must be stable across application restarts.
func WithAddress(address string) Option {
	return func() {}
}

// WithCluster must be used when starting a newly added application node for
// the first time.
//
// It should contain the addresses of one or more applications nodes which are
// already part of the cluster.
func WithCluster(cluster []string) Option {
	return func() {}
}

// WithExternalConn enables passing an external dial function that will be used
// whenever dqlite needs to make an outside connection.
//
// Also takes a net.Conn channel that should be received when the external connection has been accepted.
func WithExternalConn(dialFunc client.DialFunc, acceptCh chan net.Conn) Option {
	return func() {}
}

// WithTLS enables TLS encryption of network traffic.
//
// The "listen" parameter must hold the TLS configuration to use when accepting
// incoming connections clients or application nodes.
//
// The "dial" parameter must hold the TLS configuration to use when
// establishing outgoing connections to other application nodes.
func WithTLS(listen *tls.Config, dial *tls.Config) Option {
	return func() {}
}

// WithUnixSocket allows setting a specific socket path for communication between go-dqlite and dqlite.
//
// The default is an empty string which means a random abstract unix socket.
func WithUnixSocket(path string) Option {
	return func() {}
}

// WithVoters sets the number of nodes in the cluster that should have the
// Voter role.
//
// When a new node is added to the cluster or it is started again after a
// shutdown it will be assigned the Voter role in case the current number of
// voters is below n.
//
// Similarly when a node with the Voter role is shutdown gracefully by calling
// the Handover() method, it will try to transfer its Voter role to another
// non-Voter node, if one is available.
//
// All App instances in a cluster must be created with the same WithVoters
// setting.
//
// The given value must be an odd number greater than one.
//
// The default value is 3.
func WithVoters(n int) Option {
	return func() {}
}

// WithStandBys sets the number of nodes in the cluster that should have the
// StandBy role.
//
// When a new node is added to the cluster or it is started again after a
// shutdown it will be assigned the StandBy role in case there are already
// enough online voters, but the current number of stand-bys is below n.
//
// Similarly when a node with the StandBy role is shutdown gracefully by
// calling the Handover() method, it will try to transfer its StandBy role to
// another non-StandBy node, if one is available.
//
// All App instances in a cluster must be created with the same WithStandBys
// setting.
//
// The default value is 3.
func WithStandBys(n int) Option {
	return func() {}
}

// WithRolesAdjustmentFrequency sets the frequency at which the current cluster
// leader will check if the roles of the various nodes in the cluster matches
// the desired setup and perform promotions/demotions to adjust the situation
// if needed.
//
// The default is 30 seconds.
func WithRolesAdjustmentFrequency(frequency time.Duration) Option {
	return func() {}
}

// WithLogFunc sets a custom log function.
func WithLogFunc(log client.LogFunc) Option {
	return func() {}
}

// WithFailureDomain sets the node's failure domain.
//
// Failure domains are taken into account when deciding which nodes to promote
// to Voter or StandBy when needed.
func WithFailureDomain(code uint64) Option {
	return func() {}
}

// WithNetworkLatency sets the average one-way network latency.
func WithNetworkLatency(latency time.Duration) Option {
	return func() {}
}

// WithSnapshotParams sets the raft snapshot parameters.
func WithSnapshotParams(params SnapshotParams) Option {
	return func() {}
}

// App is a high-level helper for initializing a typical dqlite-based Go
// application.
//
// It takes care of starting a dqlite node and registering a dqlite Go SQL
// driver.
type App struct {
	dir string
}

// New creates a new application node.
func New(dir string, options ...Option) (*App, error) {
	return &App{dir: dir}, nil
}

// Ready can be used to wait for a node to complete tasks that
// are initiated at startup. For example a new node will attempt
// to join the cluster, a restarted node will check if it should
// assume some particular role, etc.
//
// If this method returns without error it means that those initial
// tasks have succeeded and follow-up operations like Open() are more
// likely to succeed quickly.
func (*App) Ready(ctx context.Context) error {
	return nil
}

// Open the dqlite database with the given name
func (a *App) Open(ctx context.Context, name string) (*sql.DB, error) {
	path := name
	if name != ":memory:" {
		path = filepath.Join(a.dir, name)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return db, nil
}

// Handover transfers all responsibilities for this node (such has
// leadership and voting rights) to another node, if one is available.
//
// This method should always be called before invoking Close(),
// in order to gracefully shut down a node.
func (*App) Handover(context.Context) error {
	return nil
}

// ID returns the dqlite ID of this application node.
func (*App) ID() uint64 {
	return 1
}

func (*App) Close() error {
	return nil
}
