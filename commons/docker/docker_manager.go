package docker

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/palantir/stacktrace"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"strconv"
	"time"
)

const (
	DOCKER_NETWORK_NAME ="kurtosis-bridge"
)

type DockerManager struct {
	dockerCtx           context.Context
	dockerClient        *client.Client
}

func NewDockerManager(dockerCtx context.Context, dockerClient *client.Client) (dockerManager *DockerManager, err error) {
	return &DockerManager{
		dockerCtx:           dockerCtx,
		dockerClient:        dockerClient,
	}, nil
}

func (manager DockerManager) CreateNetwork(subnetMask string, gatewayIP string) (id string, err error)  {
	networkId, ok, err := manager.getNetworkId(DOCKER_NETWORK_NAME)
	if err != nil {
		return "", stacktrace.Propagate(err, "Failed to check for network existence.")
	}
	// Network already exists - return existing id.
	if ok {
		return networkId, nil
	}
	ipamConfig := []network.IPAMConfig{{
		Subnet: subnetMask,
		Gateway: gatewayIP,
	}}
	resp, err := manager.dockerClient.NetworkCreate(manager.dockerCtx, DOCKER_NETWORK_NAME, types.NetworkCreate{
		Driver: "bridge",
		IPAM: &network.IPAM{
			Config: ipamConfig,
		},
	})
	if err != nil {
		return "", stacktrace.Propagate( err, "Failed to create network %s with subnet %s", DOCKER_NETWORK_NAME, subnetMask)
	}
	return resp.ID, nil
}

func (manager DockerManager) CreateVolume(volumeName string) (pathOnHost string, err error) {
	volumeConfig := volume.VolumeCreateBody{
		Name:       volumeName,
	}

	volume, err := manager.dockerClient.VolumeCreate(manager.dockerCtx, volumeConfig)
	if err != nil {
		return "", stacktrace.Propagate(err, "Could not create Docker volume for test controller")
	}

	// TODO this still isn't tested yet; this might not be the right call
	return volume.Mountpoint, nil
}


/*
Creates a Docker container with the given args and starts it.

Args:
	dockerImage: image to start
	attachStdOutErr: whether the container's STDOUT and STDERR should be attached to the caller's
	staticIp: IP the container will be assigned
	usedPorts: a pseudo-set of the ports that the container will listen on (and should be mapped to host ports)
	startCmdArgs: the args that will be used to run the container (leave as nil to run the CMD in the image)
	bindMounts: mapping of (host file) -> (mountpoint on container) that will be mounted on container startup
 */
func (manager DockerManager) CreateAndStartContainer(
	dockerImage string,
	staticIp string,
	usedPorts map[int]bool,
	startCmdArgs []string,
	envVariables map[string]string,
	bindMounts map[string]string,
	volumeMounts map[string]string) (containerIpAddr string, containerId string, err error) {

	imageExistsLocally, err := manager.isImageAvailableLocally(dockerImage)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "Failed to check for image availability.")
	}

	if !imageExistsLocally {
		err = manager.pullImage(dockerImage)
		if err != nil {
			return "", "", stacktrace.Propagate(err, "Failed to pull Docker image.")
		}
	}

	// TODO replace with custom per-test networks
	_, networkExistsLocally, err := manager.getNetworkId(DOCKER_NETWORK_NAME)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "Failed to check for network availability.")
	}
	if !networkExistsLocally {
		return "", "", stacktrace.NewError("Kurtosis Docker network was never created before trying to launch containers. Please call DockerManager.CreateNetwork first.")
	}

	containerConfigPtr, err := manager.getContainerCfg(dockerImage, usedPorts, startCmdArgs, envVariables)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "Failed to configure container from service.")
	}
	containerHostConfigPtr, err := manager.getContainerHostConfig(bindMounts, volumeMounts)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "Failed to configure host to container mappings from service.")
	}
	// TODO probably use a UUID for the network name (and maybe include test name too)
	resp, err := manager.dockerClient.ContainerCreate(manager.dockerCtx, containerConfigPtr, containerHostConfigPtr, nil, "")
	if err != nil {
		return "", "", stacktrace.Propagate(err, "Could not create Docker container from image %v.", dockerImage)
	}
	containerId = resp.ID

	err = manager.connectToNetwork(DOCKER_NETWORK_NAME, containerId, staticIp)
	if err != nil {
		return "","", stacktrace.Propagate(err, "Failed to connect container %s to network.", containerId)
	}
	if err := manager.dockerClient.ContainerStart(manager.dockerCtx, containerId, types.ContainerStartOptions{}); err != nil {
		return "", "", stacktrace.Propagate(err, "Could not start Docker container from image %v.", dockerImage)
	}
	return staticIp, containerId, nil
}

/*
Stops the container with the given container ID, waiting for the provided timeout before forcefully terminating the container
 */
func (manager DockerManager) StopContainer(containerId string, timeout *time.Duration) error {
	err := manager.dockerClient.ContainerStop(manager.dockerCtx, containerId, timeout)
	if err != nil {
		return stacktrace.Propagate(err, "An error occurred stopping container with ID '%v'", containerId)
	}
	return nil
}

