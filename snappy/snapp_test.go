// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/clickdeb"
	"github.com/ubuntu-core/snappy/policy"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/systemd"

	. "gopkg.in/check.v1"
)

type SnapTestSuite struct {
	tempdir   string
	clickhook string
	secbase   string
}

var _ = Suite(&SnapTestSuite{})

func (s *SnapTestSuite) SetUpTest(c *C) {
	s.clickhook = aaClickHookCmd
	aaClickHookCmd = "/bin/true"
	s.secbase = policy.SecBase
	s.tempdir = c.MkDir()
	newPartition = func() (p partition.Interface) {
		return new(MockPartition)
	}

	dirs.SetRootDir(s.tempdir)
	policy.SecBase = filepath.Join(s.tempdir, "security")
	os.MkdirAll(dirs.SnapServicesDir, 0755)
	os.MkdirAll(dirs.SnapSeccompDir, 0755)
	os.MkdirAll(dirs.SnapMetaDir, 0755)

	release.Override(release.Release{Flavor: "core", Series: "15.04"})

	dirs.ClickSystemHooksDir = filepath.Join(s.tempdir, "/usr/share/click/hooks")
	os.MkdirAll(dirs.ClickSystemHooksDir, 0755)

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	// we may not have debsig-verify installed (and we don't need it
	// for the unittests)
	clickdeb.VerifyCmd = "true"
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	// fake "du"
	duCmd = makeFakeDuCommand(c)

	// fake udevadm
	runUdevAdm = func(args ...string) error {
		return nil
	}

	// do not attempt to hit the real store servers in the tests
	storeSearchURI, _ = url.Parse("")
	storeDetailsURI, _ = url.Parse("")
	storeBulkURI, _ = url.Parse("")

	aaExec = filepath.Join(s.tempdir, "aa-exec")
	err := ioutil.WriteFile(aaExec, []byte(mockAaExecScript), 0755)
	c.Assert(err, IsNil)

	runScFilterGen = mockRunScFilterGen
}

func (s *SnapTestSuite) TearDownTest(c *C) {
	// ensure all functions are back to their original state
	aaClickHookCmd = s.clickhook
	policy.SecBase = s.secbase
	regenerateAppArmorRules = regenerateAppArmorRulesImpl
	ActiveSnapIterByType = activeSnapIterByTypeImpl
	duCmd = "du"
	stripGlobalRootDir = stripGlobalRootDirImpl
	runScFilterGen = runScFilterGenImpl
	runUdevAdm = runUdevAdmImpl
}

func (s *SnapTestSuite) makeInstalledMockSnap(yamls ...string) (yamlFile string, err error) {
	yaml := ""
	if len(yamls) > 0 {
		yaml = yamls[0]
	}

	return makeInstalledMockSnap(s.tempdir, yaml)
}

func makeSnapActive(packageYamlPath string) (err error) {
	snapdir := filepath.Dir(filepath.Dir(packageYamlPath))
	parent := filepath.Dir(snapdir)
	err = os.Symlink(snapdir, filepath.Join(parent, "current"))

	return err
}

func (s *SnapTestSuite) TestLocalSnapInvalidPath(c *C) {
	_, err := NewInstalledSnapPart("invalid-path", "")
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestLocalSnapSimple(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)
	c.Check(snap.Name(), Equals, "hello-app")
	c.Check(snap.Version(), Equals, "1.10")
	c.Check(snap.IsActive(), Equals, false)
	c.Check(snap.Description(), Equals, "Hello")
	c.Check(snap.IsInstalled(), Equals, true)

	services := snap.ServiceYamls()
	c.Assert(services, HasLen, 1)
	c.Assert(services[0].Name, Equals, "svc1")

	// ensure we get valid Date()
	st, err := os.Stat(snap.basedir)
	c.Assert(err, IsNil)
	c.Assert(snap.Date(), Equals, st.ModTime())

	c.Assert(snap.basedir, Equals, filepath.Join(s.tempdir, "apps", helloAppComposedName, "1.10"))
	c.Assert(snap.InstalledSize(), Not(Equals), -1)
}

func (s *SnapTestSuite) TestLocalSnapHash(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)

	hashesFile := filepath.Join(filepath.Dir(snapYaml), "hashes.yaml")
	err = ioutil.WriteFile(hashesFile, []byte("archive-sha512: F00F00"), 0644)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(snap.Hash(), Equals, "F00F00")
}

func (s *SnapTestSuite) TestLocalSnapActive(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)
	makeSnapActive(snapYaml)

	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(snap.IsActive(), Equals, true)
}

func (s *SnapTestSuite) TestLocalSnapFrameworks(c *C) {
	snapYaml, err := makeInstalledMockSnap(s.tempdir, `name: foo
version: 1.0
vendor: foo
frameworks:
 - one
 - two
`)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(err, IsNil)
	fmk, err := snap.Frameworks()
	c.Assert(err, IsNil)
	c.Check(fmk, DeepEquals, []string{"one", "two"})
}

func (s *SnapTestSuite) TestLocalSnapRepositoryInvalid(c *C) {
	snap := NewLocalSnapRepository("invalid-path")
	c.Assert(snap, IsNil)
}

func (s *SnapTestSuite) TestLocalSnapRepositorySimple(c *C) {
	yamlPath, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)
	err = makeSnapActive(yamlPath)
	c.Assert(err, IsNil)

	snap := NewLocalSnapRepository(filepath.Join(s.tempdir, "apps"))
	c.Assert(snap, NotNil)

	installed, err := snap.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)
	c.Assert(installed[0].Name(), Equals, "hello-app")
	c.Assert(installed[0].Version(), Equals, "1.10")
}

const (
	funkyAppName   = "8nzc1x4iim2xj1g2ul64"
	funkyAppOrigin = "chipaca"
	funkyAppVendor = "John Lenton"
)

