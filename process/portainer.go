package process

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

const (
	contentType            = "application/json"
	authenticationEndpoint = "http://%s/api/auth"
	startContainerEndpoint = "http://%s/api/endpoints/%s/docker/containers/%s/start"
	stopContainerEndpoint  = "http://%s/api/endpoints/%s/docker/containers/%s/stop"
	getContainerEndpoint   = "http://%s/api/endpoints/%s/docker/containers/%s/json"
	getContainersEndpoint  = "http://%s/api/endpoints/%s/docker/containers/json?all=true"
)

type portainer struct {
	client        http.Client
	token         string
	address       string
	endpointID    string
	containerName string
	username      string
	password      string
}

// NewPortainer creates a new portainer process that manages a docker container
func NewPortainer(containerName, address, endpointID, username, password string) (Process, error) {
	proc := portainer{
		client: http.Client{
			Timeout: contextTimeout,
		},
		address:       address,
		endpointID:    endpointID,
		containerName: fmt.Sprintf("/%s", containerName),
		username:      username,
		password:      password,
	}

	return proc, nil
}

func (proc portainer) Start() error {
	containerID, err := proc.resolveContainerName()
	if err != nil {
		return err
	}

	url := fmt.Sprintf(startContainerEndpoint, proc.address, proc.endpointID, containerID)
	if _, err := proc.request(http.MethodPost, url); err != nil {
		return err
	}

	return nil
}

func (proc portainer) Stop() error {
	containerID, err := proc.resolveContainerName()
	if err != nil {
		return err
	}

	url := fmt.Sprintf(stopContainerEndpoint, proc.address, proc.endpointID, containerID)
	if _, err := proc.request(http.MethodPost, url); err != nil {
		return err
	}

	return nil
}

func (proc portainer) IsRunning() (bool, error) {
	containerID, err := proc.resolveContainerName()
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf(getContainerEndpoint, proc.address, proc.endpointID, containerID)
	response, err := proc.request(http.MethodGet, url)
	if err != nil {
		return false, err
	}

	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return false, err
	}

	state := struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}{}

	if err := json.Unmarshal(data, &state); err != nil {
		return false, err
	}

	return state.State.Running, nil
}

func (proc portainer) request(method, url string) (*http.Response, error) {
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", proc.token))

	response, err := proc.client.Do(request)
	if err != nil {
		return nil, err
	}

	switch response.StatusCode {
	case http.StatusNotFound:
		return nil, errors.New("no such container")
	case http.StatusInternalServerError:
		return nil, fmt.Errorf("server error: %s", url)
	case http.StatusUnauthorized:
		if err := proc.authenticate(); err != nil {
			return nil, fmt.Errorf("could not authorize; %s", err)
		}
		return proc.request(method, url)
	}

	return response, nil
}

func (proc portainer) resolveContainerName() (string, error) {
	url := fmt.Sprintf(getContainersEndpoint, proc.address, proc.endpointID)
	response, err := proc.request(http.MethodGet, url)
	if err != nil {
		return "", err
	}

	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	var containers []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
	}

	if err := json.Unmarshal(data, &containers); err != nil {
		return "", err
	}

	for _, container := range containers {
		for _, name := range container.Names {
			if name != proc.containerName {
				continue
			}
			return container.ID, nil
		}
	}

	return "", fmt.Errorf("container with name \"%s\" not found", proc.containerName)
}

func (proc *portainer) authenticate() error {
	var credentials = struct {
		Username string `json:"Username"`
		Password string `json:"Password"`
	}{
		Username: proc.username,
		Password: proc.password,
	}

	bodyJSON, err := json.Marshal(credentials)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(authenticationEndpoint, proc.address)
	response, err := proc.client.Post(url, contentType, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusOK {
		return errors.New(http.StatusText(response.StatusCode))
	}

	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var jwtResponse = struct {
		JWT string `json:"jwt"`
	}{}

	if err := json.Unmarshal(data, &jwtResponse); err != nil {
		return err
	}

	proc.token = jwtResponse.JWT

	return nil
}
