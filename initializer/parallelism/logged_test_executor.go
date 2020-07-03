package parallelism

import (
	"context"
	"fmt"
	"github.com/docker/distribution/uuid"
	"github.com/docker/docker/client"
	"github.com/kurtosis-tech/kurtosis/commons/docker"
	"github.com/kurtosis-tech/kurtosis/commons/networks"
	"github.com/kurtosis-tech/kurtosis/initializer"
	"github.com/palantir/stacktrace"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"os"
)

/*
WARNING WARNING WARNING WARNING WARNING WARNING WARNING WARNING WARNING WARNING
No logging to the system-level logger is allowed in this file!!! Everything should use the specific
logger passed in at construction time!!
WARNING WARNING WARNING WARNING WARNING WARNING WARNING WARNING WARNING WARNING
 */

type loggedTestExecutor struct {
	log *logrus.Logger
}

func newLoggedTestExecutor(log *logrus.Logger) *loggedTestExecutor {
	return &loggedTestExecutor{log: log}
}

/*
Returns:
	error: An error if an error occurred *while setting up or running the test* (independent from whether the test itself passed)
	bool: A boolean indicating whether the test passed (undefined if an error occurred running the test)
 */
func (executor loggedTestExecutor) runTest(
		executionInstanceId uuid.UUID,
		testContext context.Context,
		dockerClient *client.Client,
		subnetMask string,
		testControllerImageName string,
		testControllerLogLevel string,
		testServiceImageName string,
		testName string) (bool, error) {
	executor.log.Info("Creating Docker client from environment settings...")
	dockerManager, err := docker.NewDockerManager(executor.log, testContext, dockerClient)
	if err != nil {
		return false, stacktrace.Propagate(err, "An error occurred getting the Docker manager for test %v", testName)
	}
	executor.log.Info("Docker client created successfully")

	executor.log.Infof("Creating Docker network for test with subnet mask %v...", subnetMask)
	networkName := fmt.Sprintf("%v-%v", executionInstanceId.String(), testName)
	publicIpProvider, err := networks.NewFreeIpAddrTracker(executor.log, subnetMask, []string{})
	if err != nil {
		return false, stacktrace.Propagate(err, "Could not create the free IP addr tracker")
	}
	gatewayIp, err := publicIpProvider.GetFreeIpAddr()
	if err != nil {
		return false, stacktrace.Propagate(err, "An error occurred getting the gateway IP")
	}
	_, err = dockerManager.CreateNetwork(networkName, subnetMask, gatewayIp)
	if err != nil {
		return false, stacktrace.Propagate(err, "Error occurred creating docker network for testnet")
	}
	defer removeNetworkDeferredFunc(executor.log, dockerManager, networkName)
	executor.log.Infof("Docker network %v created successfully", networkName)

	executor.log.Info("Running test controller...")
	controllerIp, err := publicIpProvider.GetFreeIpAddr()
	if err != nil {
		return false, stacktrace.NewError("An error occurred getting an IP for the test controller")
	}
	testPassed, err := runControllerContainer(
		executor.log,
		dockerManager,
		networkName,
		subnetMask,
		gatewayIp,
		controllerIp,
		testControllerImageName,
		testControllerLogLevel,
		testServiceImageName,
		testName,
		executionInstanceId)
	if err != nil {
		return false, stacktrace.Propagate(err, "An error occurred while running the test, independent of test success")
	}
	return testPassed, nil
	// TODO after printing logs, delete each container???
}

