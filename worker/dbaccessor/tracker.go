// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package dbaccessor

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/worker/v3"
	"gopkg.in/tomb.v2"

	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/database"
)

// TrackedDB defines the union of a TrackedDB and a worker.Worker interface.
// This is local to the package, allowing for better testing of the underlying
// trackerDB worker.
type TrackedDB interface {
	coredatabase.TrackedDB
	worker.Worker
}

// TrackedDBWorkerOption is a function that configures a TrackedDBWorker.
type TrackedDBWorkerOption func(*trackedDBWorker)

// WithVerifyDBFunc sets the function used to verify the database connection.
func WithVerifyDBFunc(f func(context.Context, *sql.DB) error) TrackedDBWorkerOption {
	return func(w *trackedDBWorker) {
		w.verifyDBFunc = f
	}
}

// WithClock sets the clock used by the worker.
func WithClock(clock clock.Clock) TrackedDBWorkerOption {
	return func(w *trackedDBWorker) {
		w.clock = clock
	}
}

// WithLogger sets the logger used by the worker.
func WithLogger(logger Logger) TrackedDBWorkerOption {
	return func(w *trackedDBWorker) {
		w.logger = logger
	}
}

// WithMetricsCollector sets the metrics collector used by the worker.
func WithMetricsCollector(metrics *Collector) TrackedDBWorkerOption {
	return func(w *trackedDBWorker) {
		w.metrics = metrics
	}
}

type trackedDBWorker struct {
	tomb tomb.Tomb

	dbApp     DBApp
	namespace string

	mutex sync.RWMutex
	db    *sql.DB
	err   error

	clock   clock.Clock
	logger  Logger
	metrics *Collector

	verifyDBFunc func(context.Context, *sql.DB) error
}

// NewTrackedDBWorker creates a new TrackedDBWorker
func NewTrackedDBWorker(dbApp DBApp, namespace string, opts ...TrackedDBWorkerOption) (TrackedDB, error) {
	w := &trackedDBWorker{
		dbApp:        dbApp,
		namespace:    namespace,
		clock:        clock.WallClock,
		verifyDBFunc: defaultVerifyDBFunc,
	}

	for _, opt := range opts {
		opt(w)
	}

	var err error
	w.db, err = w.dbApp.Open(context.TODO(), w.namespace)
	if err != nil {
		return nil, errors.Trace(err)
	}

	w.tomb.Go(w.loop)

	return w, nil
}

// DB closes over a raw *sql.DB. Closing over the DB allows the late
// realization of the database. Allowing retries of DB acquisition if there
// is a failure that is non-retryable.
func (w *trackedDBWorker) DB(fn func(*sql.DB) error) (err error) {
	// Record metrics based on the namespace.
	w.metrics.DBRequests.WithLabelValues(w.namespace).Inc()
	defer func(begin time.Time) {
		w.metrics.DBRequests.WithLabelValues(w.namespace).Dec()

		// Record the duration of the DB call and specifically call out if the
		// result was a success or an error. It might be useful to see if there
		// is a correlation between the duration of the call and the result.
		result := "success"
		if err != nil {
			result = "error"
		}
		w.metrics.DBDuration.WithLabelValues(
			w.namespace,
			result,
		).Observe(w.clock.Now().Sub(begin).Seconds())
	}(w.clock.Now())

	w.mutex.RLock()
	// We have a fatal error, the DB can not be accessed.
	if w.err != nil {
		w.mutex.RUnlock()
		return errors.Trace(w.err)
	}
	db := w.db
	w.mutex.RUnlock()

	if err = fn(db); err != nil {
		return errors.Trace(err)
	}
	return nil
}

// Txn closes over a raw *sql.Tx. This allows retry semantics in only one
// location. For instances where the underlying sql database is busy or if
// it's a common retryable error that can be handled cleanly in one place.
func (w *trackedDBWorker) Txn(ctx context.Context, fn func(context.Context, *sql.Tx) error) error {
	return w.DB(func(db *sql.DB) error {
		return database.Retry(ctx, func() error {
			// Record the number of transactions.
			w.metrics.TxnRequests.WithLabelValues(w.namespace).Inc()

			return database.Txn(ctx, db, fn)
		})
	})
}

