package state_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"labix.org/v2/mgo"

	"github.com/dockpit/pit/config"
	"github.com/dockpit/state"
)

var mongo_configd = &config.ConfigData{
	StateProviders: map[string]*config.StateProviderConfigData{
		"mongo": &config.StateProviderConfigData{
			Ports:        []string{"27017:30000"},
			ReadyPattern: ".*waiting for connections.*",
			ReadyTimeout: "1s",                    //limit to make sure it times out when journalling
			Cmd:          []string{"--nojournal"}, //mongo would normally timeout because of journaling
		},
	},
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

	conf, err := config.Parse(mongo_configd)
	if err != nil {
		t.Fatal(err)
	}

	m, err := state.NewManager(h, cert, filepath.Join(wd, ".example", "states"), conf)
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func TestBuild(t *testing.T) {
	m := getmanager(t)
	out := bytes.NewBuffer(nil)

	iname, err := m.Build("mongo", "several users", out)
	if err != nil {
		t.Fatal(err)
	}

	match, err := regexp.MatchString(`(?s).*Successfully built.*`, out.String())
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, `pitstate_mongo_a9e71e2d929f3305165ed2fc4d5b25a3`, iname)
	assert.NotEqual(t, false, match, fmt.Sprintf("unexpected build output: %s", out.String()))

	//then start it
	sc, err := m.Start("mongo", "several users")
	if err != nil {
		t.Fatal(err)
	}

	//then stop it
	defer func() {
		err = m.Stop("mongo", "several users")
		if err != nil {
			t.Fatal(err)
		}
	}()

	//test if online, if not timeout after 100ms
	_, err = mgo.DialWithTimeout("mongodb://"+sc.Host+":30000", time.Millisecond*100)
	if err != nil {
		t.Fatal(err)
	}

}