/*
Blocks until the given container exits.
 */
func (manager DockerManager) WaitForExit(containerId string) (exitCode int64, err error) {
	statusChannel, errChannel := manager.dockerClient.ContainerWait(manager.dockerCtx, containerId, container.WaitConditionNotRunning)

	// Blocks until one of the channels returns
	select {
	case channErr := <-errChannel:
		exitCode = 1
		err = stacktrace.Propagate(channErr, "Failed to wait for container to return.")
	case status := <-statusChannel:
		exitCode = status.StatusCode
		err = nil
	}
	return
}

func (manager DockerManager) isImageAvailableLocally(imageName string) (isAvailable bool, err error) {
	referenceArg := filters.Arg("reference", imageName)
	filters := filters.NewArgs(referenceArg)
	images, err := manager.dockerClient.ImageList(
		context.Background(),
		types.ImageListOptions{
			All: true,
			Filters: filters,
		})
	if err != nil {
		return false, stacktrace.Propagate(err, "Failed to list images.")
	}
	return len(images) > 0, nil
}

func (manager DockerManager) getNetworkId(networkName string) (networkId string, ok bool, err error) {
	referenceArg := filters.Arg("name", networkName)
	filters := filters.NewArgs(referenceArg)
	networks, err := manager.dockerClient.NetworkList(
		context.Background(),
		types.NetworkListOptions{
			Filters: filters,
		})
	if err != nil {
		return "", false, stacktrace.Propagate(err, "Failed to list networks.")
	}
	if len(networks) == 0 {
		return "", false, nil
	}
	return networks[0].ID, true, nil
}

func (manager DockerManager) connectToNetwork(networkName string, containerId string, staticIpAddr string) (err error) {
	networkId, ok, err := manager.getNetworkId(networkName)
	if err != nil || !ok {
		return stacktrace.Propagate(err, "Failed to get network id for %s", networkName)
	}
	err = manager.dockerClient.NetworkConnect(
		context.Background(),
		networkId,
		containerId,
		&network.EndpointSettings{
			IPAddress: staticIpAddr,
		})
	if err != nil {
		return stacktrace.Propagate(err, "Failed to connect container %s to network %s.", containerId, networkName)
	}
	return nil
}

func (manager DockerManager) pullImage(imageName string) (err error) {
	logrus.Infof("Pulling image %s...", imageName)
	out, err := manager.dockerClient.ImagePull(manager.dockerCtx, imageName, types.ImagePullOptions{})
	if err != nil {
		return stacktrace.Propagate(err, "Failed to pull image %s", imageName)
	}
	defer out.Close()
	io.Copy(ioutil.Discard, out)
	return nil
}

/*
Creates a Docker-Container-To-Host Port mapping, defining how a Container's JSON RPC and service-specific ports are
mapped to the host ports.

Args:
	usedPorts: a "set" of ports that the container will listen on (and which need to be mapped to host ports)
	bindMounts: mapping of (host file) -> (mountpoint on container) that will be mounted at container startup
	volumeMounts: mapping of (volume name) -> (mountpoint on container) that will be mounted at container startup
 */
func (manager *DockerManager) getContainerHostConfig(bindMounts map[string]string, volumeMounts map[string]string) (hostConfig *container.HostConfig, err error) {
	mountsList := make([]mount.Mount, 0, len(volumeMounts))
	for volumeName, containerMountpoint := range volumeMounts {
		mount := mount.Mount{
			Type:          mount.TypeVolume,
			Source:        volumeName,
			Target:        containerMountpoint,
		}
		mountsList = append(mountsList, mount)
	}

	bindsList := make([]string, 0, len(bindMounts))
	for hostFilepath, containerFilepath := range bindMounts {
		bindsList = append(bindsList, hostFilepath + ":" + containerFilepath)
	}

	containerHostConfigPtr := &container.HostConfig{
		Binds: bindsList,
		NetworkMode: container.NetworkMode("default"),
		Mounts: mountsList,
	}
	return containerHostConfigPtr, nil
}

// Creates a Docker container representing a service that will listen on ports in the network
func (manager *DockerManager) getContainerCfg(
			dockerImage string,
			usedPorts map[int]bool,
			startCmdArgs []string,
			envVariables map[string]string) (config *container.Config, err error) {
	portSet := nat.PortSet{}
	for port, _ := range usedPorts {
		otherPort, err := nat.NewPort("tcp", strconv.Itoa(port))
		if err != nil {
			return nil, stacktrace.Propagate(err, "Could not parse port int.")
		}
		portSet[otherPort] = struct{}{}
	}

	envVariablesSlice := make([]string, 0, len(envVariables))
	for key, val := range envVariables {
		envVariablesSlice = append(envVariablesSlice, fmt.Sprintf("%v=%v", key, val))
	}

	nodeConfigPtr := &container.Config{
		Tty: false,
		Image: dockerImage,
		// TODO allow modifying of protocol at some point
		ExposedPorts: portSet,
		Cmd: startCmdArgs,
		Env: envVariablesSlice,
	}
	return nodeConfigPtr, nil
}
