package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/docker/docker/client"
	"github.com/gmarchetti/kurtosis/ava_commons"
	"github.com/gmarchetti/kurtosis/commons"
	"github.com/gmarchetti/kurtosis/initializer"
	"github.com/palantir/stacktrace"
	"log"
)

func main() {
	fmt.Println("Welcome to Kurtosis E2E Testing for Ava.")

	// Define and parse command line flags.
	geckoImageNameArg := flag.String(
		"gecko-image-name", 
		"gecko-f290f73", // by default, pick commit that was on master May 14, 2020.
		"the name of a pre-built gecko image in your docker engine.",
	)

	// Define and parse command line flags.
	pullImageFromRepoArg := flag.Bool(
		"pull-image-from-repo",
		false, // by default, pick main ava repository.
		"the name of a pre-built gecko image in your docker engine.",
	)

	portRangeStartArg := flag.Int(
		"port-range-start",
		9650,
		"Beginning of port range to be used by testnet on the local environment. Must be between 1024-65535",
	)

	portRangeEndArg := flag.Int(
		"port-range-end",
		9670,
		"End of port range to be used by testnet on the local environment. Must be between 1024-65535",
	)
	flag.Parse()

	// Initialize default environment context.
	dockerCtx := context.Background()
	// Initialize a Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(stacktrace.Propagate(err,"Failed to initialize Docker client from environment."))
	}
	dockerManager, err := commons.NewDockerManager(dockerCtx, dockerClient, *portRangeStartArg, *portRangeEndArg)
	if err != nil {
		panic(stacktrace.Propagate(err,"Failed to initialize Docker Manager."))
	}

	if *pullImageFromRepoArg {
		log.Printf("Pulling image...")
		dockerManager.PullImageFromRepo(*geckoImageNameArg)
	}

	testSuiteRunner := initializer.NewTestSuiteRunner(dockerManager)

	// TODO Uncomment this when our RunTests method supports calling tests by name (rather than just running all tests)
	/*
	singleNodeNetwork := ava_commons.SingleNodeAvaNetworkCfgProvider{GeckoImageName: *geckoImageNameArg}
	testSuiteRunner.RegisterTest("singleNodeNetwork", singleNodeNetwork)

	twoNodeNetwork := ava_commons.TwoNodeAvaNetworkCfgProvider{GeckoImageName: *geckoImageNameArg}
	testSuiteRunner.RegisterTest("twoNodeNetwork", twoNodeNetwork)
	 */

	tenNodeNetwork := ava_commons.TenNodeAvaNetworkCfgProvider{GeckoImageName: *geckoImageNameArg}
	testSuiteRunner.RegisterTest("tenNodeNetwork", tenNodeNetwork)

	// Create the container based on the configurations, but don't start it yet.
	fmt.Println("I'm going to run a Gecko testnet, and hang while it's running! Kill me and then clear your docker containers.")
	error := testSuiteRunner.RunTests()
	if error != nil {
		panic(error)
	}
}