// Err will return any fatal errors that have occurred on the worker, trying
// to acquire the database.
func (w *trackedDBWorker) Err() error {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	return w.err
}

// Kill implements worker.Worker
func (w *trackedDBWorker) Kill() {
	w.tomb.Kill(nil)
}

// Wait implements worker.Worker
func (w *trackedDBWorker) Wait() error {
	return w.tomb.Wait()
}

func (w *trackedDBWorker) loop() error {
	timer := w.clock.NewTimer(PollInterval)
	defer timer.Stop()

	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying
		case <-timer.Chan():
			// Any retryable errors are handled at the txn level. If we get an
			// error returning here, we've either exhausted the number of
			// retries or the error was fatal.
			w.mutex.RLock()
			currentDB := w.db
			w.mutex.RUnlock()

			newDB, err := w.verifyDB(currentDB)
			if err != nil {
				// If we get an error, ensure we close the underlying db and
				// mark the tracked db in an error state.
				w.mutex.Lock()
				if err := w.db.Close(); err != nil {
					w.logger.Errorf("error closing database: %v", err)
				}
				w.err = errors.Trace(err)
				w.mutex.Unlock()

				// As we failed attempting to verify the db, we're in a fatal
				// state. Collapse the worker and if required, cause the other
				// workers to follow suite.
				return errors.Trace(err)
			}

			// We've got a new DB. Close the old one and replace it with the
			// new one, if they're not the same.
			if newDB != currentDB {
				w.mutex.Lock()
				if err := w.db.Close(); err != nil {
					w.logger.Errorf("error closing database: %v", err)
				}
				w.db = newDB
				w.err = nil
				w.mutex.Unlock()
			}
		}
	}
}

func (w *trackedDBWorker) verifyDB(db *sql.DB) (*sql.DB, error) {
	// Force the timeout to be lower that the DefaultTimeout,
	// so we can spot issues sooner.
	// Also allow killing the tomb to cancel the context,
	// so shutdown/restart can not be blocked by this call.
	ctx, cancel := context.WithTimeout(context.TODO(), time.Second*10)
	ctx = w.tomb.Context(ctx)
	defer cancel()

	if w.logger.IsTraceEnabled() {
		w.logger.Tracef("verifying database by attempting a ping")
	}

	// There are multiple levels of retries here. For good reason. We want to
	// retry the transaction semantics for retryable errors. Then if that fails
	// we want to retry if the database is at fault. In that case we want
	// to open up a new database and try the transaction again.
	for i := 0; i < DefaultVerifyAttempts; i++ {
		// Verify that we don't have a potential nil database from the retry
		// semantics.
		if db == nil {
			return nil, errors.NotFoundf("database")
		}

		err := database.Retry(ctx, func() error {
			if w.logger.IsTraceEnabled() {
				w.logger.Tracef("attempting ping")
			}
			return w.verifyDBFunc(ctx, db)
		})
		// We were successful at requesting the schema, so we can bail out
		// early.
		if err == nil {
			return db, nil
		}

		// We failed to apply the transaction, so just return the error and
		// cause the worker to crash.
		if i == DefaultVerifyAttempts-1 {
			return nil, errors.Trace(err)
		}

		// We got an error that is non-retryable, attempt to open a new database
		// connection and see if that works.
		w.logger.Errorf("unable to ping db: attempting to reopen the database before attempting again: %v", err)

		// Attempt to open a new database. If there is an error, just crash
		// the worker, we can't do anything else.
		if db, err = w.dbApp.Open(ctx, w.namespace); err != nil {
			return nil, errors.Trace(err)
		}
	}
	return nil, errors.NotValidf("database")
}

func defaultVerifyDBFunc(ctx context.Context, db *sql.DB) error {
	return db.PingContext(ctx)
}