/* acquired via:
curl -s -H 'accept: application/hal+json' -H "X-Ubuntu-Release: 15.04-core" -H "X-Ubuntu-Architecture: amd64" "https://search.apps.ubuntu.com/api/v1/search?q=8nzc1x4iim2xj1g2ul64&fields=publisher,package_name,origin,title,icon_url,prices,content,ratings_average,version,anon_download_url,download_url,download_sha512,last_updated,binary_filesize,support_url" | python -m json.tool
*/
const MockSearchJSON = `{
    "_embedded": {
        "clickindex:package": [
            {
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/8nzc1x4iim2xj1g2ul64.chipaca"
                    }
                },
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
                "binary_filesize": 65375,
                "content": "application",
                "download_sha512": "5364253e4a988f4f5c04380086d542f410455b97d48cc6c69ca2a5877d8aef2a6b2b2f83ec4f688cae61ebc8a6bf2cdbd4dbd8f743f0522fc76540429b79df42",
                "download_url": "https://public.apps.ubuntu.com/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/04/hello.svg_Dlrd3L4.png",
                "last_updated": "2015-04-15T18:30:16Z",
                "origin": "chipaca",
                "package_name": "8nzc1x4iim2xj1g2ul64",
                "prices": {},
                "publisher": "John Lenton",
                "ratings_average": 0.0,
                "support_url": "http://lmgtfy.com",
                "title": "Returns for store credit only.",
                "version": "42"
            }
        ]
    },
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=8nzc1x4iim2xj1g2ul64&fields=publisher,package_name,origin,title,icon_url,prices,content,ratings_average,version,anon_download_url,download_url,download_sha512,last_updated,binary_filesize,support_url"
        }
    }
}
`

/* acquired via:
curl -s --data-binary '{"name":["8nzc1x4iim2xj1g2ul64.chipaca"]}'  -H 'content-type: application/json' https://search.apps.ubuntu.com/api/v1/click-metadata
*/
const MockUpdatesJSON = `[
    {
        "status": "Published",
        "name": "8nzc1x4iim2xj1g2ul64.chipaca",
        "package_name": "8nzc1x4iim2xj1g2ul64",
        "origin": "chipaca",
        "changelog": "",
        "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/04/hello.svg_Dlrd3L4.png",
        "title": "Returns for store credit only.",
        "binary_filesize": 65375,
        "anon_download_url": "https://public.apps.ubuntu.com/anon/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
        "allow_unauthenticated": true,
        "version": "42",
        "download_url": "https://public.apps.ubuntu.com/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
        "download_sha512": "5364253e4a988f4f5c04380086d542f410455b97d48cc6c69ca2a5877d8aef2a6b2b2f83ec4f688cae61ebc8a6bf2cdbd4dbd8f743f0522fc76540429b79df42"
    }
]`

/* acquired via
   curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 15.04-core" https://search.apps.ubuntu.com/api/v1/package/8nzc1x4iim2xj1g2ul64.chipaca | python -m json.tool
*/
const MockDetailsJSON = `{
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/package/8nzc1x4iim2xj1g2ul64.chipaca"
        }
    },
    "alias": null,
    "allow_unauthenticated": true,
    "anon_download_url": "https://public.apps.ubuntu.com/anon/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
    "architecture": [
        "all"
    ],
    "binary_filesize": 65375,
    "blacklist_country_codes": [
        "AX"
    ],
    "channel": "edge",
    "changelog": "",
    "click_framework": [],
    "click_version": "0.1",
    "company_name": "",
    "content": "application",
    "date_published": "2015-04-15T18:34:40.060874Z",
    "department": [
        "food-drink"
    ],
    "description": "Returns for store credit only.\nThis is a simple hello world example.",
    "developer_name": "John Lenton",
    "download_sha512": "5364253e4a988f4f5c04380086d542f410455b97d48cc6c69ca2a5877d8aef2a6b2b2f83ec4f688cae61ebc8a6bf2cdbd4dbd8f743f0522fc76540429b79df42",
    "download_url": "https://public.apps.ubuntu.com/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
    "framework": [],
    "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/04/hello.svg_Dlrd3L4.png",
    "icon_urls": {
        "256": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/04/hello.svg_Dlrd3L4.png"
    },
    "id": 2333,
    "keywords": [],
    "last_updated": "2015-04-15T18:30:16Z",
    "license": "Proprietary",
    "name": "8nzc1x4iim2xj1g2ul64.chipaca",
    "origin": "chipaca",
    "package_name": "8nzc1x4iim2xj1g2ul64",
    "price": 0.0,
    "prices": {},
    "publisher": "John Lenton",
    "ratings_average": 0.0,
    "release": [
        "15.04-core"
    ],
    "screenshot_url": null,
    "screenshot_urls": [],
    "status": "Published",
    "stores": {
        "ubuntu": {
            "status": "Published"
        }
    },
    "support_url": "http://lmgtfy.com",
    "terms_of_service": "",
    "title": "Returns for store credit only.",
    "translations": {},
    "version": "42",
    "video_embedded_html_urls": [],
    "video_urls": [],
    "website": "",
    "whitelist_country_codes": []
}
`

/* acquired via
curl -s -H 'accept: application/hal+json' -H "X-Ubuntu-Release: 15.04-core" -H "X-Ubuntu-Architecture: amd64" "https://search.apps.ubuntu.com/api/v1/search?q=8nzc1x4iim2xj1g2ul64&fields=publisher,package_name,origin,title,icon_url,prices,content,ratings_average,version,anon_download_url,download_url,download_sha512,last_updated,binary_filesize,support_url,alias" | python -m json.tool
*/
const MockAliasSearchJSON = `{
    "_embedded": {
        "clickindex:package": [
            {
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/hello-world.canonical"
                    }
                },
                "alias": "hello-world",
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download/canonical/hello-world.canonical/hello-world.canonical_1.0.8_all.snap",
                "binary_filesize": 32409,
                "content": "application",
                "download_sha512": "70381281e979f2914851296ae70ea2f5d964724e8cebb3bdd98d2d51e07ebb19ab56c1009c3c78c91246076ba726b7abbccd97aaec40c9fdd7ac3f1025c3cf52",
                "download_url": "https://public.apps.ubuntu.com/download/canonical/hello-world.canonical/hello-world.canonical_1.0.8_all.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "last_updated": "2015-04-16T16:13:58.104820Z",
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "hello-world",
                "version": "1.0.8"
            },
            {
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/hello-world.jdstrand"
                    }
                },
                "alias": null,
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download/jdstrand/hello-world.jdstrand/hello-world.jdstrand_1.4_all.snap",
                "binary_filesize": 32487,
                "content": "application",
                "download_sha512": "1cf102ace19a5b3605038cebcfddd2778a946a7f3fb7f66a9b7d0824f01c1ee805b2d9fa5cc644270547d790e848e9eb00a23998e8c4bc517db5ef8d943448cc",
                "download_url": "https://public.apps.ubuntu.com/download/jdstrand/hello-world.jdstrand/hello-world.jdstrand_1.4_all.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg.png",
                "last_updated": "2015-04-16T15:32:09.118993Z",
                "origin": "jdstrand",
                "package_name": "hello-world",
                "prices": {},
                "publisher": "Jamie Strandboge",
                "ratings_average": 0.0,
                "support_url": "mailto:jamie@strandboge.com",
                "title": "hello-world",
                "version": "1.4"
            }
        ]
    },
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=hello-world&fields=publisher,package_name,origin,title,icon_url,prices,content,ratings_average,version,anon_download_url,download_url,download_sha512,last_updated,binary_filesize,support_url,alias"
        }
    }
}
`

