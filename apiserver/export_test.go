// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver

import (
	"context"
	"sync"

	"github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/names/v4"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version/v2"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/facade"
	"github.com/juju/juju/apiserver/stateauthenticator"
	"github.com/juju/juju/core/permission"
	"github.com/juju/juju/rpc"
	"github.com/juju/juju/state"
)

var (
	NewPingTimeout        = newPingTimeout
	MaxClientPingInterval = maxClientPingInterval
	NewBackups            = &newBackups
	SetResource           = setResource
)

func APIHandlerWithEntity(entity state.Entity) *apiHandler {
	return &apiHandler{entity: entity}
}

func NewErrRoot(err error) *errRoot {
	return &errRoot{err}
}

type testingAPIRootHandler struct{}

func (testingAPIRootHandler) State() *state.State {
	return nil
}

func (testingAPIRootHandler) SharedContext() *sharedServerContext {
	return nil
}

func (testingAPIRootHandler) Authorizer() facade.Authorizer {
	return nil
}

func (testingAPIRootHandler) Resources() *common.Resources {
	return common.NewResources()
}

// TestingAPIRoot gives you an APIRoot as a rpc.Methodfinder that is
// *barely* connected to anything.  Just enough to let you probe some
// of the interfaces, but not enough to actually do any RPC calls.
func TestingAPIRoot(facades *facade.Registry) rpc.Root {
	root, err := newAPIRoot(clock.WallClock, facades, testingAPIRootHandler{}, nil)
	if err != nil {
		// While not ideal, this is only in test code, and there are a bunch of other functions
		// that depend on this one that don't return errors either.
		panic(err)
	}
	return root
}

// TestingAPIHandler gives you an APIHandler that isn't connected to
// anything real. It's enough to let test some basic functionality though.
func TestingAPIHandler(c *gc.C, pool *state.StatePool, st *state.State) (*apiHandler, *common.Resources) {
	authenticator, err := stateauthenticator.NewAuthenticator(pool, clock.WallClock)
	c.Assert(err, jc.ErrorIsNil)
	offerAuthCtxt, err := newOfferAuthcontext(pool)
	c.Assert(err, jc.ErrorIsNil)
	srv := &Server{
		authenticator: authenticator,
		offerAuthCtxt: offerAuthCtxt,
		shared:        &sharedServerContext{statePool: pool},
		tag:           names.NewMachineTag("0"),
	}
	h, err := newAPIHandler(srv, st, nil, st.ModelUUID(), 6543, "testing.invalid:1234")
	c.Assert(err, jc.ErrorIsNil)
	return h, h.Resources()
}

// TestingAPIHandlerWithEntity gives you the sane kind of APIHandler as
// TestingAPIHandler but sets the passed entity as the apiHandler
// entity.
func TestingAPIHandlerWithEntity(c *gc.C, pool *state.StatePool, st *state.State, entity state.Entity) (*apiHandler, *common.Resources) {
	h, hr := TestingAPIHandler(c, pool, st)
	h.entity = entity
	return h, hr
}

// TestingAPIHandlerWithToken gives you the sane kind of APIHandler as
// TestingAPIHandler but sets the passed token as the apiHandler
// login token.
func TestingAPIHandlerWithToken(c *gc.C, pool *state.StatePool, st *state.State, jwt jwt.Token) (*apiHandler, *common.Resources) {
	h, hr := TestingAPIHandler(c, pool, st)
	user, err := names.ParseUserTag(jwt.Subject())
	c.Assert(err, jc.ErrorIsNil)
	h.entity = tokenEntity{user: user}
	h.loginToken = jwt
	return h, hr
}

// TokenPublicKey returns the login token public key for the given url.
func TokenPublicKey(c *gc.C, srv *Server, url string) jwk.Set {
	set, err := srv.jwtTokenService.cache.Get(context.TODO(), url)
	c.Assert(err, jc.ErrorIsNil)
	return set
}

// TestingUpgradingRoot returns a resricted srvRoot in an upgrade
// scenario.
func TestingUpgradingRoot() rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, upgradeMethodsOnly)
}

// TestingMigratingRoot returns a resricted srvRoot in a migration
// scenario.
func TestingMigratingRoot() rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, migrationClientMethodsOnly)
}

// TestingAnonymousRoot returns a restricted srvRoot as if
// logged in anonymously.
func TestingAnonymousRoot() rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, anonymousFacadesOnly)
}

// TestingControllerOnlyRoot returns a restricted srvRoot as if
// logged in to the root of the API path.
func TestingControllerOnlyRoot() rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, controllerFacadesOnly)
}

// TestingModelOnlyRoot returns a restricted srvRoot as if
// logged in to a model.
func TestingModelOnlyRoot() rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, modelFacadesOnly)
}

// TestingCAASModelOnlyRoot returns a restricted srvRoot as if
// logged in to a CAAS model.
func TestingCAASModelOnlyRoot() rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, caasModelFacadesOnly)
}

// TestingRestrictedRoot returns a restricted srvRoot.
func TestingRestrictedRoot(check func(string, string) error) rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, check)
}

// TestingUpgradeOrMigrationOnlyRoot returns a restricted srvRoot
// as if called from a newer client.
func TestingUpgradeOrMigrationOnlyRoot(userLogin bool, clientVersion version.Number) rpc.Root {
	r := TestingAPIRoot(AllFacades())
	return restrictRoot(r, checkClientVersion(userLogin, clientVersion))
}

// PatchGetMigrationBackend overrides the getMigrationBackend function
// to support testing.
func PatchGetMigrationBackend(p Patcher, ctrlSt controllerBackend, st migrationBackend) {
	p.PatchValue(&getMigrationBackend, func(*state.State) migrationBackend {
		return st
	})
	p.PatchValue(&getControllerBackend, func(pool *state.StatePool) (controllerBackend, error) {
		return ctrlSt, nil
	})
}

// PatchGetControllerCACert overrides the getControllerCACert function
// to support testing.
func PatchGetControllerCACert(p Patcher, caCert string) {
	p.PatchValue(&getControllerCACert, func(backend controllerBackend) (string, error) {
		return caCert, nil
	})
}

// ServerWaitGroup exposes the underlying wait group used to track running API calls
// to allow tests to hold a server open.
func ServerWaitGroup(server *Server) *sync.WaitGroup {
	return &server.wg
}

// Patcher defines an interface that matches the PatchValue method on
// CleanupSuite
type Patcher interface {
	PatchValue(ptr, value interface{})
}

func AssertHasPermission(c *gc.C, handler *apiHandler, access permission.Access, tag names.Tag, expect bool) {
	hasPermission, err := handler.HasPermission(access, tag)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(hasPermission, gc.Equals, expect)
}

func CheckHasPermission(st *state.State, operation permission.Access, target names.Tag) (bool, error) {
	if operation != permission.SuperuserAccess || target.Kind() != names.UserTagKind {
		return false, errors.Errorf("%s is not a user", names.ReadableString(target))
	}
	isAdmin, err := st.IsControllerAdmin(target.(names.UserTag))
	if err != nil {
		return false, err
	}
	if !isAdmin {
		return false, errors.Errorf("%s is not a controller admin", names.ReadableString(target))
	}
	return true, nil
}