/*
Runs the controller container against the given test network.

Args:
	log: The test-specific logger to write log messages to
	manager: the Docker manager, used for starting container & waiting for it to finish
	networkName: The name of the Docker network that the controller container will run in
	subnetMask: The CIDR representation of the network that the Docker network that the controller container is running in
	gatewayIp: The IP of the gateway on the Docker network that the controller is running in
	controllerIpAddr: The IP address that should be used for the container that the controller is running in
	controllerImageName: The name of the Docker image that should be used to run the controller container
	logLevel: A string representing the log level that the controller should use (will be passed as-is to the controller;
		should be semantically meaningful to the given controller image)
	testServiceImageName: The name of the Docker image that's being tested, and will be used for spinning up "test" nodes
		on the controller
	testName: Name of the test to tell the controller to run
	executionUuid: A UUID representing this specific execution of the test suite

Returns:
	bool: true if the test succeeded, false if not
	error: if any error occurred during the execution of the controller (independent of the test itself)
*/
func runControllerContainer(
			log *logrus.Logger,
			manager *docker.DockerManager,
			networkName string,
			subnetMask string,
			gatewayIp string,
			controllerIpAddr string,
			controllerImageName string,
			logLevel string,
			testServiceImageName string,
			testName string,
			executionUuid uuid.UUID) (bool, error){
	volumeName := fmt.Sprintf("%v-%v", executionUuid.String(), testName)
	if err := manager.CreateVolume(volumeName); err != nil {
		return false, stacktrace.Propagate(err, "Error creating Docker volume to share amongst test nodes")
	}

	testControllerLogFilename := fmt.Sprintf("%v-%v-controller-logs", executionUuid.String(), executionUuid.String())
	logTmpFile, err := ioutil.TempFile("", testControllerLogFilename)
	if err != nil {
		return false, stacktrace.Propagate(err, "Could not create tempfile to store log info for passing to test controller")
	}
	logTmpFile.Close()
	log.Debugf("Temp filepath to write log file to: %v", logTmpFile.Name())

	envVariables := generateTestControllerEnvVariables(
		networkName,
		subnetMask,
		gatewayIp,
		controllerIpAddr,
		testName,
		logLevel,
		testServiceImageName,
		volumeName)
	log.Debugf("Environment variables that are being passed to the controller: %v", envVariables)

	_, controllerContainerId, err := manager.CreateAndStartContainer(
		controllerImageName,
		networkName,
		controllerIpAddr,
		make(map[int]bool),
		nil, // Use the default image CMD (which is parameterized)
		envVariables,
		map[string]string{
			// Because the test controller will need to spin up new images, we need to bind-mount the host Docker engine into the test controller
			"/var/run/docker.sock": "/var/run/docker.sock",
			logTmpFile.Name():      initializer.CONTROLLER_LOG_MOUNT_FILEPATH,
		},
		map[string]string{
			volumeName: initializer.TEST_VOLUME_MOUNTPOINT,
		})
	if err != nil {
		return false, stacktrace.Propagate(err, "Failed to run test controller container")
	}
	log.Infof("Controller container started successfully with id %s", controllerContainerId)

	log.Info("Waiting for controller container to exit...")
	// TODO add a timeout here if the test doesn't complete successfully
	exitCode, err := manager.WaitForExit(controllerContainerId)
	if err != nil {
		return false, stacktrace.Propagate(err, "Failed when waiting for controller to exit")
	}
	log.Info("Controller container exited successfully")

	// We open a new fp for reading because our original FP is only for writing
	log.Infof("- - - - - - - - - - - Controller Logs - - - - - - - - - - - - - -")
	logReadFp, err := os.Open(logTmpFile.Name())
	if err != nil {
		return false, stacktrace.Propagate(err, "Failed to open controller log file for reading")
	}
	io.Copy(log.Out, logReadFp)
	log.Infof("- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -")

	logReadFp.Close()
	os.Remove(logTmpFile.Name()) // We're responsible for removing the tempfile

	// TODO Clean up the volumeFilepath we created!
	return exitCode == initializer.SUCCESS_EXIT_CODE, nil
}

/*
Helper function for making a best-effort attempt at removing a network and logging any error states; intended to be run
as a deferred function.
*/
func removeNetworkDeferredFunc(log *logrus.Logger, dockerManager *docker.DockerManager, networkName string) {
	log.Infof("Attempting to remove Docker network with name %v...", networkName)
	err := dockerManager.RemoveNetwork(networkName)
	if err != nil {
		log.Errorf("An error occurred removing Docker network with name %v:", networkName)
		log.Error(err.Error())
	} else {
		log.Infof("Docker network %v successfully removed", networkName)
	}
}

/*
NOTE: This is a separate function because it provides a nice documentation reference point, where we can say to users,
"to see the latest special environment variables that will be passed to the test controller, see this function". Do not
put anything else in this function!!!

Args:
	networkName: The name of the Docker network that the test controller is running in, and which all services should be started in
	subnetMask: The subnet mask used to create the Docker network that the test controller, and all services it starts, are running in
	gatewayIp: The IP of the gateway of the Docker network that the test controller will run inside
	controllerIpAddr: The IP address of the container running the test controller
	testName: The name of the test that the test controller should run
	logLevel: A string representing the controller's loglevel (NOTE: this should be interpretable by the controller; the
		initializer will not know what to do with this!)
	testServiceImageName: The name of the Docker image of the service that we're testing
	testVolumeName: The name of the Docker volume that has been created for this particular test execution, and that the
		test controller can share with the services that it spins up to read and write data to them
*/
func generateTestControllerEnvVariables(
			networkName string,
			subnetMask string,
			gatewayIp string,
			controllerIpAddr string,
			testName string,
			logLevel string,
			testServiceImageName string,
			testVolumeName string) map[string]string {
	return map[string]string{
		initializer.TEST_NAME_BASH_ARG:         testName,
		initializer.SUBNET_MASK_ARG:            subnetMask,
		initializer.NETWORK_NAME_ARG:           networkName,
		initializer.GATEWAY_IP_ARG:             gatewayIp,
		initializer.LOG_FILEPATH_ARG:           initializer.CONTROLLER_LOG_MOUNT_FILEPATH,
		initializer.LOG_LEVEL_ARG:              logLevel,
		initializer.TEST_IMAGE_NAME_ARG:        testServiceImageName,
		initializer.TEST_CONTROLLER_IP_ARG:     controllerIpAddr,
		initializer.TEST_VOLUME_ARG:            testVolumeName,
		initializer.TEST_VOLUME_MOUNTPOINT_ARG: initializer.TEST_VOLUME_MOUNTPOINT,
	}
}