const MockNoDetailsJSON = `{"errors": ["No such package"], "result": "error"}`

type MockUbuntuStoreServer struct {
	quit chan int

	searchURI string
}

func (s *SnapTestSuite) TestUbuntuStoreRepositorySearch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	storeSearchURI, err = url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	results, err := snap.Search(funkyAppName)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[funkyAppName], NotNil)

	parts := results[funkyAppName].Parts
	c.Assert(parts, HasLen, 1)
	c.Check(parts[0].Name(), Equals, funkyAppName)
	c.Check(parts[0].Origin(), Equals, funkyAppOrigin)
	c.Check(parts[0].Vendor(), Equals, funkyAppVendor)
	c.Check(parts[0].Version(), Equals, "42")
	c.Check(parts[0].Description(), Equals, "Returns for store credit only.")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryAliasSearch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, MockAliasSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	storeSearchURI, err = url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	results, err := snap.Search("hello-world")
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results["hello-world"], NotNil)

	parts := results["hello-world"].Parts
	c.Assert(parts, HasLen, 2)
	c.Check(parts[0].Name(), Equals, "hello-world")
	c.Check(parts[1].Name(), Equals, "hello-world")
	c.Check(parts[0].Origin(), Equals, "canonical")
	c.Check(parts[1].Origin(), Equals, "jdstrand")
	c.Check(parts[0].Vendor(), Equals, "Canonical")
	c.Check(parts[1].Vendor(), Equals, "Jamie Strandboge")
	c.Check(parts[0].Version(), Equals, "1.0.8")
	c.Check(parts[1].Version(), Equals, "1.4")
	c.Check(parts[0].Description(), Equals, "hello-world")
	c.Check(parts[1].Description(), Equals, "hello-world")

	alias := results["hello-world"].Alias
	c.Assert(alias, DeepEquals, parts[0])
}
func mockActiveSnapIterByType(mockSnaps []string) {
	ActiveSnapIterByType = func(f func(Part) string, snapTs ...pkg.Type) (res []string, err error) {
		return mockSnaps, nil
	}
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryUpdates(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(string(jsonReq), Equals, `{"name":["`+funkyAppName+`"]}`)
		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	storeBulkURI, err = url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// override the real ActiveSnapIterByType to return our
	// mock data
	mockActiveSnapIterByType([]string{funkyAppName})

	// the actual test
	results, err := snap.Updates()
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, funkyAppName)
	c.Assert(results[0].Version(), Equals, "42")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryUpdatesNoSnaps(c *C) {

	var err error
	storeDetailsURI, err = url.Parse("https://some-uri")
	c.Assert(err, IsNil)
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// ensure we do not hit the net if there is nothing installed
	// (otherwise the store will send us all snaps)
	snap.bulkURI = "http://i-do.not-exist.really-not"
	mockActiveSnapIterByType([]string{})

	// the actual test
	results, err := snap.Updates()
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryHeaders(c *C) {
	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Assert(err, IsNil)

	setUbuntuStoreHeaders(req)

	c.Assert(req.Header.Get("X-Ubuntu-Release"), Equals, release.String())
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no store ID by default
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "")

		c.Check(filepath.Base(r.URL.String()), Equals, funkyAppName+"."+funkyAppOrigin)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	storeDetailsURI, err = url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// the actual test
	results, err := snap.Details(funkyAppName, funkyAppOrigin)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Check(results[0].Name(), Equals, funkyAppName)
	c.Check(results[0].Origin(), Equals, funkyAppOrigin)
	c.Check(results[0].Vendor(), Equals, funkyAppVendor)
	c.Check(results[0].Version(), Equals, "42")
	c.Check(results[0].Hash(), Equals, "5364253e4a988f4f5c04380086d542f410455b97d48cc6c69ca2a5877d8aef2a6b2b2f83ec4f688cae61ebc8a6bf2cdbd4dbd8f743f0522fc76540429b79df42")
	c.Check(results[0].Date().String(), Equals, "2015-04-15 18:30:16 +0000 UTC")
	c.Check(results[0].DownloadSize(), Equals, int64(65375))
	c.Check(results[0].Channel(), Equals, "edge")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryNoDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(strings.HasSuffix(r.URL.String(), "no-such-pkg"), Equals, true)
		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	storeDetailsURI, err = url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// the actual test
	results, err := snap.Details("no-such-pkg", "")
	c.Assert(results, HasLen, 0)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestMakeConfigEnv(c *C) {
	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnapPart(yamlFile, "sergiusens")
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)

	os.Setenv("SNAP_NAME", "override-me")
	defer os.Setenv("SNAP_NAME", "")

	env := makeSnapHookEnv(snap)

	// now ensure that the environment we get back is what we want
	envMap := helpers.MakeMapFromEnvList(env)
	// regular env is unaltered
	c.Assert(envMap["PATH"], Equals, os.Getenv("PATH"))
	// SNAP_* is overriden
	c.Assert(envMap["SNAP_NAME"], Equals, "hello-app")
	c.Assert(envMap["SNAP_VERSION"], Equals, "1.10")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryInstallRemoteSnap(c *C) {
	snapPackage := makeTestSnapPackage(c, "")
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)

	iconContent := "this is an icon"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/snap":
			io.Copy(w, snapR)
		case "/icon":
			fmt.Fprintf(w, iconContent)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := RemoteSnapPart{}
	snap.pkg.AnonDownloadURL = mockServer.URL + "/snap"
	snap.pkg.IconURL = mockServer.URL + "/icon"
	snap.pkg.Name = "foo"
	snap.pkg.Origin = "bar"
	snap.pkg.Description = "this is a description"
	snap.pkg.Version = "1.0"

	p := &MockProgressMeter{}
	name, err := snap.Install(p, 0)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	st, err := os.Stat(snapPackage)
	c.Assert(err, IsNil)
	c.Assert(p.written, Equals, int(st.Size())+len(iconContent))

	installed, err := ListInstalled()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	iconPath := filepath.Join(dirs.SnapIconsDir, "foo.bar_1.0.png")
	c.Check(installed[0].Icon(), Equals, iconPath)
	c.Check(installed[0].Origin(), Equals, "bar")
	c.Check(installed[0].Description(), Equals, "this is a description")

	_, err = os.Stat(filepath.Join(dirs.SnapMetaDir, "foo.bar_1.0.manifest"))
	c.Check(err, IsNil)
}

func (s *SnapTestSuite) TestRemoteSnapUpgradeService(c *C) {
	snapPackage := makeTestSnapPackage(c, `name: foo
version: 1.0
vendor: foo
services:
 - name: svc
`)
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)

	iconContent := "icon"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/snap":
			io.Copy(w, snapR)
			snapR.Seek(0, 0)
		case "/icon":
			fmt.Fprintf(w, iconContent)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := RemoteSnapPart{}
	snap.pkg.AnonDownloadURL = mockServer.URL + "/snap"
	snap.pkg.Origin = testOrigin
	snap.pkg.IconURL = mockServer.URL + "/icon"
	snap.pkg.Name = "foo"
	snap.pkg.Origin = "bar"
	snap.pkg.Version = "1.0"

	p := &MockProgressMeter{}
	name, err := snap.Install(p, 0)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 0)

	_, err = snap.Install(p, 0)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 1)
	c.Check(p.notified[0], Matches, "Waiting for .* stop.")
}

