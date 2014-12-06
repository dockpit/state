package state_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/bmizerany/assert"
	"github.com/fsouza/go-dockerclient"

	"github.com/dockpit/pit/config"
	"github.com/dockpit/state"
)

type configMock struct{}

func (c *configMock) DependencyConfigs() []config.DependencyC  { return []config.DependencyC{} }
func (c *configMock) ProviderConfigs() []config.StateProviderC { return []config.StateProviderC{} }
func (c *configMock) PortBindingsForDep(dep string) map[docker.Port][]docker.PortBinding {
	return map[docker.Port][]docker.PortBinding{}
}
func (c *configMock) PortBindingsForState(pname string) map[docker.Port][]docker.PortBinding {
	return map[docker.Port][]docker.PortBinding{}
}

func getmanager(t *testing.T) *state.Manager {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	h := os.Getenv("DOCKER_HOST")
	if h == "" {
		t.Skip("No DOCKER_HOST env variable setup")
	}

	cert := os.Getenv("DOCKER_CERT_PATH")
	if cert == "" {
		t.Skip("No DOCKER_CERT_PATH env variable setup")
	}

	conf := &configMock{}

	m, err := state.NewManager(h, cert, filepath.Join(wd, "docs", "states"), conf)
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func TestBuild(t *testing.T) {
	m := getmanager(t)
	out := bytes.NewBuffer(nil)

	iname, err := m.Build("mysql", "a single user", out)
	if err != nil {
		t.Fatal(err)
	}

	match, err := regexp.MatchString(`(?s).*Successfully built.*`, out.String())
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, `pitstate_mysql_c189303ee6bcedc685646c70a493ed27`, iname)
	assert.NotEqual(t, false, match, fmt.Sprintf("unexpected build output: %s", out.String()))

	//then start it
	err = m.Start("mysql", "a single user")
	if err != nil {
		t.Fatal(err)
	}

	//@todo test if online

	//then stop it
	err = m.Stop("mysql", "a single user")
	if err != nil {
		t.Fatal(err)
	}

}
