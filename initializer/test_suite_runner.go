package initializer

import (
	"context"
	"github.com/docker/distribution/uuid"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gmarchetti/kurtosis/commons"
	"os"

	"github.com/palantir/stacktrace"
)


type TestSuiteRunner struct {
	tests map[string]commons.TestNetworkConfigProvider
	dockerManager *commons.DockerManager
}

func NewTestSuiteRunner(dockerManager *commons.DockerManager) *TestSuiteRunner {
	return &TestSuiteRunner{
		tests: make(map[string]commons.TestNetworkConfigProvider),
		dockerManager: dockerManager,
	}
}

func (runner TestSuiteRunner) RegisterTest(name string, configProvider commons.TestNetworkConfigProvider) {
	// TODO check if the test already exists and throw an error if so (means an error in the user code)
	runner.tests[name] = configProvider
}

// Runs the tests whose names are defined in the given map (the map value is ignored - this is a hacky way to
// do a set implementation)
func (runner TestSuiteRunner) RunTests() (err error) {

	// TODO implement parallelism and specific test selection here
	for testName, configProvider := range runner.tests {
		testNetworkCfg, err := configProvider.GetNetworkConfig()
		if err != nil {
			stacktrace.Propagate(err, "Unable to get network config from config provider")
		}
		networkName := testName + uuid.Generate().String()
		serviceNetwork, err := testNetworkCfg.CreateAndRun(networkName, runner.dockerManager)
		if err != nil {
			return stacktrace.Propagate(err, "Unable to create network for test '%v'", testName)
		}
		for _, containerId := range serviceNetwork.ContainerIds {
			// TODO TODO TODO MOVE THIS TO DOCKER MANAGER
			waitAndGrabLogsOnError(runner.dockerManager.DockerCtx, runner.dockerManager.DockerClient, containerId)
		}
	}

	return nil
	// TODO add a timeout here
	// TODO gracefully shut down all the Docker containers we started here
}

// ======================== Private helper functions =====================================


func waitAndGrabLogsOnError(dockerCtx context.Context, dockerClient *client.Client, containerId string) (err error) {
	statusCh, errCh := dockerClient.ContainerWait(dockerCtx, containerId, container.WaitConditionNotRunning)

	select {
	case err := <-errCh:
		if err != nil {
			return stacktrace.Propagate(err, "Failed to wait for container to return.")
		}
	case <-statusCh:
	}

	// If the container stops and there is an error, grab the logs.
	out, err := dockerClient.ContainerLogs(
		dockerCtx,
		containerId,
		types.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
		})
	if err != nil {
		return stacktrace.Propagate(err, "Failed to retrieve container logs.")
	}

	// Copy the logs to stdout.
	stdcopy.StdCopy(os.Stdout, os.Stderr, out)
	return nil
}