func (s *SnapTestSuite) TestErrorOnUnsupportedArchitecture(c *C) {
	const packageHello = `name: hello-app
version: 1.10
vendor: Somebody
icon: meta/hello.svg
architectures:
    - yadayada
    - blahblah
`

	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "original", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	errorMsg := fmt.Sprintf("package's supported architectures (yadayada, blahblah) is incompatible with this system (%s)", helpers.UbuntuArchitecture())
	c.Assert(err.Error(), Equals, errorMsg)
}

func (s *SnapTestSuite) TestRemoteSnapErrors(c *C) {
	snap := RemoteSnapPart{}

	c.Assert(snap.SetActive(true, nil), Equals, ErrNotInstalled)
	c.Assert(snap.SetActive(false, nil), Equals, ErrNotInstalled)
	c.Assert(snap.Uninstall(nil), Equals, ErrNotInstalled)
}

func (s *SnapTestSuite) TestServicesWithPorts(c *C) {
	const packageHello = `name: hello-app
version: 1.10
vendor: Michael Vogt
icon: meta/hello.svg
binaries:
 - name: bin/hello
services:
 - name: svc1
   description: "Service #1"
   ports:
      external:
        ui:
          port: 8080/tcp
        nothing:
          port: 8081/tcp
          negotiable: yes
 - name: svc2
   description: "Service #2"
`

	yamlFile, err := makeInstalledMockSnap(s.tempdir, packageHello)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnapPart(yamlFile, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)

	c.Assert(snap.Name(), Equals, "hello-app")
	c.Assert(snap.Origin(), Equals, testOrigin)
	c.Assert(snap.Vendor(), Equals, "Michael Vogt")
	c.Assert(snap.Version(), Equals, "1.10")
	c.Assert(snap.IsActive(), Equals, false)

	services := snap.ServiceYamls()
	c.Assert(services, HasLen, 2)

	c.Assert(services[0].Name, Equals, "svc1")
	c.Assert(services[0].Description, Equals, "Service #1")

	external1Ui, ok := services[0].Ports.External["ui"]
	c.Assert(ok, Equals, true)
	c.Assert(external1Ui.Port, Equals, "8080/tcp")
	c.Assert(external1Ui.Negotiable, Equals, false)

	external1Nothing, ok := services[0].Ports.External["nothing"]
	c.Assert(ok, Equals, true)
	c.Assert(external1Nothing.Port, Equals, "8081/tcp")
	c.Assert(external1Nothing.Negotiable, Equals, true)

	c.Assert(services[1].Name, Equals, "svc2")
	c.Assert(services[1].Description, Equals, "Service #2")

	// ensure we get valid Date()
	st, err := os.Stat(snap.basedir)
	c.Assert(err, IsNil)
	c.Assert(snap.Date(), Equals, st.ModTime())

	c.Assert(snap.basedir, Equals, filepath.Join(s.tempdir, "apps", helloAppComposedName, "1.10"))
	c.Assert(snap.InstalledSize(), Not(Equals), -1)
}

func (s *SnapTestSuite) TestPackageYamlMultipleArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
architecture: [i386, armhf]
`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"i386", "armhf"})
}

func (s *SnapTestSuite) TestPackageYamlSingleArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
architecture: i386
`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"i386"})
}

func (s *SnapTestSuite) TestPackageYamlNoArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"all"})
}

func (s *SnapTestSuite) TestPackageYamlBadArchitectureParsing(c *C) {
	data := []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
architecture:
  armhf:
    no
`)
	_, err := parsePackageYamlData(data, false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestPackageYamlWorseArchitectureParsing(c *C) {
	data := []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
architecture:
  - armhf:
      sometimes
`)
	_, err := parsePackageYamlData(data, false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestPackageYamlLicenseParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`
name: foo
version: 1.0
vendor: foo
explicit-license-agreement: Y`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.ExplicitLicenseAgreement, Equals, true)
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryOemStoreId(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ensure we get the right header
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Assert(storeID, Equals, "my-store")
		w.WriteHeader(404)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	// install custom oem snap with store-id
	packageYaml, err := makeInstalledMockSnap(s.tempdir, `name: oem-test
version: 1.0
vendor: mvo
oem:
  store:
    id: my-store
type: oem
`)
	c.Assert(err, IsNil)
	makeSnapActive(packageYaml)

	storeDetailsURI, err = url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	repo := NewUbuntuStoreSnapRepository()
	c.Assert(repo, NotNil)

	// we just ensure that the right header is set
	repo.Details("xkcd", "")
}

func (s *SnapTestSuite) TestUninstallBuiltIn(c *C) {
	// install custom oem snap with store-id
	oemYaml, err := makeInstalledMockSnap(s.tempdir, `name: oem-test
version: 1.0
vendor: mvo
oem:
  store:
    id: my-store
  software:
    built-in:
      - hello-app
type: oem
`)
	c.Assert(err, IsNil)
	makeSnapActive(oemYaml)

	packageYaml, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	makeSnapActive(packageYaml)

	p := &MockProgressMeter{}

	snap := NewLocalSnapRepository(filepath.Join(s.tempdir, "apps"))
	c.Assert(snap, NotNil)
	installed, err := snap.Installed()
	c.Assert(err, IsNil)
	parts := FindSnapsByName("hello-app", installed)
	c.Assert(parts, HasLen, 1)
	c.Check(parts[0].Uninstall(p), Equals, ErrPackageNotRemovable)
}

