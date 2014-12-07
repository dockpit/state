package state

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"time"

	"github.com/dockpit/go-dockerclient"

	"github.com/dockpit/dirtar"
	"github.com/dockpit/pit/config"
)

type Manager struct {
	Dir string

	client *docker.Client
	conf   config.C
}

//configuration stuff? @todo move to config
var ReadyInterval = time.Millisecond * 50

// Manages state for microservice testing by creating
// docker images and starting containers when necessary
func NewManager(host, cert, path string, conf config.C) (*Manager, error) {
	m := &Manager{
		Dir: path,

		conf: conf,
	}

	//parse docker host addr as url
	hurl, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	//change to http connection
	hurl.Scheme = "https"

	//create docker client
	m.client, err = docker.NewTLSClient(hurl.String(), filepath.Join(cert, "cert.pem"), filepath.Join(cert, "key.pem"), filepath.Join(cert, "ca.pem"))
	if err != nil {
		return nil, err
	}

	return m, nil
}

//turn provider and statename into a path
func (m *Manager) ContextPath(pname, sname string) string {
	return filepath.Join(m.Dir, pname, fmt.Sprintf("'%s'", sname))
}

// generate an unique image name based on the provider name and path to the state folder
func (m *Manager) ImageName(pname, spath string) (string, error) {

	//create md5 of full path
	hash := md5.New()
	_, err := hash.Write([]byte(spath))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("pitstate_%s_%s", pname, hex.EncodeToString(hash.Sum(nil))), nil
}

// Build a state by buildign Docker images
func (m *Manager) Build(pname, sname string, out io.Writer) (string, error) {

	//create writers
	in := bytes.NewBuffer(nil)

	//expected context path for the state
	root := m.ContextPath(pname, sname)

	//tar directory
	err := dirtar.Tar(root, in)
	if err != nil {
		return "", err
	}

	// generate an unique image name based on the provider name and path to the state folder
	iname, err := m.ImageName(pname, root)
	if err != nil {
		return "", err
	}

	//name after provider and hash of state name
	bopts := docker.BuildImageOptions{
		Name:         iname,
		InputStream:  in,
		OutputStream: out,
	}

	//issue build command to docker host
	if err := m.client.BuildImage(bopts); err != nil {
		return "", err
	}

	return iname, nil
}

// Start a state by running a Docker container from an image
func (m *Manager) Start(pname, sname string) error {

	//determine image name by path
	root := m.ContextPath(pname, sname)
	iname, err := m.ImageName(pname, root)
	if err != nil {
		return err
	}

	//get state provider conf
	spconf := m.conf.StateProviderConfig(pname)
	if spconf == nil {
		return fmt.Errorf("No configuration for state provider: %s", pname)
	}

	//create the container
	c, err := m.client.CreateContainer(docker.CreateContainerOptions{
		Name: iname,
		Config: &docker.Config{
			Image: iname,
			Cmd:   spconf.Cmd(),
		},
	})

	if err != nil {
		return err
	}

	//start the container we created
	err = m.client.StartContainer(c.ID, &docker.HostConfig{PortBindings: spconf.PortBindings()})
	if err != nil {
		return err
	}

	//'ping' logs until we got something that indicates
	// it started
	to := make(chan bool, 1)
	go func() {
		time.Sleep(spconf.ReadyTimeout())
		to <- true
	}()

	//start pinging logs
	var buf bytes.Buffer
	for {

		buf.Reset()
		err = m.client.Logs(docker.LogsOptions{
			Container:    c.ID,
			OutputStream: &buf,
			ErrorStream:  &buf,
			Stdout:       true,
			Stderr:       true,
			RawTerminal:  true,
		})
		if err != nil {
			return err
		}

		//if it matches; break loop the state started
		if spconf.ReadyExp().MatchString(buf.String()) {
			break
		}

		select {
		case <-to:
			return fmt.Errorf("Mock server starting timed out")
			break
		case <-time.After(ReadyInterval):
			continue
		}

	}

	//@todo, wait for state container to be ready

	//get container port mapping
	ci, err := m.client.InspectContainer(c.ID)
	if err != nil {
		return err
	}

	_ = ci
	//@todo formulate and return information that is handy for consumers

	return nil
}

// stop a state by removing a Docker container
func (m *Manager) Stop(pname, sname string) error {

	//create name for container
	root := m.ContextPath(pname, sname)
	iname, err := m.ImageName(pname, root)
	if err != nil {
		return err
	}

	//get all containers
	cs, err := m.client.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		return err
	}

	//get container that matches the name
	// var container *docker.APIContainers
	var container docker.APIContainers
	for _, c := range cs {
		for _, n := range c.Names {
			if n[1:] == iname {
				container = c
			}
		}
	}

	//remove hard since mocks are ephemeral
	return m.client.RemoveContainer(docker.RemoveContainerOptions{
		ID:            container.ID,
		RemoveVolumes: true,
		Force:         true,
	})
}
