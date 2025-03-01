// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	"github.com/juju/charm/v10"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/state"
)

type EndpointSuite struct {
}

var _ = gc.Suite(&EndpointSuite{})

var canRelateTests = []struct {
	role1, role2 charm.RelationRole
	success      bool
}{
	{charm.RoleProvider, charm.RoleRequirer, true},
	{charm.RoleRequirer, charm.RolePeer, false},
	{charm.RolePeer, charm.RoleProvider, false},
	{charm.RoleProvider, charm.RoleProvider, false},
	{charm.RoleRequirer, charm.RoleRequirer, false},
	{charm.RolePeer, charm.RolePeer, false},
}

func (s *EndpointSuite) TestCanRelate(c *gc.C) {
	for i, t := range canRelateTests {
		c.Logf("test %d", i)
		ep1 := state.Endpoint{
			ApplicationName: "one-application",
			Relation: charm.Relation{
				Interface: "ifce",
				Name:      "foo",
				Role:      t.role1,
				Scope:     charm.ScopeGlobal,
			},
		}
		ep2 := state.Endpoint{
			ApplicationName: "another-application",
			Relation: charm.Relation{
				Interface: "ifce",
				Name:      "bar",
				Role:      t.role2,
				Scope:     charm.ScopeGlobal,
			},
		}
		if t.success {
			c.Assert(ep1.CanRelateTo(ep2), jc.IsTrue)
			c.Assert(ep2.CanRelateTo(ep1), jc.IsTrue)
			ep1.Interface = "different"
		}
		c.Assert(ep1.CanRelateTo(ep2), jc.IsFalse)
		c.Assert(ep2.CanRelateTo(ep1), jc.IsFalse)
	}
	ep1 := state.Endpoint{
		ApplicationName: "same-application",
		Relation: charm.Relation{
			Interface: "ifce",
			Name:      "foo",
			Role:      charm.RoleProvider,
			Scope:     charm.ScopeGlobal,
		},
	}
	ep2 := state.Endpoint{
		ApplicationName: "same-application",
		Relation: charm.Relation{
			Interface: "ifce",
			Name:      "bar",
			Role:      charm.RoleRequirer,
			Scope:     charm.ScopeGlobal,
		},
	}
	c.Assert(ep1.CanRelateTo(ep2), jc.IsFalse)
	c.Assert(ep2.CanRelateTo(ep1), jc.IsFalse)
}