var securityBinaryPackageYaml = []byte(`name: test-snap
version: 1.2.8
vendor: Jamie Strandboge <jamie@canonical.com>
icon: meta/hello.svg
binaries:
 - name: testme
   exec: bin/testme
   description: "testme client"
   caps:
     - "foo_group"
   security-template: "foo_template"
 - name: testme-override
   exec: bin/testme-override
   security-override:
     apparmor: meta/testme-override.apparmor
 - name: testme-policy
   exec: bin/testme-policy
   security-policy:
     apparmor: meta/testme-policy.profile
`)

func (s *SnapTestSuite) TestPackageYamlSecurityBinaryParsing(c *C) {
	m, err := parsePackageYamlData(securityBinaryPackageYaml, false)
	c.Assert(err, IsNil)

	c.Assert(m.Binaries[0].Name, Equals, "testme")
	c.Assert(m.Binaries[0].Exec, Equals, "bin/testme")
	c.Assert(m.Binaries[0].SecurityCaps, HasLen, 1)
	c.Assert(m.Binaries[0].SecurityCaps[0], Equals, "foo_group")
	c.Assert(m.Binaries[0].SecurityTemplate, Equals, "foo_template")

	c.Assert(m.Binaries[1].Name, Equals, "testme-override")
	c.Assert(m.Binaries[1].Exec, Equals, "bin/testme-override")
	c.Assert(m.Binaries[1].SecurityCaps, HasLen, 0)
	c.Assert(m.Binaries[1].SecurityOverride.Apparmor, Equals, "meta/testme-override.apparmor")

	c.Assert(m.Binaries[2].Name, Equals, "testme-policy")
	c.Assert(m.Binaries[2].Exec, Equals, "bin/testme-policy")
	c.Assert(m.Binaries[2].SecurityCaps, HasLen, 0)
	c.Assert(m.Binaries[2].SecurityPolicy.Apparmor, Equals, "meta/testme-policy.profile")
}

var securityServicePackageYaml = []byte(`name: test-snap
version: 1.2.8
vendor: Jamie Strandboge <jamie@canonical.com>
icon: meta/hello.svg
services:
 - name: testme-service
   start: bin/testme-service.start
   stop: bin/testme-service.stop
   description: "testme service"
   caps:
     - "network-client"
     - "foo_group"
   security-template: "foo_template"
`)

func (s *SnapTestSuite) TestPackageYamlSecurityServiceParsing(c *C) {
	m, err := parsePackageYamlData(securityServicePackageYaml, false)
	c.Assert(err, IsNil)

	c.Assert(m.ServiceYamls[0].Name, Equals, "testme-service")
	c.Assert(m.ServiceYamls[0].Start, Equals, "bin/testme-service.start")
	c.Assert(m.ServiceYamls[0].Stop, Equals, "bin/testme-service.stop")
	c.Assert(m.ServiceYamls[0].SecurityCaps, HasLen, 2)
	c.Assert(m.ServiceYamls[0].SecurityCaps[0], Equals, "network-client")
	c.Assert(m.ServiceYamls[0].SecurityCaps[1], Equals, "foo_group")
	c.Assert(m.ServiceYamls[0].SecurityTemplate, Equals, "foo_template")
}

func (s *SnapTestSuite) TestPackageYamlFrameworkParsing(c *C) {
	m, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
vendor: foo
framework: one, two
`), false)
	c.Assert(err, IsNil)
	c.Assert(m.Frameworks, HasLen, 2)
	c.Check(m.Frameworks, DeepEquals, []string{"one", "two"})
	c.Check(m.FrameworksForClick(), Matches, "one,two,ubuntu-core.*")
}

func (s *SnapTestSuite) TestPackageYamlFrameworksParsing(c *C) {
	m, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
vendor: foo
frameworks:
 - one
 - two
`), false)
	c.Assert(err, IsNil)
	c.Assert(m.Frameworks, HasLen, 2)
	c.Check(m.Frameworks, DeepEquals, []string{"one", "two"})
	c.Check(m.FrameworksForClick(), Matches, "one,two,ubuntu-core.*")
}

