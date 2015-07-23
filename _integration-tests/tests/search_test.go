// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package tests

import (
	. "launchpad.net/snappy/_integration-tests/common"

	. "gopkg.in/check.v1"
)

var _ = Suite(&searchSuite{})

type searchSuite struct {
	SnappySuite
}

func (s *searchSuite) TestSearchFrameworkMustPrintMatch(c *C) {
	searchOutput := ExecCommand(c, "snappy", "search", "hello-dbus-fwk")

	expected := "(?ms)" +
		"Name +Version +Summary *\n" +
		".*" +
		"^hello-dbus-fwk +.* +hello-dbus-fwk *\n" +
		".*"

	c.Assert(searchOutput, Matches, expected)
}