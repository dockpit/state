package state

import (
	"bytes"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/samalba/dockerclient"

	"github.com/dockpit/dirtar"
	"github.com/dockpit/iowait"
	"github.com/dockpit/pit/config"
)

type Manager struct {
	Dir string

	client *dockerclient.DockerClient
	conf   config.C
	host   string
}

// Manages state for microservice testing by creating
// docker images and starting containers when necessary
func NewManager(host, cert, path string, conf config.C) (*Manager, error) {
	m := &Manager{
		Dir: path,

		host: host,
		conf: conf,
	}

	//parse docker host addr as url
	hurl, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	//change to http connection
	hurl.Scheme = "https"

	var tlsc tls.Config
	c, err := tls.LoadX509KeyPair(filepath.Join(cert, "cert.pem"), filepath.Join(cert, "key.pem"))
	tlsc.Certificates = append(tlsc.Certificates, c)
	tlsc.InsecureSkipVerify = true //@todo switch to secure with docker ca.pem

	//create docker client
	m.client, err = dockerclient.NewDockerClient(hurl.String(), &tlsc)
	if err != nil {
		return nil, err
	}

	return m, nil
}

//turn provider and statename into a path
func (m *Manager) contextPath(pname, sname string) string {
	return filepath.Join(m.Dir, pname, fmt.Sprintf("'%s'", sname))
}

// generate an unique image name based on the provider name and path to the state folder
func (m *Manager) imageName(pname, spath string) (string, error) {

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
	root := m.contextPath(pname, sname)

	//tar directory
	err := dirtar.Tar(root, in)
	if err != nil {
		return "", err
	}

	// generate an unique image name based on the provider name and path to the state folder
	iname, err := m.imageName(pname, root)
	if err != nil {
		return "", err
	}

	// fall back to streaming ourselves for building the image, samalba has yet to
	// implement image building: https://github.com/samalba/dockerclient/issues/62
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/build?t=%s", m.client.URL, iname), in)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/tar")
	resp, err := m.client.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	if resp.StatusCode > 200 {
		return "", fmt.Errorf("Unexpected response while building image: %s", resp.Status)
	}

	io.Copy(out, resp.Body)
	return iname, nil
}

// Start a state by running a Docker container from an image
func (m *Manager) Start(pname, sname string) (*StateContainer, error) {
	root := m.contextPath(pname, sname)
	iname, err := m.imageName(pname, root)
	if err != nil {
		return nil, err
	}

	spconf := m.conf.StateProviderConfig(pname)
	if spconf == nil {
		return nil, fmt.Errorf("No configuration for state provider: %s", pname)
	}

	id, err := m.client.CreateContainer(&dockerclient.ContainerConfig{Image: iname, Cmd: spconf.Cmd()}, iname)
	if err != nil {
		return nil, fmt.Errorf("Failed to create state container with image '%s': %s, are your states build?", iname, err)
	}

	bindings := map[string][]dockerclient.PortBinding{}
	for _, p := range spconf.Ports() {
		bindings[p.Container+"/tcp"] = []dockerclient.PortBinding{
			dockerclient.PortBinding{"0.0.0.0", p.Host},
		}
	}

	err = m.client.StartContainer(id, &dockerclient.HostConfig{PortBindings: bindings})
	if err != nil {
		return nil, err
	}

	rc, err := m.client.ContainerLogs(id, &dockerclient.LogOptions{Follow: true, Stdout: true, Stderr: true})
	if err != nil {
		return nil, fmt.Errorf("Failed to follow logs of state container %s: %s", id, err)
	}
	defer rc.Close()

	err = iowait.WaitForRegexp(rc, spconf.ReadyExp(), spconf.ReadyTimeout())
	if err != nil {
		return nil, fmt.Errorf("Failed to wait for state container %s: %s", id, err)
	}

	hurl, err := url.Parse(m.host)
	if err != nil {
		return nil, err
	}

	return &StateContainer{
		ID:   id,
		Host: strings.SplitN(hurl.Host, ":", 2)[0],
	}, nil
}

// stop a state by removing a Docker container
func (m *Manager) Stop(pname, sname string) error {

	//create name for container
	root := m.contextPath(pname, sname)
	iname, err := m.imageName(pname, root)
	if err != nil {
		return err
	}

	//get all containers
	cs, err := m.client.ListContainers(true, false, "")
	if err != nil {
		return err
	}

	//get container that matches the name
	// var container *docker.APIContainers
	var container dockerclient.Container
	for _, c := range cs {
		for _, n := range c.Names {
			if n[1:] == iname {
				container = c
			}
		}
	}

	return m.client.RemoveContainer(container.Id, true)
}