func (s *SnapTestSuite) TestPackageYamlFrameworkAndFrameworksFails(c *C) {
	_, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
vendor: foo
frameworks:
 - one
 - two
framework: three, four
`), false)
	c.Assert(err, Equals, ErrInvalidFrameworkSpecInYaml)
}

func (s *SnapTestSuite) TestDetectsAlreadyInstalled(c *C) {
	data := "name: afoo\nversion: 1\nvendor: foo"
	yamlPath, err := makeInstalledMockSnap(s.tempdir, data)
	c.Assert(err, IsNil)
	c.Assert(makeSnapActive(yamlPath), IsNil)

	yaml, err := parsePackageYamlData([]byte(data), false)
	c.Assert(err, IsNil)
	c.Check(yaml.checkForPackageInstalled("otherns"), Equals, ErrPackageNameAlreadyInstalled)
}

func (s *SnapTestSuite) TestIgnoresAlreadyInstalledSameOrigin(c *C) {
	// NOTE remote snaps are stopped before clickInstall gets to run

	data := "name: afoo\nversion: 1\nvendor: foo"
	yamlPath, err := makeInstalledMockSnap(s.tempdir, data)
	c.Assert(err, IsNil)
	c.Assert(makeSnapActive(yamlPath), IsNil)

	yaml, err := parsePackageYamlData([]byte(data), false)
	c.Assert(err, IsNil)
	c.Check(yaml.checkForPackageInstalled(testOrigin), IsNil)
}

func (s *SnapTestSuite) TestIgnoresAlreadyInstalledFrameworkSameOrigin(c *C) {
	data := "name: afoo\nversion: 1\nvendor: foo\ntype: framework"
	yamlPath, err := makeInstalledMockSnap(s.tempdir, data)
	c.Assert(err, IsNil)
	c.Assert(makeSnapActive(yamlPath), IsNil)

	yaml, err := parsePackageYamlData([]byte(data), false)
	c.Assert(err, IsNil)
	c.Check(yaml.checkForPackageInstalled(testOrigin), IsNil)
}

func (s *SnapTestSuite) TestDetectsAlreadyInstalledFramework(c *C) {
	data := "name: afoo\nversion: 1\nvendor: foo\ntype: framework"
	yamlPath, err := makeInstalledMockSnap(s.tempdir, data)
	c.Assert(err, IsNil)
	c.Assert(makeSnapActive(yamlPath), IsNil)

	yaml, err := parsePackageYamlData([]byte(data), false)
	c.Assert(err, IsNil)
	c.Check(yaml.checkForPackageInstalled("otherns"), Equals, ErrPackageNameAlreadyInstalled)
}

func (s *SnapTestSuite) TestUsesStoreMetaData(c *C) {
	data := "name: afoo\nversion: 1\nvendor: foo\ntype: framework"
	yamlPath, err := makeInstalledMockSnap(s.tempdir, data)
	c.Assert(err, IsNil)
	c.Assert(makeSnapActive(yamlPath), IsNil)

	err = os.MkdirAll(dirs.SnapMetaDir, 0755)
	c.Assert(err, IsNil)

	data = "name: afoo\nalias: afoo\ndescription: something nice\ndownloadsize: 10\norigin: someplace"
	err = ioutil.WriteFile(filepath.Join(dirs.SnapMetaDir, "afoo_1.manifest"), []byte(data), 0644)
	c.Assert(err, IsNil)

	snaps, err := ListInstalled()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)

	c.Check(snaps[0].Name(), Equals, "afoo")
	c.Check(snaps[0].Version(), Equals, "1")
	c.Check(snaps[0].Type(), Equals, pkg.TypeFramework)
	c.Check(snaps[0].Origin(), Equals, "someplace")
	c.Check(snaps[0].Description(), Equals, "something nice")
	c.Check(snaps[0].DownloadSize(), Equals, int64(10))
}

func (s *SnapTestSuite) TestDetectsNameClash(c *C) {
	data := []byte(`name: afoo
version: 1.0
vendor: foo
services:
 - name: foo
binaries:
 - name: foo
`)
	yaml, err := parsePackageYamlData(data, false)
	c.Assert(err, IsNil)
	err = yaml.checkForNameClashes()
	c.Assert(err, ErrorMatches, ".*binary and service both called foo.*")
}

func (s *SnapTestSuite) TestDetectsMissingFrameworks(c *C) {
	data := []byte(`name: afoo
version: 1.0
vendor: foo
frameworks:
 - missing
 - also-missing
`)
	yaml, err := parsePackageYamlData(data, false)
	c.Assert(err, IsNil)
	err = yaml.checkForFrameworks()
	c.Assert(err, ErrorMatches, `missing frameworks: missing, also-missing`)
}

func (s *SnapTestSuite) TestDetectsFrameworksInUse(c *C) {
	_, err := makeInstalledMockSnap(s.tempdir, `name: foo
version: 1.0
vendor: foo
frameworks:
 - fmk
`)
	c.Assert(err, IsNil)

	yaml, err := parsePackageYamlData([]byte(`name: fmk
version: 1.0
vendor: foo
type: framework`), false)
	c.Assert(err, IsNil)
	part := &SnapPart{m: yaml}
	deps, err := part.Dependents()
	c.Assert(err, IsNil)
	c.Check(deps, HasLen, 1)
	c.Check(deps[0].Name(), Equals, "foo")

	names, err := part.DependentNames()
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"foo"})
}

func (s *SnapTestSuite) TestRefreshDependentsSecurity(c *C) {
	oldDir := dirs.SnapAppArmorDir
	defer func() {
		dirs.SnapAppArmorDir = oldDir
		timestampUpdater = helpers.UpdateTimestamp
	}()
	touched := []string{}
	dirs.SnapAppArmorDir = c.MkDir()
	fn := filepath.Join(dirs.SnapAppArmorDir, "foo."+testOrigin+"_hello_1.0.json")
	c.Assert(os.Symlink(fn, fn), IsNil)
	timestampUpdater = func(s string) error {
		touched = append(touched, s)
		return nil
	}

	_, err := makeInstalledMockSnap(s.tempdir, `name: foo
version: 1.0
vendor: foo
frameworks:
 - fmk
binaries:
 - name: hello
   security-override:
    apparmor: fmk_foo
`)
	c.Assert(err, IsNil)

	d1 := c.MkDir()
	d2 := c.MkDir()
	dp := filepath.Join("meta", "framework-policy", "apparmor", "policygroups")

	yaml := "name: fmk\ntype: framework\nversion: 1\nvendor: foo"
	_, err = makeInstalledMockSnap(d1, yaml)
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d1, dp), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d1, dp, "foo"), []byte(""), 0644), IsNil)

	_, err = makeInstalledMockSnap(d2, "name: fmk\ntype: framework\nversion: 2\nvendor: foo")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d2, dp), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d2, dp, "foo"), []byte("x"), 0644), IsNil)

	pb := &MockProgressMeter{}
	m, err := parsePackageYamlData([]byte(yaml), false)
	part := &SnapPart{m: m, origin: testOrigin, basedir: d1}
	c.Assert(part.RefreshDependentsSecurity(&SnapPart{basedir: d2}, pb), IsNil)
	c.Check(touched, DeepEquals, []string{fn})
}

func (s *SnapTestSuite) TestRemoveChecksFrameworks(c *C) {
	yamlFile, err := makeInstalledMockSnap(s.tempdir, `name: fmk
version: 1.0
vendor: foo
type: framework`)
	c.Assert(err, IsNil)
	yaml, err := parsePackageYamlFile(yamlFile)

	_, err = makeInstalledMockSnap(s.tempdir, `name: foo
version: 1.0
vendor: foo
frameworks:
 - fmk
`)
	c.Assert(err, IsNil)

	part := &SnapPart{m: yaml, origin: testOrigin}
	err = part.Uninstall(new(MockProgressMeter))
	c.Check(err, ErrorMatches, `framework still in use by: foo`)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateSecurityPolicy(c *C) {
	// if a security policy is defined, never flag for update
	sd := &SecurityDefinitions{SecurityPolicy: &SecurityPolicyDefinition{}}
	c.Check(sd.NeedsAppArmorUpdate(nil, nil), Equals, false)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateSecurityOverride(c *C) {
	// if a security override is defined, always flag for update
	sd := &SecurityDefinitions{SecurityOverride: &SecurityOverrideDefinition{}}
	c.Check(sd.NeedsAppArmorUpdate(nil, nil), Equals, true)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateTemplatePresent(c *C) {
	// if the template is in the map, it needs updating
	sd := &SecurityDefinitions{SecurityTemplate: "foo_bar"}
	c.Check(sd.NeedsAppArmorUpdate(nil, map[string]bool{"foo_bar": true}), Equals, true)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateTemplateAbsent(c *C) {
	// if the template is not in the map, it does not
	sd := &SecurityDefinitions{SecurityTemplate: "foo_bar"}
	c.Check(sd.NeedsAppArmorUpdate(nil, nil), Equals, false)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdatePolicyPresent(c *C) {
	// if the cap is in the map, it needs updating
	sd := &SecurityDefinitions{SecurityCaps: []string{"foo_bar"}}
	c.Check(sd.NeedsAppArmorUpdate(map[string]bool{"foo_bar": true}, nil), Equals, true)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdatePolicyAbsent(c *C) {
	// if the cap is not in the map, it does not
	sd := &SecurityDefinitions{SecurityCaps: []string{"foo_quux"}}
	c.Check(sd.NeedsAppArmorUpdate(map[string]bool{"foo_bar": true}, nil), Equals, false)
}

func (s *SnapTestSuite) TestRequestAppArmorUpdateService(c *C) {
	var updated []string
	timestampUpdater = func(s string) error {
		updated = append(updated, s)
		return nil
	}
	defer func() { timestampUpdater = helpers.UpdateTimestamp }()
	// if one of the services needs updating, it's updated and returned
	svc := ServiceYaml{Name: "svc", SecurityDefinitions: SecurityDefinitions{SecurityTemplate: "foo"}}
	part := &SnapPart{m: &packageYaml{Name: "part", ServiceYamls: []ServiceYaml{svc}, Version: "42"}, origin: testOrigin}
	err := part.RequestAppArmorUpdate(nil, map[string]bool{"foo": true})
	c.Assert(err, IsNil)
	c.Assert(updated, HasLen, 1)
	c.Check(filepath.Base(updated[0]), Equals, "part."+testOrigin+"_svc_42.json")
}

func (s *SnapTestSuite) TestRequestAppArmorUpdateBinary(c *C) {
	var updated []string
	timestampUpdater = func(s string) error {
		updated = append(updated, s)
		return nil
	}
	defer func() { timestampUpdater = helpers.UpdateTimestamp }()
	// if one of the binaries needs updating, the part needs updating
	bin := Binary{Name: "echo", SecurityDefinitions: SecurityDefinitions{SecurityTemplate: "foo"}}
	part := &SnapPart{m: &packageYaml{Name: "part", Binaries: []Binary{bin}, Version: "42"}, origin: testOrigin}
	err := part.RequestAppArmorUpdate(nil, map[string]bool{"foo": true})
	c.Assert(err, IsNil)
	c.Assert(updated, HasLen, 1)
	c.Check(filepath.Base(updated[0]), Equals, "part."+testOrigin+"_echo_42.json")
}

func (s *SnapTestSuite) TestRequestAppArmorUpdateNothing(c *C) {
	var updated []string
	timestampUpdater = func(s string) error {
		updated = append(updated, s)
		return nil
	}
	defer func() { timestampUpdater = helpers.UpdateTimestamp }()
	svc := ServiceYaml{Name: "svc", SecurityDefinitions: SecurityDefinitions{SecurityTemplate: "foo"}}
	bin := Binary{Name: "echo", SecurityDefinitions: SecurityDefinitions{SecurityTemplate: "foo"}}
	part := &SnapPart{m: &packageYaml{ServiceYamls: []ServiceYaml{svc}, Binaries: []Binary{bin}, Version: "42"}, origin: testOrigin}
	err := part.RequestAppArmorUpdate(nil, nil)
	c.Check(err, IsNil)
	c.Check(updated, HasLen, 0)
}

func (s *SnapTestSuite) TestDetectIllegalYamlBinaries(c *C) {
	_, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
binaries:
 - name: tes!me
   exec: something
`), false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestDetectIllegalYamlService(c *C) {
	_, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
services:
 - name: tes!me
   start: something
`), false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestOriginFromPath(c *C) {
	n, err := originFromYamlPath("/oem/foo.bar/1.0/meta/package.yaml")
	c.Check(err, IsNil)
	c.Check(n, Equals, "bar")

	n, err = originFromYamlPath("/oem/foo_bar/1.0/meta/package.yaml")
	c.Check(err, NotNil)
	c.Check(n, Equals, "")

	n, err = originFromYamlPath("/oo_bar/1.0/mpackage.yaml")
	c.Check(err, NotNil)
	c.Check(n, Equals, "")
}

func (s *SnapTestSuite) TestStructFields(c *C) {
	type t struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(getStructFields(t{}), DeepEquals, []string{"hello", "potato"})
}

func (s *SnapTestSuite) TestStructFieldsSurvivesNoTag(c *C) {
	type t struct {
		Foo int `json:"hello"`
		Bar int
	}
	c.Assert(getStructFields(t{}), DeepEquals, []string{"hello"})
}

func (s *SnapTestSuite) TestIllegalPackageNameWithOrigin(c *C) {
	_, err := parsePackageYamlData([]byte(`name: foo.something
version: 1.0
vendor: foo
`), false)

	c.Assert(err, Equals, ErrPackageNameNotSupported)
}

var hardwareYaml = []byte(`name: oem-foo
version: 1.0
vendor: someone
oem:
 hardware:
  assign:
   - part-id: device-hive-iot-hal
     rules:
     - kernel: ttyUSB0
     - subsystem: tty
       with-subsystems: usb-serial
       with-driver: pl2303
       with-attrs:
       - idVendor=0xf00f00
       - idProduct=0xb00
       with-props:
       - BAUD=9600
       - META1=foo*
       - META2=foo?
       - META3=foo[a-z]
       - META4=a|b
`)

func (s *SnapTestSuite) TestParseHardwareYaml(c *C) {
	m, err := parsePackageYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)
	c.Assert(m.OEM.Hardware.Assign[0].PartID, Equals, "device-hive-iot-hal")
	c.Assert(m.OEM.Hardware.Assign[0].Rules[0].Kernel, Equals, "ttyUSB0")
	c.Assert(m.OEM.Hardware.Assign[0].Rules[1].Subsystem, Equals, "tty")
	c.Assert(m.OEM.Hardware.Assign[0].Rules[1].WithDriver, Equals, "pl2303")
	c.Assert(m.OEM.Hardware.Assign[0].Rules[1].WithAttrs[0], Equals, "idVendor=0xf00f00")
	c.Assert(m.OEM.Hardware.Assign[0].Rules[1].WithAttrs[1], Equals, "idProduct=0xb00")
}

var expectedUdevRule = `KERNEL=="ttyUSB0", TAG:="snappy-assign", ENV{SNAPPY_APP}:="device-hive-iot-hal"

SUBSYSTEM=="tty", SUBSYSTEMS=="usb-serial", DRIVER=="pl2303", ATTRS{idVendor}=="0xf00f00", ATTRS{idProduct}=="0xb00", ENV{BAUD}=="9600", ENV{META1}=="foo*", ENV{META2}=="foo?", ENV{META3}=="foo[a-z]", ENV{META4}=="a|b", TAG:="snappy-assign", ENV{SNAPPY_APP}:="device-hive-iot-hal"

`

func (s *SnapTestSuite) TestGenerateHardwareYamlData(c *C) {
	m, err := parsePackageYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	output, err := m.OEM.Hardware.Assign[0].generateUdevRuleContent()
	c.Assert(err, IsNil)

	c.Assert(output, Equals, expectedUdevRule)
}

func (s *SnapTestSuite) TestWriteHardwareUdevEtc(c *C) {
	m, err := parsePackageYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	dirs.SnapUdevRulesDir = c.MkDir()
	writeOemHardwareUdevRules(m)

	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapUdevRulesDir, "80-snappy_oem-foo_device-hive-iot-hal.rules")), Equals, true)
}

func (s *SnapTestSuite) TestWriteHardwareUdevCleanup(c *C) {
	m, err := parsePackageYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	dirs.SnapUdevRulesDir = c.MkDir()
	udevRulesFile := filepath.Join(dirs.SnapUdevRulesDir, "80-snappy_oem-foo_device-hive-iot-hal.rules")
	c.Assert(ioutil.WriteFile(udevRulesFile, nil, 0644), Equals, nil)
	cleanupOemHardwareUdevRules(m)

	c.Assert(helpers.FileExists(udevRulesFile), Equals, false)
}

func (s *SnapTestSuite) TestWriteHardwareUdevActivate(c *C) {
	type aCmd []string
	var cmds = []aCmd{}

	runUdevAdm = func(args ...string) error {
		cmds = append(cmds, args)
		return nil
	}
	defer func() { runUdevAdm = runUdevAdmImpl }()

	err := activateOemHardwareUdevRules()
	c.Assert(err, IsNil)
	c.Assert(cmds[0], DeepEquals, aCmd{"udevadm", "control", "--reload-rules"})
	c.Assert(cmds[1], DeepEquals, aCmd{"udevadm", "trigger"})
	c.Assert(cmds, HasLen, 2)
}

func (s *SnapTestSuite) TestLegacyConfigHook(c *C) {
	packageYaml, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
vendor: Foo Bar <foo@example.com>
`), true)
	c.Assert(err, IsNil)
	c.Check(packageYaml.Integration["snappy-config"], DeepEquals, clickAppHook{"apparmor": "meta/snappy-config.apparmor"})
}

