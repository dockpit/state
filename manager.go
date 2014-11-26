package state

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path/filepath"

	"github.com/fsouza/go-dockerclient"

	"github.com/dockpit/dirtar"
)

type Manager struct {
	Dir    string
	Client *docker.Client
}

// Manages state for microservice testing by creating
// docker images and starting containers when necessary
func NewManager(host, cert, path string) (*Manager, error) {
	m := &Manager{
		Dir: path,
	}

	//parse docker host addr as url
	hurl, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	//change to http connection
	hurl.Scheme = "https"

	//create docker client
	m.Client, err = docker.NewTLSClient(hurl.String(), filepath.Join(cert, "cert.pem"), filepath.Join(cert, "key.pem"), filepath.Join(cert, "ca.pem"))
	if err != nil {
		return nil, err
	}

	return m, nil
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
	root := filepath.Join(m.Dir, pname, fmt.Sprintf("'%s'", sname))

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
	if err := m.Client.BuildImage(bopts); err != nil {
		return "", err
	}

	return iname, nil
}

// Start a state by running a Docker container from an image
func (m *Manager) Start(pname, sname string) error {

	return fmt.Errorf("not implemented")
}
