package state

import (
	"archive/tar"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/fsouza/go-dockerclient"
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
	tw := tar.NewWriter(in)

	//expected context path for the state
	root := filepath.Join(m.Dir, pname, fmt.Sprintf("'%s'", sname))

	//walk and tar all files in a dir
	//@from http://stackoverflow.com/questions/13611100/how-to-write-a-directory-not-just-the-files-in-it-to-a-tar-gz-file-in-golang
	visit := func(fpath string, fi os.FileInfo, err error) error {

		//cancel walk if something went wrong
		if err != nil {
			return err
		}

		//skip root
		if fpath == root {
			return nil
		}

		//dont 'add' dirs to archive
		if fi.IsDir() {
			return nil
		}

		f, err := os.Open(fpath)
		if err != nil {
			return err
		}
		defer f.Close()

		//use relative path inside archive
		rel, err := filepath.Rel(root, fpath)
		if err != nil {
			return err
		}

		//create header from file info struct
		hdr, err := tar.FileInfoHeader(fi, rel)
		if err != nil {
			return err
		}

		//write header to archive
		// hdr.Name = rel?
		err = tw.WriteHeader(hdr)
		if err != nil {
			return err
		}

		//copy content into archive
		if _, err = io.Copy(tw, f); err != nil {
			return err
		}

		return nil
	}

	//walk the context and create archive
	if err := filepath.Walk(root, visit); err != nil {
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