func (s *SnapTestSuite) TestQualifiedNameName(c *C) {
	packageYaml, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`), false)
	c.Assert(err, IsNil)

	udevName := packageYaml.qualifiedName("mvo")
	c.Assert(udevName, Equals, "foo.mvo")
}

func (s *SnapTestSuite) TestQualifiedNameNameFramework(c *C) {
	packageYaml, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
icon: foo.svg
type: framework
vendor: Foo Bar <foo@example.com>
`), false)
	c.Assert(err, IsNil)

	udevName := packageYaml.qualifiedName("")
	c.Assert(udevName, Equals, "foo")
}

func (s *SnapTestSuite) TestParsePackageYamlDataChecksName(c *C) {
	_, err := parsePackageYamlData([]byte(`
version: 1.0
vendor: Foo Bar <foo@example.com>
`), false)
	c.Assert(err, ErrorMatches, "can not parse package.yaml: missing required fields 'name'.*")
}

func (s *SnapTestSuite) TestParsePackageYamlDataChecksVersion(c *C) {
	_, err := parsePackageYamlData([]byte(`
name: foo
vendor: Foo Bar <foo@example.com>
`), false)
	c.Assert(err, ErrorMatches, "can not parse package.yaml: missing required fields 'version'.*")
}

func (s *SnapTestSuite) TestParsePackageYamlDataChecksVendor(c *C) {
	_, err := parsePackageYamlData([]byte(`
name: foo
version: 1.0
`), false)
	c.Assert(err, ErrorMatches, "can not parse package.yaml: missing required fields 'vendor'.*")
}

func (s *SnapTestSuite) TestParsePackageYamlDataChecksMultiple(c *C) {
	_, err := parsePackageYamlData([]byte(`
`), false)
	c.Assert(err, ErrorMatches, "can not parse package.yaml: missing required fields 'name, version, vendor'.*")
}

func (s *SnapTestSuite) TestIntegrateBoring(c *C) {
	m := &packageYaml{}
	m.legacyIntegration(false)

	// no binaries, no service, no legacyIntegration
	c.Check(m.Integration, HasLen, 0)
}

func (s *SnapTestSuite) TestIntegrateConfig(c *C) {
	m := &packageYaml{}
	m.legacyIntegration(true)

	// no binaries, no service, but config! => legacyIntegration
	c.Check(m.Integration, HasLen, 1)
	c.Check(m.Integration["snappy-config"], DeepEquals, clickAppHook{"apparmor": "meta/snappy-config.apparmor"})
}

func (s *SnapTestSuite) TestIntegrateBinary(c *C) {
	m := &packageYaml{
		Binaries: []Binary{
			{
				Name: "testme",
				Exec: "bin/testme",
			},
			{
				Name: "testme-override",
				Exec: "bin/testme-override",
				SecurityDefinitions: SecurityDefinitions{
					SecurityOverride: &SecurityOverrideDefinition{Apparmor: "meta/testme-override.apparmor"},
				},
			},
			{
				Name: "testme-policy",
				Exec: "bin/testme-policy",
				SecurityDefinitions: SecurityDefinitions{
					SecurityPolicy: &SecurityPolicyDefinition{Apparmor: "meta/testme-policy.profile"},
				},
			},
		},
	}
	m.legacyIntegration(false)

	c.Check(m.Integration, DeepEquals, map[string]clickAppHook{
		"testme": {
			"apparmor": "meta/testme.apparmor",
			"bin-path": "bin/testme",
		},
		"testme-override": {
			"apparmor": "meta/testme-override.apparmor",
			"bin-path": "bin/testme-override",
		},
		"testme-policy": {
			"apparmor-profile": "meta/testme-policy.profile",
			"bin-path":         "bin/testme-policy",
		},
	})
}

func (s *SnapTestSuite) TestIntegrateService(c *C) {
	m := &packageYaml{
		ServiceYamls: []ServiceYaml{
			{
				Name: "svc",
			},
		},
	}

	m.legacyIntegration(false)

	// no binaries, no service, no integrate
	c.Check(m.Integration, DeepEquals, map[string]clickAppHook{
		"svc": clickAppHook{
			"apparmor": "meta/svc.apparmor",
		}})
}

func (s *SnapTestSuite) TestCpiURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", ""), IsNil)
	before := cpiURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_CPI", "")
	after := cpiURL()

	c.Check(before, Not(Equals), after)
}

func (s *SnapTestSuite) TestChannelFromLocalManifest(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(snap.Channel(), Equals, "remote-channel")
}
